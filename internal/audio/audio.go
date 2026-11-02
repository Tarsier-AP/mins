// Package audio implements track selection, codec policy, the audio quality
// metrics (ViSQOL and ffmpeg's asdr family), and the bitrate search.
package audio

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"mins/internal/config"
	"mins/internal/ffmpeg"
	"mins/internal/logx"
	"mins/internal/media"
	"mins/internal/util"
)

// Selection is the resolved audio track set for one input.
type Selection struct {
	AMap     []string
	AIDX     []int
	AKeep    int
	KEPTDesc string
}

// CodecArgs is the resolved -c:a / filter argument set.
type CodecArgs struct {
	Args []string
	Desc string
}

// Engine carries the dependencies of the audio helpers.
type Engine struct {
	R         *ffmpeg.Runner
	Log       *logx.Logger
	C         *config.Config
	VisqolBin string
}

// ResolveAudio applies --atracks to input.
func (e *Engine) ResolveAudio(input string) (*Selection, error) {
	sel := &Selection{}
	if e.C.ATracks == "none" {
		return sel, nil
	}
	tracks, err := media.AudioTracks(e.R, input)
	if err != nil {
		return nil, err
	}
	n := len(tracks)
	if e.C.ATracks == "all" {
		sel.AMap = []string{"-map", "0:a?"}
		sel.AKeep = n
		sel.KEPTDesc = "all"
		for i := 0; i < n; i++ {
			sel.AIDX = append(sel.AIDX, i)
		}
		return sel, nil
	}
	avail := ""
	for i, t := range tracks {
		if i > 0 {
			avail += " "
		}
		avail += fmt.Sprintf("%d:%s", i, t.Lang)
	}
	var keep []int
	for _, tok := range strings.Split(e.C.ATracks, ",") {
		if tok == "" {
			continue
		}
		if idx, err := strconv.Atoi(tok); err == nil {
			if idx >= n {
				return nil, fmt.Errorf("audio track %d out of range in %s (available: %s)", idx, filepath.Base(input), orNone(avail))
			}
			keep = append(keep, idx)
			continue
		}
		found := false
		for i, t := range tracks {
			if strings.EqualFold(t.Lang, tok) {
				keep = append(keep, i)
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("--atracks: '%s' matches no audio track in %s (available: %s)", tok, filepath.Base(input), orNone(avail))
		}
	}
	seen := map[int]bool{}
	for _, k := range keep {
		if seen[k] {
			continue
		}
		seen[k] = true
		sel.AMap = append(sel.AMap, "-map", fmt.Sprintf("0:a:%d", k))
		sel.AIDX = append(sel.AIDX, k)
		if sel.KEPTDesc != "" {
			sel.KEPTDesc += ","
		}
		sel.KEPTDesc += fmt.Sprintf("%d:%s", k, tracks[k].Lang)
	}
	sel.AKeep = len(seen)
	return sel, nil
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// CopyOK reports whether a stream copy of codec into the video container
// muxes.
func CopyOK(codec, vidfmt string) bool {
	if codec == "" || codec == "unknown" {
		return false
	}
	switch vidfmt {
	case "mkv":
		return true
	case "mp4":
		switch codec {
		case "aac", "ac3", "eac3", "mp2", "mp3", "opus", "flac", "alac", "dts",
			"pcm_s16le", "pcm_s16be", "pcm_s24le", "pcm_s24be", "pcm_s32le", "pcm_s32be",
			"pcm_f32le", "pcm_f32be", "pcm_f64le", "pcm_f64be":
			return true
		}
	case "webm":
		switch codec {
		case "opus", "vorbis":
			return true
		}
	}
	return false
}

// FmtOK reports whether codec muxes into an --audio-fmt container.
func FmtOK(codec, format string) bool {
	switch codec {
	case "libopus":
		codec = "opus"
	case "libvorbis":
		codec = "vorbis"
	}
	switch format {
	case "ogg":
		return codec == "opus" || codec == "vorbis" || codec == "flac"
	case "wav":
		switch codec {
		case "pcm_s16le", "pcm_s16be", "pcm_s24le", "pcm_s24be", "pcm_s32le", "pcm_s32be",
			"pcm_f32le", "pcm_f32be", "pcm_f64le", "pcm_f64be":
			return true
		}
	case "mp4":
		switch codec {
		case "aac", "ac3", "eac3", "mp2", "mp3", "opus", "vorbis", "flac", "alac", "dts",
			"pcm_s16le", "pcm_s16be", "pcm_s24le", "pcm_s32le", "pcm_f32le":
			return true
		}
	case "mov":
		switch codec {
		case "aac", "ac3", "eac3", "mp2", "mp3", "vorbis", "alac", "dts",
			"pcm_s16le", "pcm_s16be", "pcm_s24le", "pcm_s32le", "pcm_f32le",
			"pcm_u8", "pcm_alaw", "pcm_mulaw":
			return true
		}
	case "mka":
		return true
	case "webm":
		return codec == "opus" || codec == "vorbis"
	}
	return false
}

// FmtExt returns the output extension for a container/codec pair.
func FmtExt(format, codec string) string {
	switch codec {
	case "libopus":
		codec = "opus"
	case "libvorbis":
		codec = "vorbis"
	}
	switch format {
	case "ogg":
		if codec == "opus" {
			return "opus"
		}
		return "ogg"
	case "wav":
		return "wav"
	case "mp4":
		return "mp4"
	case "mov":
		return "mov"
	case "mka":
		return "mka"
	case "webm":
		return "webm"
	}
	return ""
}

// LayoutFilter returns the aformat filter for an encoder's layout quirks.
func LayoutFilter(codec string) string {
	switch codec {
	case "libopus":
		return "aformat=channel_layouts=mono|stereo|3.0|quad|5.0|5.1|6.1|7.1"
	case "libvorbis":
		return "aformat=channel_layouts=stereo|3.0|quad|5.0|5.1|6.1|7.1"
	}
	return ""
}

func transcodeArgs(c *config.Config, codec, sfx string) []string {
	if codec == "pcm_s16le" {
		return []string{"-c:a" + sfx, "pcm_s16le"}
	}
	args := []string{"-c:a" + sfx, codec, "-b:a" + sfx, "256k"}
	if lf := LayoutFilter(codec); lf != "" {
		args = append(args, "-filter:a"+sfx, lf)
	}
	return args
}

// ResolveACodec applies --acodec to the selected tracks.
func (e *Engine) ResolveACodec(input string, sel *Selection) (*CodecArgs, error) {
	res := &CodecArgs{Desc: e.C.ACodec}
	if sel.AKeep == 0 {
		return res, nil
	}
	switch e.C.ACodec {
	case "copy":
		res.Args = []string{"-c:a", "copy"}
		return res, nil
	case "auto":
		// fall through to per-track resolution
	default:
		res.Args = transcodeArgs(e.C, e.C.ACodec, "")
		return res, nil
	}
	codecs, err := media.AudioCodecs(e.R, input)
	if err != nil {
		return nil, err
	}
	fb := "libopus"
	if e.C.VidFmt == "mp4" {
		fb = "aac"
	}
	res.Desc = ""
	for o, k := range sel.AIDX {
		codec := ""
		if k < len(codecs) {
			codec = codecs[k]
		}
		if CopyOK(codec, e.C.VidFmt) {
			res.Args = append(res.Args, fmt.Sprintf("-c:a:%d", o), "copy")
			res.Desc += comma(res.Desc) + codec + ":copy"
		} else {
			res.Args = append(res.Args, transcodeArgs(e.C, fb, fmt.Sprintf(":%d", o))...)
			if codec == "" {
				codec = "unknown"
			}
			res.Desc += comma(res.Desc) + codec + ":" + fb
		}
	}
	res.Desc = "auto[" + res.Desc + "]"
	return res, nil
}

func comma(s string) string {
	if s == "" {
		return ""
	}
	return ","
}

// ResolveSearch re-encodes each kept track at a searched bitrate.
func (e *Engine) ResolveSearch(input string, sel *Selection, ss, t string, dur float64, work string) (*CodecArgs, error) {
	res := &CodecArgs{Desc: e.C.ACodec}
	if sel.AKeep == 0 {
		return res, nil
	}
	var codec string
	switch e.C.ACodec {
	case "aac", "libopus", "libvorbis":
		codec = e.C.ACodec
	case "auto":
		codec = "libopus"
		if e.C.VidFmt == "mp4" {
			codec = "aac"
		}
	default:
		e.Log.Info("warning: --audio-search needs a lossy --acodec (aac/libopus/libvorbis) or auto; '%s' has no bitrate to search, keeping normal audio handling", e.C.ACodec)
		return res, nil
	}
	res.Desc = ""
	for o, k := range sel.AIDX {
		br, err := e.SearchBitrate(input, k, codec, work, ss, t, dur, fmt.Sprintf("audio track %d", k))
		if err != nil {
			return nil, err
		}
		res.Args = append(res.Args, fmt.Sprintf("-c:a:%d", o), codec, fmt.Sprintf("-b:a:%d", o), fmt.Sprintf("%dk", br))
		if lf := LayoutFilter(codec); lf != "" {
			res.Args = append(res.Args, fmt.Sprintf("-filter:a:%d", o), lf)
		}
		res.Desc += comma(res.Desc) + fmt.Sprintf("%d:%s@%dk", k, codec, br)
	}
	res.Desc = "search[" + res.Desc + "]"
	return res, nil
}

// SearchBitrate binary-searches the lowest bitrate clearing the gate.
func (e *Engine) SearchBitrate(input string, k int, codec, work, ss, t string, edur float64, label string) (int, error) {
	var ext string
	switch codec {
	case "libopus":
		ext = "opus"
	case "aac":
		ext = "m4a"
	case "libvorbis":
		ext = "ogg"
	default:
		return 0, fmt.Errorf("internal error: no container for codec %q", codec)
	}
	lo, hi := e.C.BRMin, e.C.BRMax
	best := 0
	fb, fm, lb, lm := 0, 0.0, 0, 0.0
	for lo <= hi {
		mid := (lo + hi) / 2
		cand := filepath.Join(work, fmt.Sprintf("asearch_%d_%d.%s", k, mid, ext))
		args := []string{"-y", "-loglevel", "error"}
		if ss != "" {
			args = append(args, "-ss", ss)
		}
		if t != "" {
			args = append(args, "-t", t)
		}
		args = append(args, "-i", input, "-map", fmt.Sprintf("0:a:%d", k), "-vn", "-sn",
			"-c:a", codec, "-b:a", fmt.Sprintf("%dk", mid))
		if codec == "libopus" {
			args = append(args, "-vbr", "on")
		}
		if lf := LayoutFilter(codec); lf != "" {
			args = append(args, "-af", lf)
		}
		args = append(args, cand)
		if out, err := e.R.RunCapture(args...); err != nil {
			e.Log.Append("--- ffmpeg (audio search encode) stderr (tail) ---")
			e.Log.Append(out)
			return 0, fmt.Errorf("audio search: ffmpeg encode failed for %s at %dk", label, mid)
		}
		mean, err := e.Measure(input, k, cand, codec, filepath.Join(work, "asearch_metric.log"), ss, t, edur)
		if err != nil {
			return 0, fmt.Errorf("audio search: %s failed for %s at %dk: %w", e.C.AudioMetric, label, mid, err)
		}
		size := fileSize(cand)
		verdict := "FAIL"
		if mean >= e.C.TargetAudio {
			verdict = "PASS"
		}
		e.Log.Info("audio search (%s): br=%dk %s=%s%s size=%s %s", label, mid, e.C.AudioMetric, trimFloat(mean), e.C.AudioUnit, util.HumanSize(size), verdict)
		if fb == 0 {
			fb, fm = mid, mean
		}
		lb, lm = mid, mean
		if verdict == "PASS" {
			best = mid
			hi = mid - 1
		} else {
			lo = mid + 1
		}
		_ = os.Remove(cand)
	}
	e.VisqolCleanup(work)
	if best == 0 {
		best = e.C.BRMax
		e.PlateauHint("audio search ("+label+")", fb, fm, lb, lm)
		e.Log.Info("warning: audio search (%s): no bitrate in [%d..%d]k reached %s %s%s; falling back to %dk", label, e.C.BRMin, e.C.BRMax, e.C.AudioMetric, trimFloat(e.C.TargetAudio), e.C.AudioUnitLong, best)
	}
	return best, nil
}

// PlateauHint warns when the score stops tracking bitrate.
func (e *Engine) PlateauHint(label string, fb int, fm float64, lb int, lm float64) {
	if float64(lb) >= float64(fb)*1.4 && lm-fm < e.C.AudioEps {
		if e.C.AudioMetric == "visqol" {
			e.Log.Info("warning: %s: visqol barely moves with bitrate (%s at %dk -> %s at %dk). Either --target-audio %s is above what ViSQOL's audio-mode model can report for this signal (it tops out around 4.75 even for a perfect match), or the candidate differs from the source in a way more bitrate doesn't change (channel layout, level, timing) -- lower --target-audio or try another --acodec",
				label, trimFloat(fm), fb, trimFloat(lm), lb, trimFloat(e.C.TargetAudio))
		} else {
			e.Log.Info("warning: %s: %s barely moves with bitrate (%s dB at %dk -> %s dB at %dk). That's a source-vs-codec mismatch, typically DC offset / sub-bass / ultrasonic content this codec deliberately doesn't reproduce -- more bitrate won't fix it. Try another --acodec, adjust --audio-band, or lower --target-audio",
				label, e.C.AudioMetric, trimFloat(fm), fb, trimFloat(lm), lb)
		}
	}
}

// Measure scores candidate against the reference track.
func (e *Engine) Measure(ref string, k int, cand, codec, errLog, ss, t string, edur float64) (float64, error) {
	if e.C.AudioMetric == "visqol" {
		return e.measureVisqol(ref, k, cand, codec, errLog, ss, t, edur)
	}
	tag := map[string]string{"asdr": "SDR", "apsnr": "PSNR", "asisdr": "SI-SDR"}[e.C.AudioMetric]
	lf := LayoutFilter(codec)
	band := e.C.AudioBandFilter
	lfComma, bandSuffix := "", ""
	if lf != "" {
		lfComma = lf + ","
	}
	if band != "" {
		bandSuffix = "," + band
	}
	filter := fmt.Sprintf("[0:a:%d]%saformat=sample_fmts=fltp:sample_rates=48000,asetpts=PTS-STARTPTS%s[r];[1:a:0]aformat=sample_fmts=fltp:sample_rates=48000,asetpts=PTS-STARTPTS%s[d];[r][d]%s",
		k, lfComma, bandSuffix, bandSuffix, e.C.AudioMetric)
	args := []string{"-y", "-nostats", "-loglevel", "info"}
	if ss != "" {
		args = append(args, "-ss", ss)
	}
	if t != "" {
		args = append(args, "-t", t)
	}
	args = append(args, "-i", ref, "-i", cand, "-filter_complex", filter, "-f", "null", "-")
	out, err := e.R.RunCapture(args...)
	_ = os.WriteFile(errLog, []byte(out), 0o644)
	if err != nil {
		return 0, fmt.Errorf("%s failed on %s vs %s", e.C.AudioMetric, filepath.Base(cand), filepath.Base(ref))
	}
	re := regexp.MustCompile(regexp.QuoteMeta(tag) + `\s+ch\d+:\s*(-?(?:inf|nan|[0-9.]+(?:[eE][+-]?[0-9]+)?))\s*dB`)
	var vals []float64
	for _, line := range strings.Split(out, "\n") {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		v, err := strconv.ParseFloat(m[1], 64)
		if err == nil && !math.IsNaN(v) {
			vals = append(vals, v)
		}
	}
	if len(vals) == 0 {
		return 0, fmt.Errorf("%s ran but produced no readable dB line", e.C.AudioMetric)
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	mean := round4(sum / float64(len(vals)))
	e.durationGuard(cand, edur)
	return mean, nil
}

func prefixComma(s string) string {
	if s == "" {
		return ""
	}
	return s + ","
}

var reMOS = regexp.MustCompile(`(?i)MOS[-_ ]?LQO\s*[:=]?\s*(-?(?:nan|[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?))`)

func (e *Engine) measureVisqol(ref string, k int, cand, codec, errLog, ss, t string, edur float64) (float64, error) {
	wdir := filepath.Dir(errLog)
	refwav := filepath.Join(wdir, fmt.Sprintf("visqol_ref_%d.wav", k))
	degwav := filepath.Join(wdir, "visqol_deg.wav")
	keyFile := refwav + ".key"
	silent := refwav + ".silent"
	lf := LayoutFilter(codec)
	key := fmt.Sprintf("%s|%d|%s|%s|%s", ref, k, ss, t, lf)

	if st, err := os.Stat(refwav); err != nil || st.Size() == 0 || !sameKey(keyFile, key) {
		_ = os.Remove(refwav)
		_ = os.Remove(keyFile)
		_ = os.Remove(silent)
		args := []string{"-y", "-loglevel", "error"}
		if ss != "" {
			args = append(args, "-ss", ss)
		}
		if t != "" {
			args = append(args, "-t", t)
		}
		args = append(args, "-i", ref, "-map", fmt.Sprintf("0:a:%d", k), "-vn", "-sn")
		if lf != "" {
			args = append(args, "-af", lf)
		}
		args = append(args, "-ar", "48000", "-ac", "2", "-c:a", "pcm_s16le", refwav)
		if out, err := e.R.RunCapture(args...); err != nil {
			e.Log.Append("--- ffmpeg (visqol reference decode) stderr (tail) ---")
			e.Log.Append(out)
			return 0, fmt.Errorf("visqol: could not decode %s audio track %d to 48 kHz WAV", filepath.Base(ref), k)
		}
		_ = os.WriteFile(keyFile, []byte(key), 0o644)
		volOut, _ := e.R.RunCapture("-i", refwav, "-af", "volumedetect", "-f", "null", "-")
		vol := parseMaxVolume(volOut)
		if vol < -80 {
			_ = os.WriteFile(silent, nil, 0o644)
			e.Log.Info("warning: %s audio track %d is digital silence (max %s dB); ViSQOL can't score silence -- treating it as passing, so it lands on the lowest bitrate", filepath.Base(ref), k, trimFloat(vol))
		}
	}
	if _, err := os.Stat(silent); err == nil {
		return 5, nil
	}
	args := []string{"-y", "-loglevel", "error", "-i", cand, "-map", "0:a:0", "-vn", "-sn",
		"-ar", "48000", "-ac", "2", "-c:a", "pcm_s16le", degwav}
	if out, err := e.R.RunCapture(args...); err != nil {
		e.Log.Append("--- ffmpeg (visqol candidate decode) stderr (tail) ---")
		e.Log.Append(out)
		return 0, fmt.Errorf("visqol: could not decode %s to 48 kHz WAV", filepath.Base(cand))
	}
	bin := e.VisqolBin
	if bin == "" {
		bin = "visqol"
	}
	out, err := exec.Command(bin,
		"--reference_file", refwav,
		"--degraded_file", degwav,
		"--similarity_to_quality_model", e.C.VisqolModel,
		"--verbose").CombinedOutput()
	_ = os.WriteFile(errLog, out, 0o644)
	if err != nil {
		e.Log.Append("--- visqol output (tail) ---")
		e.Log.Append(string(out))
		return 0, fmt.Errorf("visqol failed on %s vs %s (--audio-metric asisdr gates without it)", filepath.Base(cand), filepath.Base(ref))
	}
	all := reMOS.FindAllStringSubmatch(string(out), -1)
	if len(all) == 0 {
		return 0, fmt.Errorf("visqol ran but printed no MOS-LQO score (see %s)", errLog)
	}
	v, err := strconv.ParseFloat(all[len(all)-1][1], 64)
	if err != nil {
		return 0, fmt.Errorf("visqol printed an unreadable MOS-LQO score")
	}
	e.durationGuard(cand, edur)
	return round4(v), nil
}

func sameKey(path, key string) bool {
	data, err := os.ReadFile(path)
	return err == nil && string(data) == key
}

func parseMaxVolume(out string) float64 {
	re := regexp.MustCompile(`max_volume:\s*(-?[0-9.]+) dB`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		return 0
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v
}

func (e *Engine) durationGuard(cand string, expected float64) {
	if expected < 0.001 {
		return
	}
	d, err := media.Duration(e.R, cand)
	if err != nil {
		return
	}
	if math.Abs(d-expected) > 0.1 {
		e.Log.Info("warning: encode duration %ss differs from expected %ss by more than 100ms -- %s may be comparing misaligned audio (check for a codec/container priming-sample issue)", trimFloat(d), trimFloat(expected), e.C.AudioMetric)
	}
}

// VisqolCleanup removes the cached reference WAVs for a work dir.
func (e *Engine) VisqolCleanup(work string) {
	matches, _ := filepath.Glob(filepath.Join(work, "visqol_ref_*.wav*"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
	_ = os.Remove(filepath.Join(work, "visqol_deg.wav"))
}

func trimFloat(v float64) string {
	if math.IsInf(v, 1) {
		return "inf"
	}
	if math.IsInf(v, -1) {
		return "-inf"
	}
	if math.IsNaN(v) {
		return "nan"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// round4 reproduces the round(x, 4) mins.sh applied to every audio score.
func round4(v float64) float64 {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return v
	}
	return math.RoundToEven(v*1e4) / 1e4
}

// Score formats a measured audio score the way the shell's python helper
// printed it (four-decimal rounding, inf for a perfect dB match).
func Score(v float64) string { return trimFloat(v) }

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}
