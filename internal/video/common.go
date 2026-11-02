package video

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	"mins/internal/core"
	"mins/internal/escape"
	"mins/internal/media"
	"mins/internal/util"
	"mins/internal/vmaf"
)

var reDur = regexp.MustCompile(`^[0-9]+([.][0-9]+)?$`)

// probe is the subset of source metadata every video path needs.
type probe struct {
	W, H     int
	Res      string
	FPSNum   int
	FPSDen   int
	FPS      float64
	FPSInt   int
	Dur      float64
	DurStr   string
	Audio    int
	Subs     int
	NbFrames int
}

func probeInput(e *core.Env, input string) (*probe, error) {
	p := &probe{}
	v, err := media.ProbeVideo(e.R, input)
	if err != nil {
		return nil, err
	}
	p.W, p.H = v.Width, v.Height
	p.Res = fmt.Sprintf("%d,%d", v.Width, v.Height)
	p.FPSNum, p.FPSDen = v.FPSNum, v.FPSDen
	p.FPS = v.FPS
	if p.FPS > 0 {
		p.FPSInt = int(p.FPS + 0.5)
	}
	p.Audio = v.AudioTracks
	p.Subs = v.SubTracks
	p.NbFrames = v.NBFFrames
	p.DurStr, err = media.DurationRaw(e.R, input)
	if err != nil {
		p.DurStr = ""
	}
	if reDur.MatchString(p.DurStr) {
		p.Dur, _ = strconv.ParseFloat(p.DurStr, 64)
	}
	return p, nil
}

// trim computes the effective trim window exactly as process_file did.
// start/end are the user's raw flag values.
type trimWindow struct {
	SS, EN, T, ClipDur string
	Clip               float64
	Trimmed            bool
}

func computeTrim(e *core.Env, c *trimInputs) (*trimWindow, error) {
	tw := &trimWindow{SS: c.Start, EN: c.End, ClipDur: c.DurStr, Clip: c.Dur}
	if c.Start == "" && c.End == "" {
		return tw, nil
	}
	tw.Trimmed = true
	if tw.SS == "" {
		tw.SS = "0"
	}
	if tw.EN == "" {
		tw.Clip = c.Dur - c.StartFloat
		tw.ClipDur = fmt.Sprintf("%.3f", tw.Clip)
	} else {
		tw.T = fmt.Sprintf("%.3f", c.EndFloat-c.StartFloat)
		tw.ClipDur = tw.T
		tw.Clip, _ = strconv.ParseFloat(tw.T, 64)
	}
	if c.StartFloat >= c.Dur {
		return nil, core.Dief("trim past EOF")
	}
	if tw.Clip >= c.Dur {
		e.Log.Info("warning: trim window reaches past EOF (%ss); clamping", c.DurStr)
		tw.EN = c.DurStr
		tw.T = ""
		tw.Clip = c.Dur - c.StartFloat
		tw.ClipDur = fmt.Sprintf("%.3f", tw.Clip)
	}
	return tw, nil
}

type trimInputs struct {
	Start, End           string
	StartFloat, EndFloat float64
	DurStr               string
	Dur                  float64
}

// encoderArgs returns the video-encoder half of an encode command.
func encoderArgs(c *core.Env, crf int, tier string) []string {
	switch c.C.Encoder {
	case "svt":
		preset := c.C.SVTQuality
		if tier == "fast" {
			preset = c.C.SVTFast
		}
		return []string{"-c:v", "libsvtav1", "-preset", strconv.Itoa(preset),
			"-crf", strconv.Itoa(crf), "-svtav1-params", "tune=0"}
	case "aom":
		cpu := c.C.AOMQuality
		if tier == "fast" {
			cpu = c.C.AOMFast
		}
		return []string{"-c:v", "libaom-av1", "-crf", strconv.Itoa(crf),
			"-cpu-used", strconv.Itoa(cpu), "-row-mt", "1"}
	default:
		preset, mp := c.C.NVENCQuality, "fullres"
		if tier == "fast" {
			preset, mp = c.C.NVENCFast, "disabled"
		}
		return []string{"-c:v", "av1_nvenc", "-preset", strconv.Itoa(preset), "-tune", "hq",
			"-multipass", mp, "-rc", "vbr", "-cq", strconv.Itoa(crf), "-b:v", "0",
			"-spatial-aq", "1", "-aq-strength", strconv.Itoa(c.C.NVENCAQStrength)}
	}
}

func muxArgs(c *core.Env) []string {
	switch c.C.VidFmt {
	case "mkv":
		return []string{"-f", "matroska"}
	case "webm":
		return []string{"-f", "webm"}
	default:
		return []string{"-movflags", "+faststart", "-f", "mp4"}
	}
}

// vmafRun measures cand against the source (via insrc) and returns the
// pooled mean plus the number of compared frames.
func vmafRun(e *core.Env, cand string, insrc, durargs []string, w, h int,
	total float64, runLog, vlog, label string) (float64, int, error) {

	filter := fmt.Sprintf("[0:v]setpts=PTS-STARTPTS,crop=%d:%d:0:0,format=yuv420p[d];"+
		"[1:V]setpts=PTS-STARTPTS,crop=%d:%d:0:0,format=yuv420p[r];"+
		"[d][r]libvmaf=n_threads=%d:log_path=%s:log_fmt=json",
		w, h, w, h, e.C.NProc, escape.Filter(vlog))

	args := []string{"-i", cand}
	args = append(args, insrc...)
	args = append(args, durargs...)
	args = append(args, "-lavfi", filter, "-f", "null", "-")
	if err := e.R.Run(label, total, runLog, args...); err != nil {
		return 0, 0, err
	}
	l, err := vmaf.Parse(vlog)
	if err != nil {
		return 0, 0, err
	}
	mean, n := l.Mean()
	return mean, n, nil
}

// expectedFrames is round(clip * fps), or 0 when fps is unknown.
func expectedFrames(clip float64, fps float64) int {
	if fps <= 0 {
		return 0
	}
	return int(clip*fps + 0.5)
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

// ---- shared descriptions ---------------------------------------------------

func audioDesc(audioTracks, keep int, kept string) string {
	if audioTracks > 0 {
		return fmt.Sprintf("audio %d/%d kept: %s", keep, audioTracks, kept)
	}
	return fmt.Sprintf("%d audio track(s)", audioTracks)
}

func subDesc(hasSubs, keep int, kept string) string {
	if hasSubs == 0 {
		return ""
	}
	if kept == "" {
		kept = "none"
	}
	return fmt.Sprintf(", subs %d/%d kept: %s", keep, hasSubs, kept)
}

func subPolicy(vidfmt string) string {
	switch vidfmt {
	case "mp4":
		return "mov_text"
	case "webm":
		return "webvtt"
	default:
		return "copy"
	}
}

// fmtMean renders a VMAF score the way Python's print did.
func fmtMean(v float64) string { return util.Float(v) }

// checkExpectedFrames warns when libvmaf compared a different number of
// frames than duration*fps predicts.
func checkExpectedFrames(e *core.Env, n, expected, fpsInt int) {
	if expected <= 0 || n <= 0 {
		return
	}
	signed := n - expected
	adiff := signed
	if adiff < 0 {
		adiff = -adiff
	}
	if fpsInt > 0 && adiff*2 > fpsInt {
		e.Log.Info("warning: vmaf compared %d frames but ~%d were expected from duration*fps (diff %d) - check for VFR/frame-timing mismatch between source and encode", n, expected, signed)
	}
}
