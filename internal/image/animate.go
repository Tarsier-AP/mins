package image

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"mins/internal/core"
	"mins/internal/logx"
	"mins/internal/media"
	"mins/internal/report"
	"mins/internal/result"
	"mins/internal/util"
)

var reDur = regexp.MustCompile(`^[0-9]+([.][0-9]+)?$`)

// ProcessAnimate turns a video into an animated WebP/AVIF/JXL, gating every
// extracted frame against the encode.
func ProcessAnimate(e *core.Env, input string) (result.Row, bool, error) {
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

	v, err := media.ProbeVideo(e.R, input)
	durStr, derr := media.DurationRaw(e.R, input)
	res := ""
	if v != nil {
		res = fmt.Sprintf("%d,%d", v.Width, v.Height)
	}
	if err != nil || v == nil || v.Width <= 0 || v.Height <= 0 || derr != nil || !reDur.MatchString(durStr) {
		return errRow(filepath.Base(input), mode,
			fmt.Sprintf("ERROR: ffprobe failed / invalid metadata (RES='%s' DUR='%s')", res, durStr)), true, nil
	}
	dur, _ := strconv.ParseFloat(durStr, 64)

	ss := c.StartRaw
	en := c.EndRaw
	t := ""
	clipDur := durStr
	if ss != "" || en != "" {
		if ss == "" {
			ss = "0"
		}
		if en == "" {
			clipDur = fmt.Sprintf("%.3f", dur-c.Start)
		} else {
			t = fmt.Sprintf("%.3f", c.End-c.Start)
			clipDur = t
		}
		if util.AtLeast(c.Start, dur) {
			return errRow(filepath.Base(input), mode, "ERROR: trim past EOF"), true, nil
		}
		clip, _ := strconv.ParseFloat(clipDur, 64)
		if util.AtLeast(clip, dur) {
			e.Log.Info("warning: trim window reaches past EOF (%ss); clamping", durStr)
			en = durStr
			t = ""
			clipDur = fmt.Sprintf("%.3f", dur-c.Start)
		}
	}

	trimSuffix := ""
	if c.StartRaw != "" || c.EndRaw != "" {
		trimSuffix = fmt.Sprintf(".trim%s-%s", ss, en)
	}
	out := filepath.Join(dir, base+trimSuffix+"."+c.ImgFmt)
	rep := filepath.Join(dir, base+trimSuffix+".vmaf.json")
	if c.SkipExisting {
		if _, err := os.Stat(out); err == nil {
			e.Log.Info("skipping %s: output already exists", filepath.Base(input))
			return errRow(filepath.Base(out), mode, "SKIPPED (exists)"), false, nil
		}
	}

	work, err := core.TempDir(input)
	if err != nil {
		return errRow(filepath.Base(input), mode, "ERROR: cannot create temp dir in "+dir), true, nil
	}
	p := &Processor{E: e, B: e.Img, Tag: tagOf(input)}

	var insrc []string
	if ss != "" {
		insrc = []string{"-ss", ss, "-i", input}
	} else {
		insrc = []string{"-i", input}
	}
	var durargs []string
	if t != "" {
		durargs = []string{"-t", t}
	}
	scalevf := ""
	if c.AnimWidth > 0 {
		scalevf = fmt.Sprintf(",scale=%d:-1:flags=lanczos", c.AnimWidth)
	}

	frameDir := filepath.Join(work, "frames")
	if err := os.MkdirAll(frameDir, 0o755); err != nil {
		return result.Row{}, false, err
	}

	trimDesc := ""
	if trimSuffix != "" {
		trimDesc = fmt.Sprintf(", trim %s..%ss", ss, en)
	}
	via := ""
	if c.ImgBackend != "magick" {
		via = " via " + c.ImgBackend
	}
	widthDesc := ""
	if c.AnimWidth > 0 {
		widthDesc = fmt.Sprintf(" (width %d)", c.AnimWidth)
	}
	clip, _ := strconv.ParseFloat(clipDur, 64)
	e.Log.Info("input : %s (%s%s) -> %sfps %s%s%s",
		filepath.Base(input), res, trimDesc, c.AnimFPSRaw, c.ImgFmt, widthDesc, via)

	args := append([]string{}, insrc...)
	args = append(args, durargs...)
	args = append(args, "-map", "0:V:0", "-an", "-sn",
		"-vf", fmt.Sprintf("fps=%s%s", c.AnimFPSRaw, scalevf),
		filepath.Join(frameDir, "f-%06d.png"))
	if err := e.R.Run(fmt.Sprintf("extract: frames @ %sfps", c.AnimFPSRaw),
		clip, filepath.Join(work, "extract.log"), args...); err != nil {
		return result.Row{}, false, err
	}
	refN := countFrames(frameDir)
	if refN == 0 {
		return errRow(filepath.Base(input), mode, "ERROR: no frames extracted"), true, nil
	}

	delayCS := int(math.Round(100 / c.AnimFPS))
	if delayCS < 1 {
		delayCS = 1
	}
	delayMS := delayCS * 10
	frames, _ := filepath.Glob(filepath.Join(frameDir, "f-*.png"))
	sortStrings(frames)
	delays := make([]int, len(frames))
	for i := range delays {
		delays[i] = delayMS
	}

	encode := func(q int, dst string) (string, error) {
		elog := filepath.Join(work, "magick_err.log")
		_ = os.Remove(elog)
		return p.B.Encode(q, dst, frames, delays, elog)
	}
	measure := func(cand string) (float64, error) {
		distDir := filepath.Join(work, "dist_frames")
		_ = os.RemoveAll(distDir)
		elog := filepath.Join(work, "vmaf_nat_dist.err")
		if err := p.B.DecodeFrames(cand, distDir, refN, elog); err != nil {
			showTail(elog)
			e.Log.Append("--- " + p.B.Name + " frame decode (tail) ---")
			e.Log.Append(logx.Tail(elog, 20))
			return 0, core.Dief("%s could not decode all %d frames of %s for the VMAF gate",
				p.B.Name, refN, filepath.Base(cand))
		}
		mean, seen, refSeen, err := vmafAnim(e, distDir, frameDir,
			filepath.Join(work, "vmaf_anim.json"), filepath.Join(work, "vmaf_anim.err"))
		if err != nil {
			showTail(filepath.Join(work, "vmaf_anim.err"))
			e.Log.Append("--- libvmaf (video->anim) stderr (tail) ---")
			e.Log.Append(logx.Tail(filepath.Join(work, "vmaf_anim.err"), 20))
			return 0, core.Dief("libvmaf failed on %s vs extracted frames", filepath.Base(cand))
		}
		distN := countFrames(distDir)
		if distN != refSeen {
			e.Log.Info("warning: encode has %d frames but %d were extracted from the source — VMAF only covers the overlap", distN, refSeen)
		} else if seen != refSeen {
			e.Log.Info("warning: vmaf compared %d frames but %d were extracted", seen, refSeen)
		}
		_ = os.RemoveAll(distDir)
		return mean, nil
	}

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
	e.Log.Info("final : q=%d vmaf=%s size=%s", best, util.Float(mean), util.HumanSize(size))
	passed := util.AtLeast(mean, c.TargetVMAF)
	if c.QAuto && e.QState != nil {
		if passed {
			e.QState.Record(best)
		} else if found.Best < 0 {
			e.QState.RecordFailure()
		}
	}
	if err := os.Rename(final, out); err != nil {
		return result.Row{}, false, err
	}

	if !c.NoJSON {
		doc := &report.VideoToImage{
			Input: input, Output: out, Mode: "video-to-image",
			Encoder: p.B.Name + "/" + c.ImgFmt, Quality: best,
			TargetVMAF: report.F(c.TargetVMAF), MeanVMAF: report.F(mean),
			Passed: passed, SourceDimensions: res, AnimFPS: report.F(c.AnimFPS),
			Animated: true, Frames: refN,
			Trim:      report.Trim{Start: ss, End: en, Duration: clipDur},
			SizeBytes: size,
		}
		if c.AnimWidth > 0 {
			w := c.AnimWidth
			doc.AnimWidth = &w
		}
		switch c.ImgFmt {
		case "webp":
			v := c.WebPMethod
			doc.WebPMethod = &v
		case "avif":
			v := c.AVIFSpeed
			doc.AvifSpeed = &v
			if c.ImgBackend == "libavif" {
				doc.AvifYUV = c.AvifYUV
			}
		case "jxl":
			v := c.JXLEffort
			doc.JXLEffort = &v
		}
		if err := report.Write(rep, doc); err != nil {
			return result.Row{}, false, err
		}
	}

	status := "PASS"
	failed := false
	if !passed {
		status = "FAIL (gate)"
		failed = true
	}
	return result.Row{
		Output: filepath.Base(out), Mode: c.ImgFmt, Q: strconv.Itoa(best),
		VMAF:   util.Float(mean),
		Size:   util.HumanSize(inSize) + "->" + util.HumanSize(size),
		Status: status,
	}, failed, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
