package video

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mins/internal/core"
	"mins/internal/escape"
	"mins/internal/report"
	"mins/internal/result"
	"mins/internal/tui"
	"mins/internal/util"
	"mins/internal/vmaf"
)

type fbfCtx struct {
	e        *core.Env
	input    string
	frameDir string
	outDir   string
	resDir   string
	tmpDir   string
	work     string
	encoder  string
	crfMin   int
	crfMax   int
	pixFmt   string
	target   float64
	srcW     int
	srcH     int
	fps      float64
	threads  int
	refN     int
	qlab     string
	tag      string
}

// ProcessFBF is the --fbf frame-by-frame VMAF-gated path.
func ProcessFBF(e *core.Env, input string) (result.Row, bool, error) {
	c := e.C
	if _, err := os.Stat(input); err != nil {
		return errRow(filepath.Base(input), "fbf", "ERROR: not found"), true, nil
	}
	dir, err := core.AbsDir(input)
	if err != nil {
		return errRow(filepath.Base(input), "fbf", "ERROR: cannot enter "+filepath.Dir(input)), true, nil
	}
	base := util.InStem(input)
	p, err := probeInput(e, input)
	if err != nil || p.DurStr == "" || !reDur.MatchString(p.DurStr) {
		res, dur := "", ""
		if p != nil {
			res, dur = p.Res, p.DurStr
		}
		return errRow(filepath.Base(input), "fbf",
			fmt.Sprintf("ERROR: ffprobe failed / invalid metadata (RES='%s' DUR='%s')", res, dur)), true, nil
	}
	if p.FPS <= 0 {
		return errRow(filepath.Base(input), "fbf", "ERROR: could not determine source frame rate (required for --fbf)"), true, nil
	}

	tw, err := computeTrim(e, &trimInputs{
		Start: c.StartRaw, End: c.EndRaw, StartFloat: c.Start, EndFloat: c.End,
		DurStr: p.DurStr, Dur: p.Dur,
	})
	if err != nil {
		return errRow(filepath.Base(input), "fbf", "ERROR: "+err.Error()), true, nil
	}

	trimSuffix := ""
	if tw.Trimmed {
		trimSuffix = fmt.Sprintf(".trim%s-%s", tw.SS, tw.EN)
	}
	out := filepath.Join(dir, base+trimSuffix+".fbf.av1."+c.VidFmt)
	rep := filepath.Join(dir, base+trimSuffix+".fbf.vmaf.json")
	if c.SkipExisting {
		if _, err := os.Stat(out); err == nil {
			e.Log.Info("skipping %s: output already exists", filepath.Base(input))
			return errRow(filepath.Base(out), "fbf", "SKIPPED (exists)"), false, nil
		}
	}

	work, err := core.TempDir(input)
	if err != nil {
		return errRow(filepath.Base(input), "fbf", "ERROR: cannot create temp dir in "+dir), true, nil
	}

	st := &fileState{e: e, input: input, dir: dir, base: base, p: p, tw: tw,
		out: out, report: rep, work: work}
	if tw.Trimmed {
		st.insrc = []string{"-ss", tw.SS, "-i", input}
	} else {
		st.insrc = []string{"-i", input}
	}
	if tw.T != "" {
		st.durargs = []string{"-t", tw.T}
	}
	if err := resolveTracks(st); err != nil {
		return result.Row{}, false, err
	}

	adesc := audioDesc(p.Audio, st.sel.AKeep, st.sel.KEPTDesc)
	sdesc := subDesc(p.Subs, st.subSel.SKeep, st.subSel.Desc)
	trimDesc := ""
	if tw.Trimmed {
		trimDesc = fmt.Sprintf(", trim %s..%ss", tw.SS, tw.EN)
	}
	e.Log.Info("input : %s (%s, %s)%s%s [frame-by-frame]", filepath.Base(input), p.Res, adesc, sdesc, trimDesc)
	e.Log.Info("output: %s (av1/%s, audio: %s, every frame individually VMAF-gated)", filepath.Base(out), c.VidFmt, st.acodec)

	chunkFrames := int(c.FBFChunkSeconds*p.FPS + 0.5)
	if chunkFrames < 1 {
		chunkFrames = 1
	}
	est := 0
	if !tw.Trimmed && p.NbFrames > 0 {
		est = p.NbFrames
	}
	if est <= 0 {
		est = int(tw.Clip*p.FPS + 0.5)
	}
	if est <= 0 {
		return errRow(filepath.Base(input), "fbf", "ERROR: could not estimate a frame count from duration*fps"), true, nil
	}

	threads := c.NProc / c.Workers
	if threads < 1 {
		threads = 1
	}
	logSuffix := ""
	if e.Log.Enabled() {
		logSuffix = fmt.Sprintf("; per-frame encode/VMAF detail -> %s and %s/mins-logs/", e.Log.Path, e.Log.Dir)
	}
	e.Log.Info("fbf: encoding ~%d frame(s) across %d instance(s) (~%d thread(s) each) in chunks of ~%d frame(s) (~%ss each, kept on disk one chunk at a time) — this is much slower than the normal path%s",
		est, c.Workers, threads, chunkFrames, util.Float(c.FBFChunkSeconds), logSuffix)

	frameDir := filepath.Join(work, "frames")
	outDir := filepath.Join(work, "enc")
	resDir := filepath.Join(work, "results")
	tmpDir := filepath.Join(work, "tmp")
	for _, d := range []string{frameDir, outDir, resDir, tmpDir} {
		_ = os.MkdirAll(d, 0o755)
	}

	dash := e.TUI
	if dash == nil {
		dash = tui.New(!c.NoTUI && tui.IsTerminal(os.Stderr), "frame-by-frame", filepath.Base(out), est, c.Workers)
		dash.Start()
		defer dash.Close()
	}

	ctx := &fbfCtx{e: e, input: input, frameDir: frameDir, outDir: outDir, resDir: resDir,
		tmpDir: tmpDir, work: work, encoder: c.Encoder, crfMin: c.CRFMin, crfMax: c.CRFMax,
		pixFmt: c.PixFmt, target: c.TargetVMAF, srcW: p.W, srcH: p.H, fps: p.FPS,
		threads: threads, refN: est, qlab: c.QLab, tag: tagOf(input)}

	stop := make(chan struct{})
	progDone := make(chan struct{})
	go fbfProgress(e, resDir, est, dash, stop, progDone)

	refN := 0
	chunkStart := 0
	for chunkStart < est {
		chunkN := est - chunkStart
		if chunkN > chunkFrames {
			chunkN = chunkFrames
		}
		chunkSS := 0.0
		if tw.SS != "" {
			chunkSS, _ = strconv.ParseFloat(tw.SS, 64)
		}
		chunkSS += float64(chunkStart) / p.FPS
		chunkLo := chunkStart + 1
		args := []string{"-hide_banner", "-nostdin", "-y", "-nostats", "-loglevel", "error",
			"-ss", fmt.Sprintf("%.6f", chunkSS), "-i", input, "-frames:v", strconv.Itoa(chunkN),
			"-map", "0:V:0", "-an", "-sn", "-start_number", strconv.Itoa(chunkLo),
			filepath.Join(frameDir, "f-%06d.png")}
		outb, err := exec.Command(e.R.FFmpeg, args...).CombinedOutput()
		if err != nil {
			_ = os.WriteFile(filepath.Join(work, "extract_chunk.log"), outb, 0o644)
			close(stop)
			<-progDone
			return result.Row{}, false, core.Dief("fbf: frame extraction failed for the chunk starting at frame %d (see %s)", chunkLo, filepath.Join(work, "extract_chunk.log"))
		}
		got := 0
		for k := chunkLo; k < chunkLo+chunkN; k++ {
			if _, err := os.Stat(filepath.Join(frameDir, fmt.Sprintf("f-%06d.png", k))); err != nil {
				break
			}
			got++
		}
		if got < chunkN && chunkStart+chunkN < est {
			e.Log.Info("warning: fbf chunk at frame %d: expected %d frame(s), got %d - possible VFR source or early EOF; stopping extraction here", chunkLo, chunkN, got)
		}
		if got > 0 {
			runFrameWorkers(ctx, chunkLo, chunkLo+got-1, got)
			refN += got
			for k := chunkLo; k < chunkLo+got; k++ {
				_ = os.Remove(filepath.Join(frameDir, fmt.Sprintf("f-%06d.png", k)))
			}
		}
		if got < chunkN {
			break
		}
		chunkStart += chunkN
	}

	close(stop)
	<-progDone
	if dash.Enabled() {
		dash.SetTotal(refN)
	}

	if refN == 0 {
		return errRow(filepath.Base(input), "fbf", "ERROR: no frames extracted"), true, nil
	}

	// Count frame errors.
	var errFiles []string
	entries, _ := os.ReadDir(resDir)
	for _, ent := range entries {
		if strings.HasSuffix(ent.Name(), ".error") {
			errFiles = append(errFiles, ent.Name())
		}
	}
	if len(errFiles) > 0 {
		sort.Strings(errFiles)
		first := errFiles[0]
		msg, _ := os.ReadFile(filepath.Join(resDir, first))
		e.Log.Info("warning: %d frame(s) failed — first: %s: %s", len(errFiles), first, strings.TrimSpace(string(msg)))
		return errRow(filepath.Base(input), "fbf", fmt.Sprintf("ERROR: %d frame(s) failed to encode", len(errFiles))), true, nil
	}

	// Concatenate the per-frame encodes.
	concat := filepath.Join(work, "concat.txt")
	frameDur := fmt.Sprintf("%.6f", 1/p.FPS)
	var b strings.Builder
	for i := 1; i <= refN; i++ {
		f := filepath.Join(outDir, fmt.Sprintf("f-%06d.mkv", i))
		if _, err := os.Stat(f); err != nil {
			return result.Row{}, false, core.Dief("fbf: missing encoded frame %d (%s) after a clean worker run", i, f)
		}
		fmt.Fprintf(&b, "file '%s'\nduration %s\n", escape.Concat(f), frameDur)
	}
	if err := os.WriteFile(concat, []byte(b.String()), 0o644); err != nil {
		return result.Row{}, false, err
	}
	assembled := filepath.Join(work, "assembled.mkv")
	if err := e.R.Run(fmt.Sprintf("assemble: concat %d frames", refN), tw.Clip,
		filepath.Join(work, "concat.log"),
		"-f", "concat", "-safe", "0", "-i", concat, "-c:v", "copy", "-an", "-sn",
		"-r", util.Float(p.FPS), "-f", "matroska", assembled); err != nil {
		return result.Row{}, false, err
	}

	// Remux the Master's audio/subtitles back in.
	args := append([]string{}, st.durargs...)
	args = append(args, st.insrc...)
	args = append(args, "-i", assembled, "-map", "1:v:0")
	args = append(args, st.amap...)
	args = append(args, st.smap...)
	args = append(args, "-c:v", "copy")
	args = append(args, st.aargs...)
	args = append(args, st.sargs...)
	args = append(args, muxArgs(e)...)
	args = append(args, out+".part")
	if err := e.R.Run("mux: assembled video + audio/subs", tw.Clip, filepath.Join(work, "mux.log"), args...); err != nil {
		return result.Row{}, false, err
	}

	size := fileSize(out + ".part")
	inSize := fileSize(input)

	// Summarize the per-frame result files.
	type frameRow struct {
		Frame         int     `json:"frame"`
		CRF           int     `json:"crf"`
		VMAF          float64 `json:"vmaf"`
		SizeBytes     int64   `json:"size_bytes"`
		TargetReached bool    `json:"target_reached"`
	}
	var rows []frameRow
	entries, _ = os.ReadDir(resDir)
	for _, ent := range entries {
		if !strings.HasSuffix(ent.Name(), ".ok") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(resDir, ent.Name()))
		if err != nil {
			continue
		}
		parts := strings.Split(strings.TrimSpace(string(data)), "\t")
		if len(parts) != 4 {
			continue
		}
		i, _ := strconv.Atoi(parts[0])
		crf, _ := strconv.Atoi(parts[1])
		vm, _ := strconv.ParseFloat(parts[2], 64)
		sz, _ := strconv.ParseInt(parts[3], 10, 64)
		rows = append(rows, frameRow{Frame: i, CRF: crf, VMAF: vm, SizeBytes: sz})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Frame < rows[j].Frame })
	if len(rows) != refN {
		return errRow(filepath.Base(input), "fbf", "ERROR: frame-count mismatch while summarizing results"), true, nil
	}
	minV, meanV, crfLo, crfHi := rows[0].VMAF, 0.0, rows[0].CRF, rows[0].CRF
	worst := 0
	var below []int
	for i := range rows {
		rows[i].TargetReached = rows[i].VMAF >= c.TargetVMAF
		meanV += rows[i].VMAF
		if rows[i].VMAF < minV {
			minV = rows[i].VMAF
			worst = i
		}
		if rows[i].CRF < crfLo {
			crfLo = rows[i].CRF
		}
		if rows[i].CRF > crfHi {
			crfHi = rows[i].CRF
		}
		if !rows[i].TargetReached {
			below = append(below, rows[i].Frame)
		}
	}
	meanV /= float64(len(rows))
	passed := minV >= c.TargetVMAF

	if !c.NoJSON {
		var outRows []report.FBFFrame
		for _, r := range rows {
			outRows = append(outRows, report.FBFFrame{
				Frame: r.Frame, CRF: r.CRF, VMAF: report.F(r.VMAF),
				SizeBytes: r.SizeBytes, TargetReached: r.TargetReached,
			})
		}
		doc := &report.FBF{
			Input: input, Output: out, Mode: "frame-by-frame", Encoder: c.Encoder,
			Container: c.VidFmt, TargetVMAF: report.F(c.TargetVMAF),
			MinVMAF: report.F(minV), MeanVMAF: report.F(meanV), Passed: passed,
			CRFRange:   report.CRFRange{Min: crfLo, Max: crfHi},
			Trim:       report.Trim{Start: tw.SS, End: tw.EN, Duration: tw.ClipDur},
			FrameCount: refN, FramesBelowTarget: len(below), FramesBelowTargetList: below,
			WorstFrame: report.FBFFrame{
				Frame: rows[worst].Frame, CRF: rows[worst].CRF, VMAF: report.F(rows[worst].VMAF),
				SizeBytes: rows[worst].SizeBytes, TargetReached: rows[worst].TargetReached,
			},
			SizeBytes: size, Frames: outRows,
		}
		if doc.FramesBelowTargetList == nil {
			doc.FramesBelowTargetList = []int{}
		}
		if err := report.Write(rep, doc); err != nil {
			return result.Row{}, false, err
		}
	}

	if err := os.Rename(out+".part", out); err != nil {
		return result.Row{}, false, err
	}
	e.Log.Info("final : %s=%d-%d min_vmaf=%s mean_vmaf=%s size=%s",
		c.QLab, crfLo, crfHi, fmtFloat4(minV), fmtFloat4(meanV), util.HumanSize(size))
	if len(below) > 0 {
		extra := ""
		if !c.NoJSON {
			extra = fmt.Sprintf(" -- see \"frames_below_target_list\" in %s", filepath.Base(rep))
		}
		e.Log.Info("fbf: %d/%d frame(s) never reached target %s even at their own best crf; worst is frame %d (crf=%d, vmaf=%s). If those frames are already at crf=%d this is likely a genuine content-driven VMAF ceiling (common on flat-color/line-art animation), not a bug%s",
			len(below), refN, c.TargetRaw, rows[worst].Frame, rows[worst].CRF,
			fmtFloat4(minV), c.CRFMin, extra)
	}

	status := "PASS"
	failed := false
	if !passed {
		status = "FAIL (gate)"
		failed = true
	}
	return result.Row{
		Output: filepath.Base(out), Mode: "fbf",
		Q:      fmt.Sprintf("%d-%d", crfLo, crfHi),
		VMAF:   fmtFloat4(minV),
		Size:   util.HumanSize(inSize) + "->" + util.HumanSize(size),
		Status: status,
	}, failed, nil
}

func runFrameWorkers(ctx *fbfCtx, lo, hi, got int) {
	workers := ctx.e.C.Workers
	if workers > got {
		workers = got
	}
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				fbfOneFrame(ctx, i)
			}
		}()
	}
	for i := lo; i <= hi; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}

func fbfOneFrame(ctx *fbfCtx, i int) {
	e := ctx.e
	num := fmt.Sprintf("%06d", i)
	ref := filepath.Join(ctx.frameDir, "f-"+num+".png")
	fw := filepath.Join(ctx.tmpDir, strconv.Itoa(i))
	_ = os.MkdirAll(fw, 0o755)
	errlog := filepath.Join(fw, "err.log")
	reportLine := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		fmt.Fprintf(os.Stderr, "  [mins] frame %d/%d: %s\n", i, ctx.refN, msg)
		e.Log.Append(fmt.Sprintf("[mins] frame %d/%d: %s", i, ctx.refN, msg))
		e.TUI.Message("frame %d/%d: %s", i, ctx.refN, msg)
	}
	copyLog := func() {
		if !e.Log.Enabled() {
			return
		}
		data, err := os.ReadFile(errlog)
		if err != nil || len(data) == 0 {
			return
		}
		dst := filepath.Join(e.Log.Dir, "mins-logs", e.Log.RunID+"_"+ctx.tag+"_frame"+num+".log")
		_ = os.WriteFile(dst, data, 0o644)
	}
	fail := func(msg string) {
		_ = os.WriteFile(filepath.Join(ctx.resDir, strconv.Itoa(i)+".error"), []byte(msg), 0o644)
		reportLine("ERROR: %s", msg)
		copyLog()
	}
	encode := func(crf int, out string) error {
		args := []string{"-hide_banner", "-nostdin", "-y", "-nostats", "-loglevel", "error",
			"-i", ref, "-frames:v", "1", "-r", util.Float(ctx.fps),
			"-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2:0:0:color=black"}
		args = append(args, encoderArgs(e, crf, "quality")...)
		args = append(args, "-threads", strconv.Itoa(ctx.threads), "-pix_fmt", ctx.pixFmt,
			"-f", "matroska", out)
		outb, err := exec.Command(e.R.FFmpeg, args...).CombinedOutput()
		appendLine(errlog, string(outb))
		return err
	}
	measure := func(cand string) (float64, error) {
		vlog := filepath.Join(fw, "vmaf.json")
		filter := fmt.Sprintf("[0:v]crop=%d:%d:0:0,format=yuv420p,setpts=PTS-STARTPTS[d];"+
			"[1:v]format=yuv420p,setpts=PTS-STARTPTS[r];[d][r]libvmaf=n_threads=%d:log_path=%s:log_fmt=json",
			ctx.srcW, ctx.srcH, ctx.threads, escape.Filter(vlog))
		args := []string{"-hide_banner", "-nostdin", "-y", "-nostats", "-loglevel", "error",
			"-i", cand, "-i", ref, "-lavfi", filter, "-f", "null", "-"}
		outb, err := exec.Command(e.R.FFmpeg, args...).CombinedOutput()
		appendLine(errlog, string(outb))
		if err != nil {
			return 0, err
		}
		lg, err := vmaf.Parse(vlog)
		if err != nil {
			return 0, err
		}
		mean, _ := lg.Mean()
		return mean, nil
	}

	lo, hi := ctx.crfMin, ctx.crfMax
	best := -1
	for lo <= hi {
		mid := (lo + hi) / 2
		cand := filepath.Join(fw, fmt.Sprintf("search_%d.mkv", mid))
		if err := encode(mid, cand); err != nil {
			fail(fmt.Sprintf("encode failed at crf=%d (see %s)", mid, errlog))
			return
		}
		mean, err := measure(cand)
		if err != nil {
			fail(fmt.Sprintf("libvmaf failed at crf=%d (see %s)", mid, errlog))
			return
		}
		verdict := "FAIL"
		if mean >= ctx.target {
			verdict = "PASS"
			best = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
		reportLine("crf=%d (search): vmaf=%s target=%s %s", mid, fmtMean(mean), ctx.e.C.TargetRaw, verdict)
		_ = os.Remove(cand)
	}
	if best < 0 {
		best = ctx.crfMin
		reportLine("WARNING: no crf in [%d,%d] reached target %s; using crf=%d (best effort — could be a genuine content-driven VMAF ceiling for this frame rather than a bug; see CONTEXT.md)",
			ctx.crfMin, ctx.crfMax, ctx.e.C.TargetRaw, best)
	}

	final := filepath.Join(ctx.outDir, "f-"+num+".mkv")
	if err := encode(best, final); err != nil {
		fail(fmt.Sprintf("final encode failed at crf=%d (see %s)", best, errlog))
		return
	}
	mean, err := measure(final)
	if err != nil {
		fail(fmt.Sprintf("final libvmaf failed at crf=%d (see %s)", best, errlog))
		return
	}
	size := fileSize(final)
	verdict := "FAIL"
	if mean >= ctx.target {
		verdict = "PASS"
	}
	reportLine("crf=%d (final): vmaf=%s target=%s %s size=%s", best, fmtMean(mean), ctx.e.C.TargetRaw, verdict, util.HumanSize(size))
	_ = os.WriteFile(filepath.Join(ctx.resDir, strconv.Itoa(i)+".ok"),
		[]byte(fmt.Sprintf("%d\t%d\t%s\t%d\n", i, best, fmtMean(mean), size)), 0o644)
	copyLog()
	_ = os.RemoveAll(fw)
}

func fbfProgress(e *core.Env, resDir string, total int, dash *tui.Dashboard, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	start := time.Now()
	lastDecile, lastErr := -1, 0
	count := func() (int, int) {
		ok, errs := 0, 0
		ents, _ := os.ReadDir(resDir)
		for _, ent := range ents {
			if strings.HasSuffix(ent.Name(), ".ok") {
				ok++
			} else if strings.HasSuffix(ent.Name(), ".error") {
				errs++
			}
		}
		return ok, errs
	}
	report := func() {
		ok, errs := count()
		n := ok + errs
		dash.Progress(n, errs)
		decile := n * 10 / total
		if n > 0 && (decile > lastDecile || errs > lastErr) {
			pct := n * 100 / total
			elapsed := int(time.Since(start).Seconds())
			if errs > 0 {
				e.Log.Info("fbf: %d/%d frames done (%d%%, %ds elapsed) - %d error(s) so far", n, total, pct, elapsed, errs)
			} else {
				e.Log.Info("fbf: %d/%d frames done (%d%%, %ds elapsed)", n, total, pct, elapsed)
			}
			if decile > lastDecile {
				lastDecile = decile
			}
			lastErr = errs
		}
	}
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			ok, errs := count()
			n := ok + errs
			if n > 0 && lastDecile < 10 {
				elapsed := int(time.Since(start).Seconds())
				if errs > 0 {
					e.Log.Info("fbf: %d/%d frames done (100%%, %ds elapsed) - %d error(s) so far", n, n, elapsed, errs)
				} else {
					e.Log.Info("fbf: %d/%d frames done (100%%, %ds elapsed)", n, n, elapsed)
				}
			}
			dash.Progress(n, errs)
			return
		case <-t.C:
			report()
		}
	}
}

func fmtFloat4(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }
