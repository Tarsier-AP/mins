// Package ffmpeg runs the bundled ffmpeg/ffprobe and renders the same live
// progress bar mins.sh showed.
package ffmpeg

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"mins/internal/logx"
)

// Runner invokes ffmpeg and ffprobe.
type Runner struct {
	FFmpeg  string
	FFprobe string
	Log     *logx.Logger
	// Progress enables the \r-redrawn bar. Batch workers set it false so
	// concurrent workers do not fight over the terminal.
	Progress bool
	// Quiet suppresses the "[mins] label" line (workers report through the
	// dashboard instead).
	Quiet bool

	mu sync.Mutex
}

// New returns a runner.
func New(ffmpegPath, ffprobePath string, log *logx.Logger) *Runner {
	return &Runner{FFmpeg: ffmpegPath, FFprobe: ffprobePath, Log: log, Progress: true}
}

// Version returns the first line of ffmpeg -version.
func (r *Runner) Version() string {
	out, err := exec.Command(r.FFmpeg, "-version").Output()
	if err != nil {
		return ""
	}
	line := strings.SplitN(string(out), "\n", 2)[0]
	return line
}

// Run executes ffmpeg. label is shown as "[mins] label"; total (seconds)
// drives the progress bar. stderr is captured to logPath.
func (r *Runner) Run(label string, total float64, logPath string, args ...string) error {
	if !r.Quiet {
		fmt.Fprintf(os.Stderr, "[mins] %s\n", label)
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("cannot create %s: %w", logPath, err)
	}
	defer logFile.Close()

	full := []string{"-hide_banner", "-nostdin", "-y", "-nostats", "-loglevel", "error"}
	if r.Progress {
		full = append(full, "-stats_period", "0.5", "-progress", "pipe:1")
	}
	full = append(full, args...)

	cmd := exec.Command(r.FFmpeg, full...)
	cmd.Stderr = logFile
	var stdout io.ReadCloser
	if r.Progress {
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			return err
		}
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot start ffmpeg: %w", err)
	}
	drew := false
	if r.Progress && stdout != nil {
		sc := bufio.NewScanner(stdout)
		var curUS int64
		for sc.Scan() {
			k, v, ok := strings.Cut(sc.Text(), "=")
			if !ok {
				continue
			}
			switch k {
			case "out_time_us", "out_time_ms":
				curUS, _ = strconv.ParseInt(v, 10, 64)
			case "speed":
				if curUS > 0 {
					drawBar(curUS, int64(total*1e6), strings.TrimSuffix(v, "x"))
					drew = true
				}
			}
		}
		_, _ = io.Copy(io.Discard, stdout)
	}
	err = cmd.Wait()
	if drew {
		fmt.Fprintln(os.Stderr)
	}
	if err != nil {
		if r.Log != nil {
			tail := logx.Tail(logPath, 40)
			if tail != "" {
				fmt.Fprintln(os.Stderr, tail)
				r.Log.Append("--- ffmpeg stderr (tail) during: " + label + " ---")
				r.Log.Append(tail)
			}
		}
		return fmt.Errorf("ffmpeg failed during: %s (exit code %v)", label, exitCode(err))
	}
	if r.Log != nil && r.Log.Enabled() {
		r.Log.CopyLog(logPath, "ffmpeg")
	}
	return nil
}

// RunCapture runs ffmpeg and returns its combined output without progress.
func (r *Runner) RunCapture(args ...string) (string, error) {
	full := append([]string{"-hide_banner", "-nostdin", "-y", "-nostats"}, args...)
	out, err := exec.Command(r.FFmpeg, full...).CombinedOutput()
	return string(out), err
}

// Probe runs ffprobe with the given arguments and returns stdout.
func (r *Runner) Probe(args ...string) (string, error) {
	full := append([]string{"-v", "error"}, args...)
	out, err := exec.Command(r.FFprobe, full...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func exitCode(err error) int {
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

const barWidth = 34

func drawBar(curUS, totalUS int64, speed string) {
	if totalUS <= 0 {
		fmt.Fprint(os.Stderr, "\r  working...")
		return
	}
	p := float64(curUS) / float64(totalUS) * 100
	if p > 100 {
		p = 100
	}
	if p < 0 {
		p = 0
	}
	n := int(float64(barWidth)*p/100 + 0.5)
	var b strings.Builder
	for i := 0; i < barWidth; i++ {
		if i < n {
			b.WriteByte('#')
		} else {
			b.WriteByte('-')
		}
	}
	eta := "??:??:??"
	if sp, err := strconv.ParseFloat(speed, 64); err == nil && sp > 0 {
		eta = hms(float64(totalUS-curUS) / 1e6 / sp)
	}
	fmt.Fprintf(os.Stderr, "\r  [%s] %5.1f%% %s/%s %ss ETA %s ",
		b.String(), p, hms(float64(curUS)/1e6), hms(float64(totalUS)/1e6), speed, eta)
}

func hms(s float64) string {
	t := int(s + 0.5)
	return fmt.Sprintf("%02d:%02d:%02d", t/3600, (t%3600)/60, t%60)
}

// DrawFrames renders the frame-count sibling used by --fbf.
func DrawFrames(cur, total int, rate float64) {
	if total <= 0 {
		fmt.Fprint(os.Stderr, "\r  working...")
		return
	}
	p := float64(cur) / float64(total) * 100
	if p > 100 {
		p = 100
	}
	n := int(float64(barWidth)*p/100 + 0.5)
	var b strings.Builder
	for i := 0; i < barWidth; i++ {
		if i < n {
			b.WriteByte('#')
		} else {
			b.WriteByte('-')
		}
	}
	eta := "??:??:??"
	if rate > 0 {
		eta = hms(float64(total-cur) / rate)
	}
	fmt.Fprintf(os.Stderr, "\r  [%s] %5.1f%% %d/%d frames %.2f/s ETA %s ", b.String(), p, cur, total, rate, eta)
}
