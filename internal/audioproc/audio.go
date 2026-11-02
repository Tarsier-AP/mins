// Package audioproc implements the standalone-audio processing path.
package audioproc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"mins/internal/audio"
	"mins/internal/core"
	"mins/internal/logx"
	"mins/internal/media"
	"mins/internal/report"
	"mins/internal/result"
	"mins/internal/util"
)

var reDur = regexp.MustCompile(`^[0-9]+([.][0-9]+)?$`)

func errRow(name, msg string) result.Row {
	return result.Row{Output: name, Mode: "audio", Q: "-", VMAF: "-", Size: "-", Status: msg}
}

// Process handles one standalone audio file: bitrate-gated re-encode.
func Process(e *core.Env, input string) (result.Row, bool, error) {
	c := e.C
	if _, err := os.Stat(input); err != nil {
		return errRow(filepath.Base(input), "ERROR: not found"), true, nil
	}

	acodec := c.ACodec
	if acodec == "auto" {
		acodec = "libopus"
	}
	ext := ""
	switch acodec {
	case "libopus":
		ext = "opus"
	case "aac":
		ext = "m4a"
	case "libvorbis":
		ext = "ogg"
	case "pcm_s16le":
		ext = "wav"
	case "copy":
		ext = util.InExt(input)
	default:
		return errRow(filepath.Base(input),
			fmt.Sprintf("ERROR: --acodec '%s' not valid for a standalone audio input (use libopus, aac, libvorbis, pcm_s16le or copy)", acodec)), true, nil
	}

	if c.AudioFmt != "auto" {
		fmtCodec := acodec
		if acodec == "copy" {
			out, _ := e.R.Probe("-select_streams", "a:0", "-show_entries", "stream=codec_name", "-of", "csv=p=0", input)
			fmtCodec = strings.TrimSpace(out)
			if fmtCodec == "" {
				fmtCodec = "unknown"
			}
		}
		switch fmtCodec {
		case "libopus":
			fmtCodec = "opus"
		case "libvorbis":
			fmtCodec = "vorbis"
		}
		if !audio.FmtOK(fmtCodec, c.AudioFmt) {
			hint := ""
			if c.AudioFmt == "mov" && (fmtCodec == "opus" || fmtCodec == "flac") {
				hint = fmt.Sprintf(" (ffmpeg's mov muxer refuses %s outright -- 'only supported in MP4'; --audio-fmt mp4 is what DaVinci Resolve and most other NLEs expect %s wrapped in anyway)", fmtCodec, fmtCodec)
			}
			return errRow(filepath.Base(input),
				fmt.Sprintf("ERROR: --audio-fmt '%s' can't hold '%s' audio%s", c.AudioFmt, fmtCodec, hint)), true, nil
		}
		if fmtCodec == "vorbis" && (c.AudioFmt == "mp4" || c.AudioFmt == "mov") {
			e.Log.Info("warning: vorbis-in-%s muxes but essentially no player supports it (same caveat as the video path's mp4+libvorbis warning) -- ogg, mka or webm is the safe home for vorbis", c.AudioFmt)
		}
		ext = audio.FmtExt(c.AudioFmt, fmtCodec)
	}

	dir, err := core.AbsDir(input)
	if err != nil {
		return errRow(filepath.Base(input), "ERROR: cannot enter "+filepath.Dir(input)), true, nil
	}
	base := util.InStem(input)
	out := filepath.Join(dir, base+"."+ext)
	rep := filepath.Join(dir, base+".audio.json")
	if strings.EqualFold(util.InExt(input), ext) {
		out = filepath.Join(dir, base+".min."+ext)
		rep = filepath.Join(dir, base+".min.audio.json")
	}
	if c.SkipExisting {
		if _, err := os.Stat(out); err == nil {
			e.Log.Info("skipping %s: output already exists", filepath.Base(input))
			return errRow(filepath.Base(out), "SKIPPED (exists)"), false, nil
		}
	}

	inDur := 0.0
	if s, err := media.DurationRaw(e.R, input); err == nil && reDur.MatchString(s) {
		inDur, _ = strconv.ParseFloat(s, 64)
	}

	work, err := core.TempDir(input)
	if err != nil {
		return errRow(filepath.Base(input), "ERROR: cannot create temp dir in "+dir), true, nil
	}

	encode := func(br int, dst string) error {
		elog := filepath.Join(work, "ffmpeg_err.log")
		_ = os.Remove(elog)
		args := []string{"-hide_banner", "-nostdin", "-y", "-nostats", "-loglevel", "error", "-i", input,
			"-map", "0:a:0", "-vn", "-sn"}
		switch acodec {
		case "pcm_s16le":
			args = append(args, "-c:a", "pcm_s16le")
		case "copy":
			args = append(args, "-c:a", "copy")
		case "libopus":
			args = append(args, "-c:a", "libopus", "-b:a", fmt.Sprintf("%dk", br), "-vbr", "on")
		default:
			args = append(args, "-c:a", acodec, "-b:a", fmt.Sprintf("%dk", br))
		}
		if lf := audio.LayoutFilter(acodec); lf != "" {
			args = append(args, "-af", lf)
		}
		args = append(args, dst)
		f, err := os.Create(elog)
		if err != nil {
			return err
		}
		defer f.Close()
		cmd := exec.Command(e.R.FFmpeg, args...)
		cmd.Stdout = f
		cmd.Stderr = f
		return cmd.Run()
	}
	failed := func() (result.Row, bool, error) {
		elog := filepath.Join(work, "ffmpeg_err.log")
		if st, err := os.Stat(elog); err == nil && st.Size() > 0 {
			tail := logx.Tail(elog, 20)
			fmt.Fprintln(os.Stderr, tail)
			e.Log.Append("--- ffmpeg (audio encode) stderr (tail) ---")
			e.Log.Append(tail)
		}
		return errRow(filepath.Base(input), "ERROR: ffmpeg encode failed"), true, nil
	}
	metric := func(cand string) (float64, error) {
		return e.Audio.Measure(input, 0, cand, acodec, filepath.Join(work, "audio_metric.log"), "", "", inDur)
	}

	e.Log.Info("input : %s -> %s (%s gate >= %s%s)", filepath.Base(input), acodec,
		c.AudioMetric, c.AudioTargetRaw, c.AudioUnitLong)

	inSize := fileSize(input)
	best := "-"
	mean := "-"
	passed := true

	if acodec == "pcm_s16le" || acodec == "copy" {
		if err := encode(0, filepath.Join(work, "final."+ext)); err != nil {
			return failed()
		}
	} else {
		lo, hi := c.BRMin, c.BRMax
		bestN := 0
		fb, fm, lb, lm := 0, 0.0, 0, 0.0
		for lo <= hi {
			mid := (lo + hi) / 2
			cand := filepath.Join(work, fmt.Sprintf("search_%d.%s", mid, ext))
			if err := encode(mid, cand); err != nil {
				return failed()
			}
			m, err := metric(cand)
			if err != nil {
				return result.Row{}, true, err
			}
			size := fileSize(cand)
			verdict := "FAIL"
			if util.AtLeast(m, c.TargetAudio) {
				verdict = "PASS"
			}
			e.Log.Info("search: br=%dk %s=%s%s size=%s %s", mid, c.AudioMetric,
				audio.Score(m), c.AudioUnit, util.HumanSize(size), verdict)
			e.TUI.Attempt("search", mid, m, c.TargetAudio, verdict)
			if fb == 0 {
				fb, fm = mid, m
			}
			lb, lm = mid, m
			if verdict == "PASS" {
				bestN = mid
				hi = mid - 1
			} else {
				lo = mid + 1
			}
			_ = os.Remove(cand)
		}
		if bestN == 0 {
			bestN = c.BRMax
			e.Audio.PlateauHint(filepath.Base(input), fb, fm, lb, lm)
			e.Log.Info("warning: no bitrate in [%d..%d]k reached %s %s%s; falling back to br=%dk",
				c.BRMin, c.BRMax, c.AudioMetric, c.AudioTargetRaw, c.AudioUnitLong, bestN)
		}
		final := filepath.Join(work, "final."+ext)
		if err := encode(bestN, final); err != nil {
			return failed()
		}
		m, err := metric(final)
		if err != nil {
			return result.Row{}, true, err
		}
		e.Audio.VisqolCleanup(work)
		best = strconv.Itoa(bestN)
		mean = audio.Score(m)
		passed = util.AtLeast(m, c.TargetAudio)
		verdict := "FAIL"
		if passed {
			verdict = "PASS"
		}
		e.Log.Info("final : br=%dk %s=%s%s %s", bestN, c.AudioMetric, mean, c.AudioUnit, verdict)
		e.TUI.Attempt("final", bestN, m, c.TargetAudio, verdict)
	}

	final := filepath.Join(work, "final."+ext)
	size := fileSize(final)
	if err := os.Rename(final, out); err != nil {
		return result.Row{}, false, err
	}

	if !c.NoJSON {
		doc := &report.AudioReport{
			Input: input, Output: out, Mode: "audio",
			Codec: acodec, Container: ext, BitrateKbps: best,
			Metric: c.AudioMetric, Passed: passed, SizeBytes: size,
		}
		if c.AudioMetric == "visqol" {
			t := report.F(c.TargetAudio)
			m := mean
			doc.TargetMOS, doc.MeanMOS = &t, &m
			doc.VisqolModel = c.VisqolModel
		} else {
			t := report.F(c.TargetAudio)
			m := mean
			doc.TargetDB, doc.MeanDB = &t, &m
		}
		if err := report.Write(rep, doc); err != nil {
			return result.Row{}, false, err
		}
	}

	status := "PASS"
	failedGate := false
	if !passed {
		status = "FAIL (gate)"
		failedGate = true
		e.TUI.Message("did not reach %s target %s%s (best br=%sk, %s=%s%s)",
			c.AudioMetric, c.AudioTargetRaw, c.AudioUnitLong, best, c.AudioMetric, mean, c.AudioUnit)
	}
	return result.Row{
		Output: filepath.Base(out), Mode: "audio", Q: best, VMAF: mean,
		Size:   util.HumanSize(inSize) + "->" + util.HumanSize(size),
		Status: status,
	}, failedGate, nil
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}
