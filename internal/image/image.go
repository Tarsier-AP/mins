// Package image implements the still/animated image paths: input preparation,
// the quality search, the VMAF gate, and video -> animated image conversion.
package image

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"mins/internal/core"
	"mins/internal/escape"
	"mins/internal/logx"
	"mins/internal/media"
	"mins/internal/native"
	"mins/internal/report"
	"mins/internal/result"
	"mins/internal/util"
	"mins/internal/vmaf"
)

// Processor carries the shared state of one image encode.
type Processor struct {
	E   *core.Env
	B   *native.Backend
	Tag string
}

// qfloor is img_qfloor: the lowest quality the backend accepts.
func qfloor(e *core.Env) int {
	if e.C.ImgBackend == "libjxl" {
		return 5
	}
	return 0
}

func errRow(name, mode, msg string) result.Row {
	return result.Row{Output: name, Mode: mode, Q: "-", VMAF: "-", Size: "-", Status: msg}
}

// toolName is the encoder executable name for error rows.
func toolName(backend string, animated bool) string {
	switch backend {
	case "libwebp":
		if animated {
			return "img2webp"
		}
		return "cwebp"
	case "libavif":
		return "avifenc"
	case "libjxl":
		return "cjxl"
	}
	return backend
}

// reportFields fills the per-format report fields shared by the image and
// video-to-image reports.
func reportFields(e *core.Env, img *report.Image) {
	switch e.C.ImgFmt {
	case "webp":
		v := e.C.WebPMethod
		img.WebPMethod = &v
	case "avif":
		v := e.C.AVIFSpeed
		img.AvifSpeed = &v
		if e.C.ImgBackend == "libavif" {
			img.AvifYUV = e.C.AvifYUV
		}
	case "jxl":
		v := e.C.JXLEffort
		img.JXLEffort = &v
	}
}

// vmafStill measures one still candidate against one reference PNG.
func vmafStill(e *core.Env, cand, ref, vlog, errLog string) (float64, int, error) {
	filter := fmt.Sprintf("[0:v]format=yuv420p,setpts=PTS-STARTPTS[dist];"+
		"[1:v]format=yuv420p,setpts=PTS-STARTPTS[ref];"+
		"[dist][ref]libvmaf=n_threads=%d:log_path=%s:log_fmt=json",
		e.C.NProc, escape.Filter(vlog))
	_ = os.Remove(vlog)
	args := []string{"-hide_banner", "-nostdin", "-y", "-nostats", "-loglevel", "error",
		"-i", cand, "-i", ref, "-filter_complex", filter, "-f", "null", "-"}
	out, err := exec.Command(e.R.FFmpeg, args...).CombinedOutput()
	_ = os.WriteFile(errLog, out, 0o644)
	if err != nil {
		return 0, 0, fmt.Errorf("libvmaf failed on %s vs %s", filepath.Base(cand), filepath.Base(ref))
	}
	lg, err := vmaf.Parse(vlog)
	if err != nil {
		return 0, 0, fmt.Errorf("libvmaf ran but produced no readable VMAF log")
	}
	mean, n := lg.Mean()
	return mean, n, nil
}

// vmafAnim measures an animation frame sequence against the reference
// sequence, frame by frame.
func vmafAnim(e *core.Env, distDir, refDir, vlog, errLog string) (float64, int, int, error) {
	distN := countFrames(distDir)
	refN := countFrames(refDir)
	if distN == 0 || refN == 0 {
		return 0, distN, refN, fmt.Errorf("frame extraction produced no frames")
	}
	filter := fmt.Sprintf("[0:v]format=yuv420p,setpts=PTS-STARTPTS[dist];"+
		"[1:v]format=yuv420p,setpts=PTS-STARTPTS[ref];"+
		"[dist][ref]libvmaf=n_threads=%d:log_path=%s:log_fmt=json",
		e.C.NProc, escape.Filter(vlog))
	_ = os.Remove(vlog)
	args := []string{"-hide_banner", "-nostdin", "-y", "-nostats", "-loglevel", "error",
		"-framerate", "1", "-i", filepath.Join(distDir, "f-%06d.png"),
		"-framerate", "1", "-i", filepath.Join(refDir, "f-%06d.png"),
		"-lavfi", filter, "-f", "null", "-"}
	out, err := exec.Command(e.R.FFmpeg, args...).CombinedOutput()
	_ = os.WriteFile(errLog, out, 0o644)
	if err != nil {
		return 0, distN, refN, fmt.Errorf("libvmaf failed on the animated frames")
	}
	lg, err := vmaf.Parse(vlog)
	if err != nil {
		return 0, distN, refN, fmt.Errorf("libvmaf ran but produced no readable VMAF log")
	}
	mean, n := lg.Mean()
	return mean, n, refN, nil
}

func countFrames(dir string) int {
	m, _ := filepath.Glob(filepath.Join(dir, "f-*.png"))
	return len(m)
}

// encodeFail reports and logs an encoder failure exactly like magick_failed
// / anim_failed did.
func encodeFail(e *core.Env, input, elog, tool, tag string) result.Row {
	if st, err := os.Stat(elog); err == nil && st.Size() > 0 {
		tail := logx.Tail(elog, 20)
		fmt.Fprintln(os.Stderr, tail)
		e.Log.Append("--- " + tool + " stderr (tail) ---")
		e.Log.Append(tail)
		e.Log.CopyLog(elog, tag+"_"+tool)
	}
	return errRow(filepath.Base(input), e.C.ImgFmt, "ERROR: "+tool+" encode failed")
}

// Process handles one image input (still or animated).
func Process(e *core.Env, input string) (result.Row, bool, error) {
	c := e.C
	mode := c.ImgFmt
	if _, err := os.Stat(input); err != nil {
		return errRow(filepath.Base(input), mode, "ERROR: not found"), true, nil
	}
	dir, err := core.AbsDir(input)
	if err != nil {
		return errRow(filepath.Base(input), mode, "ERROR: cannot enter "+filepath.Dir(input)), true, nil
	}
	base := util.InStem(input)
	ext := util.InExt(input)
	out := filepath.Join(dir, base+"."+c.ImgFmt)
	rep := filepath.Join(dir, base+".vmaf.json")
	if strings.EqualFold(ext, c.ImgFmt) {
		out = filepath.Join(dir, base+".min."+c.ImgFmt)
		rep = filepath.Join(dir, base+".min.vmaf.json")
	}
	if c.SkipExisting {
		if _, err := os.Stat(out); err == nil {
			e.Log.Info("skipping %s: output already exists", filepath.Base(input))
			return errRow(filepath.Base(out), mode, "SKIPPED (exists)"), false, nil
		}
	}

	info, err := probeImage(e, input)
	if err != nil || info.Width <= 0 || info.Height <= 0 {
		return errRow(filepath.Base(input), mode, "ERROR: unreadable by ffmpeg"), true, nil
	}
	res := fmt.Sprintf("%dx%d", info.Width, info.Height)
	nframes := info.Frames
	if nframes < 1 {
		nframes = 1
	}
	animated := nframes > 1

	work, err := core.TempDir(input)
	if err != nil {
		return errRow(filepath.Base(input), mode, "ERROR: cannot create temp dir in "+dir), true, nil
	}
	p := &Processor{E: e, B: e.Img, Tag: tagOf(input)}

	natSrc := filepath.Join(work, "nat_src.png")
	natRef := filepath.Join(work, "nat_ref")
	natRefFlat := filepath.Join(work, "nat_ref_flat")
	natRefFlatFile := natRefFlat + ".png"
	var frames []string
	var delays []int
	prepLog := filepath.Join(work, "nat_prep.err")

	if animated {
		frames, delays, err = p.B.PrepareAnim(input, natRef, prepLog)
		if err == nil {
			err = p.B.FlattenDir(natRef, natRefFlat, prepLog)
		}
		if err != nil || len(frames) != nframes {
			showTail(prepLog)
			return errRow(filepath.Base(input), mode,
				fmt.Sprintf("ERROR: ffmpeg could not prepare %d frame(s) for %s", nframes, p.B.Name)), true, nil
		}
		nframes = len(frames)
		animated = nframes > 1
	} else {
		err = p.B.PrepareStill(input, natSrc, prepLog)
		if err == nil {
			err = p.B.FlattenFile(natSrc, natRefFlatFile, prepLog)
		}
		if err != nil {
			showTail(prepLog)
			return errRow(filepath.Base(input), mode,
				fmt.Sprintf("ERROR: ffmpeg could not prepare input for %s", p.B.Name)), true, nil
		}
		frames = []string{natSrc}
	}

	encode := func(q int, dst string) (string, error) {
		elog := filepath.Join(work, "magick_err.log")
		_ = os.Remove(elog)
		tool, err := p.B.Encode(q, dst, frames, delays, elog)
		return tool, err
	}

	measure := func(cand string) (float64, error) {
		vlog := filepath.Join(work, "vmaf_img.json")
		if animated {
			distDir := filepath.Join(work, "vmaf_dist_frames")
			rawDir := filepath.Join(work, "vmaf_nat_raw")
			_ = os.RemoveAll(distDir)
			_ = os.RemoveAll(rawDir)
			elog := filepath.Join(work, "vmaf_nat_dist.err")
			if err := p.B.DecodeFrames(cand, rawDir, nframes, elog); err != nil {
				showTail(elog)
				e.Log.Append("--- " + p.B.Name + " frame decode (tail) ---")
				e.Log.Append(logx.Tail(elog, 20))
				return 0, core.Dief("%s could not decode all %d frames of %s for the VMAF gate (animated)",
					p.B.Name, nframes, filepath.Base(cand))
			}
			if err := p.B.FlattenDir(rawDir, distDir, elog); err != nil {
				showTail(elog)
				return 0, core.Dief("%s could not prepare the decoded frames for the VMAF gate", p.B.Name)
			}
			mean, seen, refN, err := vmafAnim(e, distDir, natRefFlat, vlog, filepath.Join(work, "vmaf_img.err"))
			if err != nil {
				showTail(filepath.Join(work, "vmaf_img.err"))
				e.Log.Append("--- libvmaf (animated image) stderr (tail) ---")
				e.Log.Append(logx.Tail(filepath.Join(work, "vmaf_img.err"), 20))
				return 0, core.Dief("libvmaf failed on %s vs %s (animated)", filepath.Base(cand), filepath.Base(input))
			}
			distN := countFrames(distDir)
			if distN != refN {
				e.Log.Info("warning: encode has %d frames but source has %d — VMAF only covers the overlap", distN, refN)
			} else if seen != refN {
				e.Log.Info("warning: vmaf compared %d frames but source has %d", seen, refN)
			}
			return mean, nil
		}
		distPng := filepath.Join(work, "vmaf_dist.png")
		rawPng := filepath.Join(work, "vmaf_nat_raw.png")
		elog := filepath.Join(work, "vmaf_nat_dist.err")
		if err := p.B.DecodeStill(cand, rawPng, elog); err != nil {
			showTail(elog)
			return 0, core.Dief("%s could not decode %s for the VMAF gate", p.B.Name, filepath.Base(cand))
		}
		if err := p.B.FlattenFile(rawPng, distPng, elog); err != nil {
			showTail(elog)
			return 0, core.Dief("%s could not prepare %s for the VMAF gate", p.B.Name, filepath.Base(cand))
		}
		mean, _, err := vmafStill(e, distPng, natRefFlatFile, vlog, filepath.Join(work, "vmaf_img.err"))
		if err != nil {
			showTail(filepath.Join(work, "vmaf_img.err"))
			e.Log.Append("--- libvmaf (image) stderr (tail) ---")
			e.Log.Append(logx.Tail(filepath.Join(work, "vmaf_img.err"), 20))
			return 0, core.Dief("libvmaf failed on %s vs %s", filepath.Base(cand), filepath.Base(input))
		}
		return mean, nil
	}

	effort := effortDesc(e)
	animDesc := ""
	if animated {
		animDesc = fmt.Sprintf(", %d frames animated", nframes)
	}
	e.Log.Info("input : %s (%s%s) -> %s %s", filepath.Base(input), res, animDesc, c.ImgFmt, effort)

	baseLo := c.QMin
	if qf := qfloor(e); baseLo < qf {
		baseLo = qf
	}
	hi, seed := c.QMax, -1
	if c.QAuto && e.QState != nil {
		hi, seed = e.QState.Window(baseLo)
	}
	encodeTool := ""
	eval := func(q int) (float64, int64, error) {
		cand := filepath.Join(work, fmt.Sprintf("search_%d.%s", q, c.ImgFmt))
		tool, err := encode(q, cand)
		encodeTool = tool
		if err != nil {
			return 0, 0, errEncodeFailed
		}
		defer os.Remove(cand)
		mean, err := measure(cand)
		if err != nil {
			return 0, 0, err
		}
		return mean, fileSize(cand), nil
	}
	found, err := searchQuality(searchRequest{
		Lo: baseLo, Hi: hi, Seed: seed, Target: c.TargetVMAF,
		Auto: c.QAuto,
		Eval: eval,
		Log:  e.Log.Info,
		Attempt: func(q int, mean float64, verdict string) {
			e.TUI.Attempt("search", q, mean, c.TargetVMAF, verdict)
		},
	})
	if err != nil {
		if errors.Is(err, errEncodeFailed) {
			return encodeFail(e, input, filepath.Join(work, "magick_err.log"), encodeTool, p.Tag), true, nil
		}
		return result.Row{}, true, err
	}
	best := found.Best
	if best < 0 {
		if c.QAuto {
			best = found.Hi
			e.Log.Info("warning: no quality in [%d..%d] reached VMAF %s; falling back to q=%d",
				found.Lo, found.Hi, c.TargetRaw, best)
		} else {
			best = c.QMax
			e.Log.Info("warning: no quality in [%d..%d] reached VMAF %s; falling back to q=%d",
				c.QMin, c.QMax, c.TargetRaw, best)
		}
	}

	inSize := fileSize(input)
	final := filepath.Join(work, "final."+c.ImgFmt)
	tool, err := encode(best, final)
	if err != nil {
		return encodeFail(e, input, filepath.Join(work, "magick_err.log"), tool, p.Tag), true, nil
	}
	mean, err := measure(final)
	if err != nil {
		return result.Row{}, true, err
	}
	size := fileSize(final)
	passed := util.AtLeast(mean, c.TargetVMAF)
	if c.QAuto && e.QState != nil {
		if passed {
			e.QState.Record(best)
		} else if found.Best < 0 {
			e.QState.RecordFailure()
		}
	}
	verdict := "FAIL"
	if passed {
		verdict = "PASS"
	}
	e.Log.Info("final : q=%d vmaf=%s size=%s %s", best, util.Float(mean), util.HumanSize(size), verdict)
	e.TUI.Attempt("final", best, mean, c.TargetVMAF, verdict)

	if err := os.Rename(final, out); err != nil {
		return result.Row{}, false, err
	}

	if !c.NoJSON {
		doc := &report.Image{
			Input: input, Output: out, Mode: "image",
			Encoder: p.B.Name + "/" + c.ImgFmt, Quality: best,
			TargetVMAF: report.F(c.TargetVMAF), MeanVMAF: report.F(mean),
			Passed: passed, Dimensions: res, Animated: animated,
			Frames: nframes, SizeBytes: size,
		}
		reportFields(e, doc)
		if err := report.Write(rep, doc); err != nil {
			return result.Row{}, false, err
		}
	}

	status := "PASS"
	failed := false
	if !passed {
		status = "FAIL (gate)"
		failed = true
		e.TUI.Message("did not reach target VMAF %s (best q=%d, vmaf=%s)", c.TargetRaw, best, util.Float(mean))
	}
	return result.Row{
		Output: filepath.Base(out), Mode: c.ImgFmt, Q: strconv.Itoa(best),
		VMAF:   util.Float(mean),
		Size:   util.HumanSize(inSize) + "->" + util.HumanSize(size),
		Status: status,
	}, failed, nil
}

func effortDesc(e *core.Env) string {
	switch e.C.ImgBackend {
	case "libwebp":
		return fmt.Sprintf("%s method=%d", e.C.ImgBackend, e.C.WebPMethod)
	case "libavif":
		return fmt.Sprintf("%s speed=%d yuv%s", e.C.ImgBackend, e.C.AVIFSpeed, e.C.AvifYUV)
	case "libjxl":
		return fmt.Sprintf("%s effort=%d", e.C.ImgBackend, e.C.JXLEffort)
	}
	return e.C.ImgBackend
}

func showTail(path string) {
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		fmt.Fprintln(os.Stderr, logx.Tail(path, 20))
	}
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

// tagOf mirrors CUR_TAG: the sanitized stem plus the trailing newline that
// in_stem's echo used to turn into an underscore.
func tagOf(input string) string {
	return util.SafeTag(util.InStem(input)) + "_"
}

// probeImage reads dimensions/frame count. HEIC/HEIF stills are not readable
// by ffmpeg without libheif, so heif-info supplies their geometry.
func probeImage(e *core.Env, input string) (*media.ImageInfo, error) {
	info, err := media.ProbeImage(e.R, input)
	if err == nil {
		return info, nil
	}
	ext := strings.ToLower(util.InExt(input))
	if ext != "heic" && ext != "heif" {
		return nil, err
	}
	w, h, ferr := heifInfo(e, input)
	if ferr != nil {
		return nil, err
	}
	return &media.ImageInfo{Width: w, Height: h, Frames: 1}, nil
}

func heifInfo(e *core.Env, input string) (int, int, error) {
	p, err := e.Tools.Path("heif-info")
	if err != nil {
		return 0, 0, err
	}
	out, err := exec.Command(p, input).CombinedOutput()
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "image:") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "image:"))
		if len(fields) == 0 {
			continue
		}
		parts := strings.SplitN(fields[0], "x", 2)
		if len(parts) != 2 {
			continue
		}
		w, err1 := strconv.Atoi(parts[0])
		h, err2 := strconv.Atoi(parts[1])
		if err1 == nil && err2 == nil {
			return w, h, nil
		}
	}
	return 0, 0, fmt.Errorf("heif-info printed no image dimensions")
}
