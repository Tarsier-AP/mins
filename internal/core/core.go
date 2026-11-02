// Package core holds the shared runtime environment passed to every
// processing path, plus the temporary-directory cleanup registry.
package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"mins/internal/audio"
	"mins/internal/config"
	"mins/internal/ffmpeg"
	"mins/internal/logx"
	"mins/internal/native"
	"mins/internal/result"
	"mins/internal/tools"
	"mins/internal/tui"
)

// Env is the resolved run state shared by the video, image and audio paths.
type Env struct {
	C     *config.Config
	Log   *logx.Logger
	R     *ffmpeg.Runner
	Tools *tools.Resolver
	Audio *audio.Engine
	Img   *native.Backend
	Table *result.Table
	TUI   *tui.Dashboard

	VisqolBin   string
	VisqolModel string
	Version     string
}

// Clone returns a copy suitable for one concurrent worker: the config's
// thread count is divided down so N workers do not oversubscribe the
// machine, and the progress bar is disabled so workers do not fight over
// the terminal.
func (e *Env) Clone(nproc int) *Env {
	c := *e.C
	if nproc >= 1 {
		c.NProc = nproc
	}
	r := ffmpeg.New(e.R.FFmpeg, e.R.FFprobe, e.Log)
	r.Progress = false
	r.Quiet = true
	ne := &Env{
		C:           &c,
		Log:         e.Log,
		R:           r,
		Tools:       e.Tools,
		Table:       e.Table,
		TUI:         e.TUI,
		VisqolBin:   e.VisqolBin,
		VisqolModel: e.VisqolModel,
		Version:     e.Version,
	}
	ne.Audio = &audio.Engine{R: r, Log: e.Log, C: &c, VisqolBin: e.VisqolBin}
	ne.Img = native.New(&c, r, e.Tools, e.Log)
	return ne
}

// ---- temporary directories -------------------------------------------------

var (
	cleanupMu   sync.Mutex
	cleanupDirs []string
)

// TempDir creates a temporary directory next to input (same filesystem, as
// mins.sh's mktemp -d "$DIR/.mins.XXXXXX" did) and registers it for cleanup.
func TempDir(input string) (string, error) {
	dir := filepath.Dir(input)
	d, err := os.MkdirTemp(dir, ".mins.")
	if err != nil {
		return "", err
	}
	// ffmpeg resolves relative paths in a concat list against the list's
	// own directory, so an absolute work dir is required for the fbf/refine
	// assembly (mins.sh's WORK was absolute too).
	if abs, err := filepath.Abs(d); err == nil {
		d = abs
	}
	cleanupMu.Lock()
	cleanupDirs = append(cleanupDirs, d)
	cleanupMu.Unlock()
	return d, nil
}

// Cleanup removes every registered temporary directory.
func Cleanup() {
	cleanupMu.Lock()
	dirs := cleanupDirs
	cleanupDirs = nil
	cleanupMu.Unlock()
	for _, d := range dirs {
		_ = os.RemoveAll(d)
	}
}

// AbsDir returns the absolute directory of path.
func AbsDir(path string) (string, error) {
	return filepath.Abs(filepath.Dir(path))
}

// Fatal is a die()-equivalent error: the run cannot continue.
type Fatal struct{ Msg string }

func (f *Fatal) Error() string { return f.Msg }

// Dief returns a fatal error.
func Dief(format string, args ...any) error {
	return &Fatal{Msg: fmt.Sprintf(format, args...)}
}
