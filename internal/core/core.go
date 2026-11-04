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

	// QState carries the adaptive image quality window across a --q-auto
	// batch. It is nil when --q-auto is off.
	QState *QState
}

// MaxQuality is the highest encoder quality the adaptive search may reach.
const MaxQuality = 100

// QState is the shared, thread-safe adaptive quality window used by --q-auto.
// qmin is the first probe for the next input (not a hard lower bound), qmax is
// the upper bound, and first reports that no successful input has been seen
// yet, so the first input still starts from the midpoint of the whole range.
type QState struct {
	mu    sync.Mutex
	qmin  int
	qmax  int
	first bool
}

// NewQState returns a window starting at the configured q-min/q-max.
func NewQState(qmin, qmax int) *QState {
	if qmax > MaxQuality {
		qmax = MaxQuality
	}
	if qmin > qmax {
		qmin = qmax
	}
	return &QState{qmin: qmin, qmax: qmax, first: true}
}

// Window returns the upper bound and the first probe for the next input. seed
// is -1 when the caller should start from the midpoint (first input, or an
// empty window). lo is the encoder's minimum quality.
func (s *QState) Window(lo int) (hi, seed int) {
	if s == nil {
		return 0, -1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	hi = s.qmax
	if hi < lo {
		hi = lo
	}
	if hi > MaxQuality {
		hi = MaxQuality
	}
	if s.first {
		return hi, -1
	}
	seed = s.qmin
	if seed < lo {
		seed = lo
	}
	if seed > hi {
		seed = hi
	}
	return hi, seed
}

// Record folds a passing quality into the shared window for the next input:
// the first result sets q-max, later results only raise it; q-min advances by
// at most 5 and never above the result.
func (s *QState) Record(q int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.first {
		s.qmax = q
		s.first = false
	} else if q > s.qmax {
		s.qmax = q
	}
	if next := s.qmin + 5; next < q {
		s.qmin = next
	} else {
		s.qmin = q
	}
}

// RecordFailure records that no quality up to q-max reached the target: the
// upper bound moves to 100 and q-min is left alone.
func (s *QState) RecordFailure() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.qmax = MaxQuality
	s.mu.Unlock()
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
		QState:      e.QState,
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
