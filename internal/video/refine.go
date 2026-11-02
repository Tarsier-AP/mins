package video

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"mins/internal/core"
	"mins/internal/escape"
	"mins/internal/media"
	"mins/internal/report"
	"mins/internal/util"
	"mins/internal/vmaf"
)

// refineResult is what a completed (or skipped) refine pass hands back.
type refineResult struct {
	Report  *report.Refine
	ModeCol string
	QCol    string
	Mean    float64
	Size    int64
	Passed  bool
}

type refFrag struct {
	idx      int
	lo, hi   int
	planned  string
	bmean    float64
	crf      int
	mean     float64
	min      float64
	reached  bool
	size     int64
	cand     string
	attempts []report.RefineAttempt
}

type refiner struct {
	st       *fileState
	dir      string
	tsv      []refFrag
	floor    int
	minf     int
	maxf     int
	step     int
	target   float64
	qlab     string
	fps      float64
	fpsNum   int
	fpsDen   int
	pts0     float64
	seekArgs []string
	n        int
	baseMean float64
	baseSize int64
	baseCRF  int
}

var rePTS = regexp.MustCompile(`pts_time:[[:space:]]*(-?[0-9]+([.][0-9]+)?)`)

// runRefine is process_refine: split the usual encode's per-frame VMAF map,
// re-encode the bad fragments at higher quality, merge them, and keep the
// result only when it is at least as good as the usual encode.
func runRefine(st *fileState) (*refineResult, error) {
	e := st.e
	c := e.C
	rfDir := filepath.Join(st.work, "refine")
	if err := os.MkdirAll(rfDir, 0o755); err != nil {
		e.Log.Info("warning: refine: cannot create %s; keeping the usual encode", rfDir)
		return nil, nil
	}
	r := &refiner{
		st: st, dir: rfDir, minf: c.RefineMinFrames, step: c.RefineStep,
		target: c.TargetVMAF, qlab: c.QLab, fps: st.p.FPS,
		fpsNum: st.p.FPSNum, fpsDen: st.p.FPSDen,
		baseMean: st.mean, baseSize: st.size, baseCRF: st.finalCRF,
	}
	r.floor = c.CRFMin
	if r.baseCRF < r.floor {
		r.floor = c.QPHardMin
	}

	skip := func(reason string) (*refineResult, error) {
		e.Log.Info("refine: %s; keeping the usual encode", reason)
		blk := r.buildReport(false, reason, nil, nil)
		return &refineResult{Report: blk, Mean: st.mean, Size: st.size, Passed: st.passed}, nil
	}

	if st.p.FPS <= 0 {
		return skip("source frame rate unknown")
	}
	if fi, err := os.Stat(st.vlog); err != nil || fi.Size() == 0 {
		return skip("no per-frame VMAF log from the usual encode")
	}
	r.maxf = int(c.RefineSeconds*st.p.FPS + 0.5)
	if r.maxf < 1 {
		r.maxf = 1
	}
	if r.maxf < r.minf {
		r.maxf = r.minf
	}

	e.Log.Info("refine: scanning the usual encode (%s=%d, mean vmaf %s) for quality below %s; fragments up to %d frame(s) (~%ss), split down to %d frame(s)",
		r.qlab, r.baseCRF, fmtMean(r.baseMean), r.st.e.C.TargetRaw, r.maxf, c.RefineSecondsRaw, r.minf)

	pk, err := media.PacketCount(e.R, st.out)
	if err != nil || pk < 1 {
		return skip("could not count the video frames of the usual encode")
	}
	lg, err := vmaf.Parse(st.vlog)
	if err != nil {
		return skip("no per-frame VMAF log from the usual encode")
	}
	scores := lg.Scores()
	if len(scores) < pk {
		return skip(fmt.Sprintf("the usual encode's per-frame VMAF log does not cover all of its %d video frame(s) (VFR/timing mismatch?)", pk))
	}
	scores = scores[:pk]
	r.n = pk

	leaves, nbad := r.plan(scores)
	nunder := 0
	lowest := scores[0]
	for _, s := range scores {
		if s < r.target {
			nunder++
		}
		if s < lowest {
			lowest = s
		}
	}
	e.Log.Info("refine: %d frame(s): %d (%d%%) individually below %s, lowest %.2f; split into %d fragment(s): %d meeting the target, %d below it",
		r.n, nunder, nunder*100/r.n, r.st.e.C.TargetRaw, lowest, len(leaves), len(leaves)-nbad, nbad)
	if nbad == 0 {
		return skip(fmt.Sprintf("no fragment is below target %s; no quality issues to repair", r.st.e.C.TargetRaw))
	}

	if !r.probePTS0() {
		e.Log.Info("warning: refine: could not probe the first frame's timestamp; assuming it sits at the start of the clip (the frame-count checks below will catch a misalignment)")
		r.pts0 = 0
		if st.tw.SS != "" {
			r.pts0, _ = strconv.ParseFloat(st.tw.SS, 64)
		}
	}

	// --- encode every fragment, ascending frame order ---
	for idx, leaf := range leaves {
		lo, hi := leaf[0], leaf[1]
		planned := "good"
		if r.meanOf(scores, lo, hi) < r.target {
			planned = "bad"
		}
		frag, err := r.fragment(idx+1, len(leaves), lo, hi, planned, r.meanOf(scores, lo, hi))
		if err != nil {
			r.copyFragmentLog(fmt.Sprintf("%06d-%06d", lo, hi-1))
			return skip(fmt.Sprintf("fragment %d (frames %d-%d) failed", idx+1, lo, hi-1))
		}
		r.tsv = append(r.tsv, *frag)
	}

	// --- merge ---
	e.Log.Info("refine: merging %d fragment(s) in frame order", len(leaves))
	concat := filepath.Join(rfDir, "concat.txt")
	if err := r.writeConcat(concat); err != nil {
		return skip(fmt.Sprintf("the fragments do not cover frames 0..%d contiguously", r.n-1))
	}
	assembled := filepath.Join(rfDir, "assembled.mp4")
	if err := e.R.Run("refine: concat "+strconv.Itoa(len(leaves))+" fragment(s)",
		st.tw.Clip, filepath.Join(rfDir, "concat.log"),
		"-f", "concat", "-safe", "0", "-i", concat, "-c:v", "copy", "-an", "-sn", "-f", "mp4", assembled); err != nil {
		return skip("concatenating the fragments failed")
	}
	apk, err := media.PacketCount(e.R, assembled)
	if err != nil || apk != r.n {
		return skip(fmt.Sprintf("the merged video has %s frame(s), expected %d", orQuestion(apk, err), r.n))
	}

	// --- mux the Master's audio/subtitles back in ---
	muxed := st.out + ".refine.part"
	args := append([]string{}, st.durargs...)
	args = append(args, st.insrc...)
	args = append(args, "-i", assembled, "-map", "1:v:0")
	args = append(args, st.amap...)
	args = append(args, st.smap...)
	args = append(args, "-c:v", "copy")
	args = append(args, st.aargs...)
	args = append(args, st.sargs...)
	args = append(args, muxArgs(e)...)
	args = append(args, muxed)
	if err := e.R.Run("refine: mux merged video + audio/subs", st.tw.Clip,
		filepath.Join(rfDir, "mux.log"), args...); err != nil {
		_ = os.Remove(muxed)
		return skip("muxing the merged video with the audio/subtitle tracks failed")
	}

	// --- overall verification ---
	if err := r.seek(0); err != nil {
		_ = os.Remove(muxed)
		return skip("could not compute the seek position")
	}
	finMean, _, _, err := r.vmaf(muxed, r.n, filepath.Join(rfDir, "verify.log"), "refine: verify : libvmaf (merged video)")
	if err != nil {
		_ = os.Remove(muxed)
		return skip("could not verify the merged video (libvmaf failure or frame-count mismatch)")
	}
	finSize := fileSize(muxed)

	nbelow := 0
	crfLo, crfHi := 0, 0
	for i, f := range r.tsv {
		if !f.reached {
			nbelow++
		}
		if i == 0 || f.crf < crfLo {
			crfLo = f.crf
		}
		if i == 0 || f.crf > crfHi {
			crfHi = f.crf
		}
	}

	accept := false
	why := ""
	switch {
	case finMean >= r.target:
		accept = true
		why = fmt.Sprintf("merged video meets the target (overall vmaf %s)", fmtMean(finMean))
	case r.baseMean < r.target && finMean >= r.baseMean:
		accept = true
		why = fmt.Sprintf("merged video (%s) is closer to the target than the usual encode (%s), though still below %s",
			fmtMean(finMean), fmtMean(r.baseMean), r.st.e.C.TargetRaw)
	default:
		why = fmt.Sprintf("merged video (%s) is below the target and no better than the usual encode (%s)",
			fmtMean(finMean), fmtMean(r.baseMean))
	}

	if accept {
		if err := os.Rename(muxed, st.out); err != nil {
			return skip("could not replace the usual encode with the refined one")
		}
		e.Log.Info("refine: kept the refined encode: %d fragment(s), %s=%d-%d, overall vmaf %s -> %s, size %s -> %s; %s",
			len(leaves), r.qlab, crfLo, crfHi, fmtMean(r.baseMean), fmtMean(finMean),
			util.HumanSize(r.baseSize), util.HumanSize(finSize), why)
	} else {
		_ = os.Remove(muxed)
		e.Log.Info("refine: keeping the usual encode: %s (refined size would have been %s)", why, util.HumanSize(finSize))
	}

	if nbelow > 0 {
		var parts []string
		for _, f := range r.tsv {
			if !f.reached {
				parts = append(parts, fmt.Sprintf("%d#%d[%d-%d]", f.idx, f.lo, f.lo, f.hi-1))
			}
		}
		list := strings.Join(parts, " ")
		if len(list) > 300 {
			list = list[:300]
		}
		e.Log.Info("refine: WARNING %d/%d fragment(s) never reached target %s even at %s=%d: %s",
			nbelow, len(leaves), r.st.e.C.TargetRaw, r.qlab, r.floor, list)
		e.TUI.Message("refine: %d fragment(s) never reached target %s", nbelow, r.st.e.C.TargetRaw)
	}
	if e.Log.Enabled() {
		e.Log.Append(fmt.Sprintf("---- refine: %d fragment(s) ----", len(leaves)))
		for _, f := range r.tsv {
			status := "BELOW-TARGET"
			if f.reached {
				status = "PASS"
			}
			e.Log.Append(fmt.Sprintf("[mins] refine frag #%d frames %d-%d (%df) planned=%s usual=%.2f -> %s=%d vmaf=%.2f min=%.2f %s size=%s tried=%s",
				f.idx, f.lo, f.hi-1, f.hi-f.lo, f.planned, f.bmean, r.qlab, f.crf,
				f.mean, f.min, status, util.HumanSize(f.size), attemptsString(f.attempts)))
		}
	}

	blk := r.buildReport(accept, why, &finMean, &finSize)
	res := &refineResult{Report: blk, Mean: st.mean, Size: st.size, Passed: st.passed}
	if accept {
		res.Mean, res.Size, res.Passed = finMean, finSize, finMean >= r.target
		res.ModeCol = "av1+r"
		res.QCol = fmt.Sprintf("%d-%d", crfLo, crfHi)
	}
	return res, nil
}

func orQuestion(n int, err error) string {
	if err != nil {
		return "?"
	}
	return strconv.Itoa(n)
}

// plan runs the quicksort-shaped split, then merges same-kind neighbours.
func (r *refiner) plan(scores []float64) ([][2]int, int) {
	pre := make([]float64, len(scores)+1)
	for i, s := range scores {
		pre[i+1] = pre[i] + s
	}
	mean := func(lo, hi int) float64 { return (pre[hi] - pre[lo]) / float64(hi-lo) }

	var best func(lo, hi int) (float64, [][2]int)
	best = func(lo, hi int) (float64, [][2]int) {
		ln := hi - lo
		m := mean(lo, hi)
		leaf := float64(r.st.e.C.RefineBoundaryCost)
		if m < r.target {
			leaf += float64(ln)
		}
		if ln < 2 || ln/2 < r.minf {
			return leaf, [][2]int{{lo, hi}}
		}
		mid := lo + ln/2
		lc, ll := best(lo, mid)
		rc, rl := best(mid, hi)
		if ln > r.maxf || lc+rc < leaf {
			return lc + rc, append(ll, rl...)
		}
		return leaf, [][2]int{{lo, hi}}
	}
	_, leaves := best(0, len(scores))

	kind := func(lo, hi int) bool { return mean(lo, hi) >= r.target }
	var merged [][2]int
	for _, l := range leaves {
		if len(merged) > 0 {
			prev := merged[len(merged)-1]
			if kind(prev[0], prev[1]) == kind(l[0], l[1]) && l[1]-prev[0] <= r.maxf {
				merged[len(merged)-1] = [2]int{prev[0], l[1]}
				continue
			}
		}
		merged = append(merged, l)
	}
	nbad := 0
	for _, l := range merged {
		if mean(l[0], l[1]) < r.target {
			nbad++
		}
	}
	return merged, nbad
}

func (r *refiner) meanOf(scores []float64, lo, hi int) float64 {
	sum := 0.0
	for i := lo; i < hi; i++ {
		sum += scores[i]
	}
	return sum / float64(hi-lo)
}

func (r *refiner) probePTS0() bool {
	st := r.st
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "info"}
	args = append(args, st.insrc...)
	args = append(args, "-map", "0:V:0", "-frames:v", "1", "-vf", "showinfo", "-an", "-sn", "-f", "null", "-")
	out, _ := exec.Command(st.e.R.FFmpeg, args...).CombinedOutput()
	m := rePTS.FindStringSubmatch(string(out))
	if m == nil {
		return false
	}
	pts, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return false
	}
	ss := 0.0
	if st.tw.SS != "" {
		ss, _ = strconv.ParseFloat(st.tw.SS, 64)
	}
	r.pts0 = ss + pts
	return true
}

func (r *refiner) seek(lo int) error {
	if lo == 0 {
		r.seekArgs = r.st.insrc
		return nil
	}
	t := r.pts0 + (float64(lo)-0.5)/r.fps
	if t < 0 {
		t = 0
	}
	r.seekArgs = []string{"-ss", fmt.Sprintf("%.6f", t), "-i", r.st.input}
	return nil
}

// currentSeek is RF_SEEK: the input arguments that land on the current
// fragment's first frame.
func (r *refiner) currentSeek() []string { return r.seekArgs }

func (r *refiner) fragment(idx, total, lo, hi int, planned string, bmean float64) (*refFrag, error) {
	n := hi - lo
	tag := fmt.Sprintf("%06d-%06d", lo, hi-1)
	cand := filepath.Join(r.dir, "frag_"+tag+".mp4")
	flog := filepath.Join(r.dir, "frag_"+tag+".log")
	_ = os.WriteFile(flog, nil, 0o644)
	pre := fmt.Sprintf("refine: frag %d/%d (%d%%) frames %d-%d (%df)", idx, total, idx*100/total, lo, hi-1, n)
	r.note(flog, "%s: planned=%s, usual-encode mean vmaf=%.2f", pre, planned, bmean)
	if err := r.seek(lo); err != nil {
		r.note(flog, "%s: ERROR could not compute the seek position", pre)
		return nil, err
	}
	frag := &refFrag{idx: idx, lo: lo, hi: hi, planned: planned, bmean: bmean, cand: cand}
	crf := r.baseCRF
	for attempt := 1; ; attempt++ {
		if err := r.encode(crf, n, cand+".try", flog); err != nil {
			r.note(flog, "%s: ERROR encode failed at %s=%d", pre, r.qlab, crf)
			return nil, err
		}
		mean, min, frames, err := r.vmaf(cand+".try", n, flog, "")
		if err != nil {
			r.note(flog, "%s: ERROR libvmaf failed or frame count mismatch at %s=%d", pre, r.qlab, crf)
			return nil, err
		}
		size := fileSize(cand + ".try")
		frag.attempts = append(frag.attempts, report.RefineAttempt{CRF: crf, VMAF: report.F(mean)})
		verdict := "FAIL"
		if mean >= r.target {
			verdict = "PASS"
		}
		r.note(flog, "%s: %s=%d (ascending #%d): vmaf=%s min=%s target=%s %s size=%s",
			pre, r.qlab, crf, attempt, fmtMean(mean), fmtFloat2(min), r.st.e.C.TargetRaw, verdict, util.HumanSize(size))
		if len(frames) > 0 {
			var ss []string
			for _, f := range frames {
				ss = append(ss, fmt.Sprintf("%.2f", f))
			}
			appendLine(flog, "[mins]   per-frame vmaf: "+strings.Join(ss, ","))
		}
		frag.crf, frag.mean, frag.min, frag.size = crf, mean, min, size
		if verdict == "PASS" {
			frag.reached = true
			break
		}
		if crf <= r.floor {
			break
		}
		crf -= r.step
		if crf < r.floor {
			crf = r.floor
		}
	}
	_ = os.Rename(cand+".try", cand)
	if frag.reached {
		r.note(flog, "%s: done %s=%d vmaf=%s size=%s (%d attempt(s))",
			pre, r.qlab, frag.crf, fmtMean(frag.mean), util.HumanSize(frag.size), len(frag.attempts))
	} else {
		r.note(flog, "%s: WARNING never reached target %s down to %s=%d, the lowest this run allows (--crf-min); keeping best effort vmaf=%s size=%s (%d attempt(s)). Likely a content-driven VMAF ceiling for these frames, not a bug.",
			pre, r.st.e.C.TargetRaw, r.qlab, frag.crf, fmtMean(frag.mean), util.HumanSize(frag.size), len(frag.attempts))
	}
	r.copyFragmentLog(tag)
	return frag, nil
}

func (r *refiner) encode(crf, n int, out, flog string) error {
	c := r.st.e.C
	vf := fmt.Sprintf("settb=%d/%d,setpts=N,pad=ceil(iw/2)*2:ceil(ih/2)*2:0:0:color=black", r.fpsDen, r.fpsNum)
	args := []string{"-hide_banner", "-nostdin", "-y", "-nostats", "-loglevel", "error"}
	args = append(args, r.currentSeek()...)
	args = append(args, "-frames:v", strconv.Itoa(n), "-map", "0:V:0", "-an", "-sn", "-vf", vf)
	args = append(args, encoderArgs(r.st.e, crf, "quality")...)
	args = append(args, "-pix_fmt", c.PixFmt, "-fps_mode", "passthrough", "-f", "mp4", out)
	outb, err := exec.Command(r.st.e.R.FFmpeg, args...).CombinedOutput()
	appendLine(flog, string(outb))
	return err
}

// vmaf pairs candidate and Master frames by index (not timestamp) and
// returns mean, min, and per-frame scores for small fragments.
func (r *refiner) vmaf(cand string, n int, flog, label string) (float64, float64, []float64, error) {
	e := r.st.e
	vlog := filepath.Join(r.dir, "vmaf_cur.json")
	_ = os.Remove(vlog)
	idxTS := fmt.Sprintf("settb=AVTB,setpts=N*%d*1000000/%d", r.fpsDen, r.fpsNum)
	filter := fmt.Sprintf("[0:v]%s,crop=%d:%d:0:0,format=yuv420p[d];"+
		"[1:V]trim=end_frame=%d,%s,crop=%d:%d:0:0,format=yuv420p[r];"+
		"[d][r]libvmaf=n_threads=%d:log_path=%s:log_fmt=json",
		idxTS, r.st.p.W, r.st.p.H, n, idxTS, r.st.p.W, r.st.p.H, e.C.NProc, escape.Filter(vlog))
	args := []string{"-i", cand}
	args = append(args, r.currentSeek()...)
	args = append(args, "-lavfi", filter, "-f", "null", "-")
	if label != "" {
		if err := e.R.Run(label, r.st.tw.Clip, flog, args...); err != nil {
			return 0, 0, nil, err
		}
	} else {
		full := append([]string{"-hide_banner", "-nostdin", "-y", "-nostats", "-loglevel", "error"}, args...)
		out, err := exec.Command(e.R.FFmpeg, full...).CombinedOutput()
		appendLine(flog, string(out))
		if err != nil {
			return 0, 0, nil, err
		}
	}
	lg, err := vmaf.Parse(vlog)
	if err != nil {
		return 0, 0, nil, err
	}
	mean, nf := lg.Mean()
	if nf != n {
		appendLine(flog, fmt.Sprintf("libvmaf compared %d frame(s), expected %d", nf, n))
		return 0, 0, nil, fmt.Errorf("frame count mismatch")
	}
	min := lg.PooledMetrics["vmaf"].Min
	var frames []float64
	if nf <= 16 {
		frames = lg.Scores()
	}
	return mean, min, frames, nil
}

func (r *refiner) writeConcat(path string) error {
	rows := append([]refFrag(nil), r.tsv...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].lo < rows[j].lo })
	pos := 0
	var b strings.Builder
	for _, f := range rows {
		if f.lo != pos || f.hi <= f.lo {
			return fmt.Errorf("gap")
		}
		fmt.Fprintf(&b, "file '%s'\nduration %.6f\n", escape.Concat(f.cand), float64(f.hi-f.lo)/r.fps)
		pos = f.hi
	}
	if pos != r.n {
		return fmt.Errorf("short")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func (r *refiner) note(flog, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	r.st.e.Log.Info("%s", msg)
	if flog != "" {
		appendLine(flog, "[mins] "+msg)
	}
}

func (r *refiner) copyFragmentLog(tag string) {
	if !r.st.e.Log.Enabled() {
		return
	}
	src := filepath.Join(r.dir, "frag_"+tag+".log")
	data, err := os.ReadFile(src)
	if err != nil || len(data) == 0 {
		return
	}
	dst := filepath.Join(r.st.e.Log.Dir, "mins-logs", r.st.e.Log.RunID+"_"+tagOf(r.st.input)+"_frag"+tag+".log")
	_ = os.WriteFile(dst, data, 0o644)
}

func (r *refiner) buildReport(applied bool, reason string, finMean *float64, finSize *int64) *report.Refine {
	blk := &report.Refine{
		Applied: applied, Reason: reason,
		TargetVMAF: report.F(r.target),
		UsualEncode: report.UsualEncode{
			CRF: r.baseCRF, MeanVMAF: report.F(r.baseMean), SizeBytes: r.baseSize,
		},
		Frames: r.n, MaxFragmentFrames: r.maxf, MinFragmentFrames: r.minf,
		CRFStep: r.step,
	}
	blk.Fragments = []report.RefineFragment{}
	below := []int{}
	for _, f := range r.tsv {
		rf := report.RefineFragment{
			Index: f.idx, FirstFrame: f.lo, LastFrame: f.hi - 1, Frames: f.hi - f.lo,
			Planned: f.planned, UsualEncodeVMAF: report.F(f.bmean),
			CRF: f.crf, VMAF: report.F(f.mean), MinFrameVMAF: report.F(f.min),
			TargetReached: f.reached, SizeBytes: f.size, Attempts: f.attempts,
		}
		blk.Fragments = append(blk.Fragments, rf)
		if !f.reached {
			below = append(below, f.idx)
		}
	}
	sort.Slice(blk.Fragments, func(i, j int) bool { return blk.Fragments[i].FirstFrame < blk.Fragments[j].FirstFrame })
	blk.FragmentCount = len(blk.Fragments)
	blk.FragmentsBelowTarget = len(below)
	blk.FragmentsBelowTargetList = below
	if finMean != nil {
		blk.MergedEncode = &report.MergedEncode{MeanVMAF: report.F(*finMean), SizeBytes: *finSize}
	}
	if len(blk.Fragments) > 0 {
		lo, hi := blk.Fragments[0].CRF, blk.Fragments[0].CRF
		for _, f := range blk.Fragments[1:] {
			if f.CRF < lo {
				lo = f.CRF
			}
			if f.CRF > hi {
				hi = f.CRF
			}
		}
		blk.CRFRange = &report.CRFRange{Min: lo, Max: hi}
	}
	return blk
}

// tagOf mirrors CUR_TAG: the stem with every character outside the allowed
// set replaced by '_', including the trailing newline in_stem used to emit.
func tagOf(input string) string {
	return util.SafeTag(util.InStem(input)) + "_"
}

func appendLine(path, line string) {
	if line == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
}

func attemptsString(a []report.RefineAttempt) string {
	var parts []string
	for _, x := range a {
		parts = append(parts, fmt.Sprintf("%d:%s", x.CRF, fmtMean(float64(x.VMAF))))
	}
	return strings.Join(parts, ",")
}

func fmtFloat2(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

var _ = core.Cleanup
