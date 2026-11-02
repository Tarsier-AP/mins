// Package app wires the resolved configuration to the processing paths and
// runs the batch.
package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"mins/internal/audio"
	"mins/internal/audioproc"
	"mins/internal/config"
	"mins/internal/core"
	"mins/internal/ffmpeg"
	"mins/internal/image"
	"mins/internal/logx"
	"mins/internal/media"
	"mins/internal/native"
	"mins/internal/result"
	"mins/internal/tools"
	"mins/internal/tui"
	"mins/internal/util"
	"mins/internal/video"
)

// Run executes a whole run and returns the process exit code.
func Run(c *config.Config, log *logx.Logger) int {
	embedded, subdir := tools.Embedded()
	tr := tools.New(embedded, subdir)

	ffmpegPath, err := tr.Path("ffmpeg")
	if err != nil {
		log.Error("%s", err)
		return 2
	}
	ffprobePath, err := tr.Path("ffprobe")
	if err != nil {
		log.Error("%s", err)
		return 2
	}

	e := &core.Env{
		C:       c,
		Log:     log,
		R:       ffmpeg.New(ffmpegPath, ffprobePath, log),
		Tools:   tr,
		Table:   result.New(),
		Version: c.Version,
	}
	e.Audio = &audio.Engine{R: e.R, Log: log, C: c}
	e.Img = native.New(c, e.R, tr, log)

	log.Append("ffmpeg: " + e.R.Version())

	// --img-encoder auto is settled once, here, in the parent.
	if c.ImgEncoder == "auto" && c.NeedImage {
		c.ImgBackend = map[string]string{"webp": "libwebp", "avif": "libavif", "jxl": "libjxl"}[c.ImgFmt]
		e.Img = native.New(c, e.R, tr, log)
	}
	if c.NeedImage && c.ImgFmt == "jxl" && c.QMax < 5 {
		log.Error("--q-max must be >= 5 with --img-encoder libjxl (cjxl can't encode below quality 5)")
		return 2
	}

	for _, w := range c.Warnings {
		log.Info("%s", w)
	}

	if err := preflight(c, e); err != nil {
		log.Error("%s", err)
		return 2
	}

	// Work out which path each positional input takes.
	type job struct {
		idx   int
		input string
		mode  string
	}
	var jobs []job
	for i, input := range c.Positional {
		fm := c.Mode
		if fm == "auto" {
			fm = util.Classify(input)
		}
		jobs = append(jobs, job{idx: i, input: input, mode: fm})
	}

	eligible := 0
	for _, j := range jobs {
		if j.mode == "image" || (j.mode == "video" && !c.FBF) {
			eligible++
		}
	}
	parallel := !c.FBF && c.WorkersExplicit && c.Workers > 1 && eligible > 1

	// The dashboard replaces minsview. It is only useful where no ffmpeg
	// progress bar is already drawing on stderr.
	dashMode := "batch"
	hasImg, hasVid := false, false
	for _, j := range jobs {
		if j.mode == "image" || j.mode == "animate" {
			hasImg = true
		}
		if j.mode == "video" {
			hasVid = true
		}
	}
	if hasImg && !hasVid {
		dashMode = "image"
	} else if hasVid && !hasImg {
		dashMode = "video"
	}
	dashOn := !c.NoTUI && tui.IsTerminal(os.Stderr) && (parallel || c.FBF)
	fileLabel := "batch"
	if len(jobs) == 1 {
		fileLabel = filepath.Base(jobs[0].input)
	}
	dash := tui.New(dashOn, dashMode, fileLabel, len(jobs), c.Workers)
	e.TUI = dash
	dash.Start()

	var (
		done    atomic.Int64
		failedN atomic.Int64
		fatal   atomic.Bool
	)
	fatalErr := make(chan error, 1)

	runOne := func(env *core.Env, j job) {
		var row result.Row
		var failed bool
		var err error
		switch j.mode {
		case "subtitle":
			row = result.Row{Output: filepath.Base(j.input), Mode: "subs", Q: "-", VMAF: "-", Size: "-",
				Status: "ERROR: standalone subtitle file (embedded subs: --stracks)"}
			failed = true
		case "image":
			row, failed, err = image.Process(env, j.input)
		case "animate":
			row, failed, err = image.ProcessAnimate(env, j.input)
		case "audio":
			row, failed, err = audioproc.Process(env, j.input)
		default:
			if c.FBF {
				row, failed, err = video.ProcessFBF(env, j.input)
			} else {
				row, failed, err = video.ProcessFile(env, j.input)
			}
		}
		if err != nil {
			fatal.Store(true)
			select {
			case fatalErr <- err:
			default:
			}
			return
		}
		e.Table.Set(j.idx, row)
		if failed {
			e.Table.Fail()
			failedN.Add(1)
		}
		n := int(done.Add(1))
		dash.Progress(n, int(failedN.Load()))
		dash.Done(filepath.Base(j.input), row.Status, "")
	}

	if parallel {
		threads := c.NProc / c.Workers
		if threads < 1 {
			threads = 1
		}
		var wg sync.WaitGroup
		sem := make(chan struct{}, c.Workers)
		for _, j := range jobs {
			if !(j.mode == "image" || (j.mode == "video" && !c.FBF)) {
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(j job) {
				defer wg.Done()
				defer func() { <-sem }()
				if fatal.Load() {
					return
				}
				runOne(e.Clone(threads), j)
			}(j)
		}
		wg.Wait()
	}

	for _, j := range jobs {
		if fatal.Load() {
			break
		}
		if parallel && (j.mode == "image" || (j.mode == "video" && !c.FBF)) {
			continue
		}
		runOne(e, j)
	}

	dash.Close()

	if fatal.Load() {
		select {
		case err := <-fatalErr:
			log.Error("%s", err)
		default:
		}
		return 2
	}

	e.Table.Print(log)
	if e.Table.Failed() > 0 {
		return 1
	}
	return 0
}

func preflight(c *config.Config, e *core.Env) error {
	needAudioGate := c.NeedAudio || (c.AudioSearch && c.NeedVideo)
	if needAudioGate {
		if c.AudioMetric == "visqol" {
			bin := os.Getenv("VISQOL_BIN")
			if bin == "" {
				p, err := e.Tools.Path("visqol")
				if err != nil {
					return fmt.Errorf("--audio-metric visqol needs the bundled 'visqol' executable (or set VISQOL_BIN); use --audio-metric asisdr to gate with ffmpeg alone")
				}
				bin = p
			}
			e.VisqolBin = bin
			e.Audio.VisqolBin = bin
			model := c.VisqolModel
			if model == "" {
				p, err := e.Tools.Model()
				if err != nil {
					return fmt.Errorf("ViSQOL model file not readable: %w", err)
				}
				model = p
			}
			if st, err := os.Stat(model); err != nil || st.IsDir() {
				return fmt.Errorf("ViSQOL model file not readable: %s (point --visqol-model, or the VISQOL_MODEL environment variable, at %s)", model, tools.ModelName)
			}
			c.VisqolModel = model
			e.Audio.C.VisqolModel = model
		} else {
			out, _ := exec.Command(e.R.FFmpeg, "-hide_banner", "-filters").Output()
			if !strings.Contains(string(out), " "+c.AudioMetric+" ") {
				return fmt.Errorf("audio gating needs ffmpeg's %s filter, not present in this ffmpeg build (asdr needs ffmpeg >= 5.0; apsnr/asisdr need ffmpeg >= 6.1) -- try --audio-metric asdr on an older ffmpeg, or upgrade", c.AudioMetric)
			}
		}
	}

	if c.NeedImage {
		animated := c.NeedAnim
		if !animated {
			for _, f := range c.Positional {
				mode := c.Mode
				if mode == "auto" {
					mode = util.Classify(f)
				}
				if mode != "image" {
					continue
				}
				if info, err := media.ProbeImage(e.R, f); err == nil && info.Frames > 1 {
					animated = true
					break
				}
			}
		}
		work, err := os.MkdirTemp("", "mins-preflight-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(work)
		if err := e.Img.Preflight(animated, work); err != nil {
			return err
		}
		suffix := ""
		if animated {
			suffix = ", animated"
		}
		e.Log.Info("%s preflight OK (%s%s)", c.ImgBackend, e.Img.Version(), suffix)
	}

	if c.Encoder == "nvenc" && c.NeedVideo {
		out, _ := exec.Command(e.R.FFmpeg, "-hide_banner", "-encoders").Output()
		if !strings.Contains(string(out), "av1_nvenc") {
			return fmt.Errorf("this ffmpeg build has no av1_nvenc")
		}
		probe := exec.Command(e.R.FFmpeg, "-hide_banner", "-v", "error",
			"-f", "lavfi", "-i", "color=black:s=256x256:d=0.2", "-pix_fmt", "yuv420p10le",
			"-c:v", "av1_nvenc", "-preset", "p1", "-f", "null", "-")
		if b, err := probe.CombinedOutput(); err != nil {
			if len(b) > 0 {
				fmt.Fprintln(os.Stderr, string(b))
			}
			return fmt.Errorf("av1_nvenc init failed — needs an Ada (RTX 40xx+) GPU and driver 520+")
		}
		e.Log.Info("av1_nvenc preflight OK")
	}
	return nil
}
