package video

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"mins/internal/audio"
	"mins/internal/core"
	"mins/internal/report"
	"mins/internal/result"
	"mins/internal/subtitle"
	"mins/internal/util"
)

// fileState carries everything one video file's paths share between the
// usual encode and the --refine pass.
type fileState struct {
	e     *core.Env
	input string
	dir   string
	base  string
	p     *probe
	tw    *trimWindow

	insrc   []string
	durargs []string

	sel    *audio.Selection
	subSel *subtitle.Selection
	amap   []string
	smap   []string
	aargs  []string
	sargs  []string
	acodec string

	out    string
	report string
	work   string
	vlog   string

	finalCRF int
	mean     float64
	size     int64
	passed   bool

	modeCol string
	qCol    string
}

func errRow(name, mode, msg string) result.Row {
	return result.Row{Output: name, Mode: mode, Q: "-", VMAF: "-", Size: "-", Status: msg}
}

// ProcessFile is the usual whole-clip AV1 path.
func ProcessFile(e *core.Env, input string) (result.Row, bool, error) {
	c := e.C
	if _, err := os.Stat(input); err != nil {
		return errRow(filepath.Base(input), "av1", "ERROR: not found"), true, nil
	}
	dir, err := core.AbsDir(input)
	if err != nil {
		return errRow(filepath.Base(input), "av1", "ERROR: cannot enter "+filepath.Dir(input)), true, nil
	}
	base := util.InStem(input)
	p, err := probeInput(e, input)
	if err != nil || p.DurStr == "" || !reDur.MatchString(p.DurStr) {
		res, dur := "", ""
		if p != nil {
			res, dur = p.Res, p.DurStr
		}
		return errRow(filepath.Base(input), "av1",
			fmt.Sprintf("ERROR: ffprobe failed / invalid metadata (RES='%s' DUR='%s')", res, dur)), true, nil
	}

	tw, err := computeTrim(e, &trimInputs{
		Start: c.StartRaw, End: c.EndRaw,
		StartFloat: c.Start, EndFloat: c.End,
		DurStr: p.DurStr, Dur: p.Dur,
	})
	if err != nil {
		return errRow(filepath.Base(input), "av1", "ERROR: "+err.Error()), true, nil
	}

	trimSuffix := ""
	if tw.Trimmed {
		trimSuffix = fmt.Sprintf(".trim%s-%s", tw.SS, tw.EN)
	}
	out := filepath.Join(dir, base+trimSuffix+".av1."+c.VidFmt)
	rep := filepath.Join(dir, base+trimSuffix+".vmaf.json")

	if c.SkipExisting {
		if _, err := os.Stat(out); err == nil {
			e.Log.Info("skipping %s: output already exists", filepath.Base(input))
			return errRow(filepath.Base(out), "av1", "SKIPPED (exists)"), false, nil
		}
	}

	work, err := core.TempDir(input)
	if err != nil {
		return errRow(filepath.Base(input), "av1", "ERROR: cannot create temp dir in "+dir), true, nil
	}

	st := &fileState{e: e, input: input, dir: dir, base: base, p: p, tw: tw,
		out: out, report: rep, work: work, vlog: filepath.Join(work, "vmaf.json")}
	if tw.Trimmed {
		st.insrc = []string{"-ss", tw.SS, "-i", input}
	} else {
		st.insrc = []string{"-i", input}
	}
	if tw.T != "" {
		st.durargs = []string{"-t", tw.T}
	}

	// Track/bundle resolution failures are fatal in mins.sh (die), not a
	// per-file summary row.
	if err := resolveTracks(st); err != nil {
		return result.Row{}, false, err
	}

	adesc := audioDesc(p.Audio, st.sel.AKeep, st.sel.KEPTDesc)
	sdesc := subDesc(p.Subs, st.subSel.SKeep, st.subSel.Desc)
	trimDesc := ""
	if tw.Trimmed {
		trimDesc = fmt.Sprintf(", trim %s..%ss", tw.SS, tw.EN)
	}
	e.Log.Info("input : %s (%s, %s)%s%s", filepath.Base(input), p.Res, adesc, sdesc, trimDesc)
	subout := ""
	if p.Subs > 0 && st.subSel.SKeep > 0 {
		subout = ", subs: " + subPolicy(c.VidFmt)
	}
	e.Log.Info("output: %s (av1/%s, audio: %s%s)", filepath.Base(out), c.VidFmt, st.acodec, subout)

	qlab := c.QLab
	lo, hi := c.CRFMin, c.CRFMax
	best := -1
	for lo <= hi {
		mid := (lo + hi) / 2
		cand := filepath.Join(work, fmt.Sprintf("search_%d.%s", mid, c.VidFmt))
		if err := st.encode(mid, "fast", cand); err != nil {
			return result.Row{}, false, err
		}
		mean, n, err := st.measure(cand, filepath.Join(work, "vmaf_run.log"), "verify : libvmaf")
		if err != nil {
			return result.Row{}, false, core.Dief("libvmaf ran but produced no readable VMAF log")
		}
		checkExpectedFrames(e, n, expectedFrames(tw.Clip, p.FPS), p.FPSInt)
		size := fileSize(cand)
		verdict := "FAIL"
		if mean >= c.TargetVMAF {
			verdict = "PASS"
			best = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
		e.Log.Info("search: %s=%d vmaf=%s size=%s %s", qlab, mid, fmtMean(mean), util.HumanSize(size), verdict)
		e.TUI.Attempt("search", mid, mean, c.TargetVMAF, verdict)
		_ = os.Remove(cand)
	}
	if best < 0 {
		best = c.CRFMin
		e.Log.Info("warning: no %s in [%d..%d] reached VMAF %s on the fast tier; falling back to %s=%d",
			strings.ToUpper(qlab), c.CRFMin, c.CRFMax, c.TargetRaw, qlab, best)
	}

	inSize := fileSize(input)
	finalCRF := best
	tries := 0
	for {
		if err := st.encode(finalCRF, "quality", out+".part"); err != nil {
			return result.Row{}, false, err
		}
		mean, n, err := st.measure(out+".part", filepath.Join(work, "vmaf_run.log"), "verify : libvmaf")
		if err != nil {
			return result.Row{}, false, core.Dief("libvmaf ran but produced no readable VMAF log")
		}
		checkExpectedFrames(e, n, expectedFrames(tw.Clip, p.FPS), p.FPSInt)
		size := fileSize(out + ".part")
		verdict := "FAIL"
		if mean >= c.TargetVMAF {
			verdict = "PASS"
			st.passed = true
			st.mean, st.size, st.finalCRF = mean, size, finalCRF
			e.Log.Info("final : %s=%d vmaf=%s size=%s %s", qlab, finalCRF, fmtMean(mean), util.HumanSize(size), verdict)
			e.TUI.Attempt("final", finalCRF, mean, c.TargetVMAF, verdict)
			break
		}
		st.passed = false
		st.mean, st.size, st.finalCRF = mean, size, finalCRF
		e.Log.Info("final : %s=%d vmaf=%s size=%s %s", qlab, finalCRF, fmtMean(mean), util.HumanSize(size), verdict)
		e.TUI.Attempt("final", finalCRF, mean, c.TargetVMAF, verdict)
		if finalCRF <= c.QPHardMin {
			break
		}
		tries++
		if tries >= 3 {
			break
		}
		finalCRF -= 2
		if finalCRF < c.QPHardMin {
			finalCRF = c.QPHardMin
		}
	}
	if err := os.Rename(out+".part", out); err != nil {
		return result.Row{}, false, err
	}

	var refineBlock *report.Refine
	if c.Refine {
		rf, err := runRefine(st)
		if err != nil {
			return result.Row{}, false, err
		}
		if rf != nil {
			refineBlock = rf.Report
			st.mean, st.size, st.passed = rf.Mean, rf.Size, rf.Passed
			st.modeCol, st.qCol = rf.ModeCol, rf.QCol
		}
	}

	if !c.NoJSON {
		if err := writeVideoReport(st, refineBlock); err != nil {
			return result.Row{}, false, err
		}
	}

	status := "PASS"
	failed := false
	if !st.passed {
		status = "FAIL (gate)"
		failed = true
		e.TUI.Message("did not reach target VMAF %s (best %s=%d, vmaf=%s)",
			c.TargetRaw, qlab, st.finalCRF, fmtMean(st.mean))
	}
	mode := st.modeCol
	if mode == "" {
		mode = "av1"
	}
	q := st.qCol
	if q == "" {
		q = strconv.Itoa(st.finalCRF)
	}
	row := result.Row{
		Output: filepath.Base(out), Mode: mode, Q: q,
		VMAF:   fmtMean(st.mean),
		Size:   util.HumanSize(inSize) + "->" + util.HumanSize(st.size),
		Status: status,
	}
	return row, failed, nil
}

func resolveTracks(st *fileState) error {
	e := st.e
	sel, err := e.Audio.ResolveAudio(st.input)
	if err != nil {
		return err
	}
	st.sel = sel
	ca, err := e.Audio.ResolveACodec(st.input, sel)
	if err != nil {
		return err
	}
	st.aargs = ca.Args
	st.acodec = ca.Desc
	if e.C.AudioSearch {
		sa, err := e.Audio.ResolveSearch(st.input, sel, st.tw.SS, st.tw.T, st.tw.Clip, st.work)
		if err != nil {
			return err
		}
		st.aargs = sa.Args
		st.acodec = sa.Desc
	}
	ss, err := subtitle.Resolve(e.R, e.Log, e.C, st.input)
	if err != nil {
		return err
	}
	st.subSel = ss
	st.smap = ss.SMAP
	st.sargs = ss.SARGS
	st.amap = sel.AMap
	return nil
}

func (st *fileState) encode(crf int, tier, out string) error {
	c := st.e.C
	args := append([]string{}, st.insrc...)
	args = append(args, st.durargs...)
	args = append(args, "-map", "0:V:0")
	args = append(args, st.amap...)
	args = append(args, st.smap...)
	args = append(args, "-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2:0:0:color=black")
	args = append(args, encoderArgs(st.e, crf, tier)...)
	args = append(args, "-pix_fmt", c.PixFmt, "-fps_mode", "passthrough")
	args = append(args, st.aargs...)
	args = append(args, st.sargs...)
	args = append(args, muxArgs(st.e)...)
	args = append(args, out)
	label := fmt.Sprintf("encode : %s=%d (%s)", c.QLab, crf, tier)
	logp := filepath.Join(st.work, fmt.Sprintf("enc_%s%d_%s.log", c.QLab, crf, tier))
	return st.e.R.Run(label, st.tw.Clip, logp, args...)
}

func (st *fileState) measure(cand, runLog, label string) (float64, int, error) {
	return vmafRun(st.e, cand, st.insrc, st.durargs, st.p.W, st.p.H,
		st.tw.Clip, runLog, st.vlog, label)
}

func writeVideoReport(st *fileState, refineBlock *report.Refine) error {
	c := st.e.C
	qv := st.finalCRF
	r := &report.Video{
		Input:       st.input,
		Output:      st.out,
		Mode:        "video",
		Encoder:     c.Encoder,
		Container:   c.VidFmt,
		TargetVMAF:  report.F(c.TargetVMAF),
		MeanVMAF:    report.F(st.mean),
		Passed:      st.passed,
		Trim:        report.Trim{Start: st.tw.SS, End: st.tw.EN, Duration: st.tw.ClipDur},
		TierQuality: tierQuality(st.e),
		PixFmt:      c.PixFmt,
		Audio: report.AudioInfo{
			Codec: st.acodec, Bitrate: c.AudioBitrate,
			Tracks: st.p.Audio, Kept: st.sel.AKeep,
			Select: c.ATracks, Resolved: st.sel.KEPTDesc,
		},
		Subtitles: report.SubInfo{
			Policy: subPolicy(c.VidFmt), Select: c.STracks, Streams: st.p.Subs,
			Kept: st.subSel.SKeep, Dropped: st.subSel.SDrop, Resolved: st.subSel.Desc,
		},
		SizeBytes: st.size,
	}
	if c.Encoder == "nvenc" {
		r.CQ = &qv
	} else {
		r.CRF = &qv
	}
	if refineBlock != nil {
		r.Refine = refineBlock
		applied := refineBlock.Applied
		r.Refined = &applied
	}
	return report.Write(st.report, r)
}

// tierQuality reproduces the tier_quality_settings object exactly as
// mins.sh's report embedded it.
func tierQuality(c *core.Env) []byte {
	switch c.C.Encoder {
	case "svt":
		return []byte(fmt.Sprintf(`{"preset": %d, "params": "tune=0"}`, c.C.SVTQuality))
	case "aom":
		return []byte(fmt.Sprintf(`{"cpu-used": %d, "row-mt": 1}`, c.C.AOMQuality))
	default:
		return []byte(fmt.Sprintf(`{"preset": %d, "tune": "hq", "multipass": "fullres", "rc": "vbr", "spatial_aq": 1, "aq_strength": %d}`,
			c.C.NVENCQuality, c.C.NVENCAQStrength))
	}
}
