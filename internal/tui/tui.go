// Package tui implements the built-in live dashboard that replaces the
// external minsview renderer from mins.sh 1.17.0.
package tui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const blockLines = 7

// Dashboard draws a fixed-height, in-place progress view on stderr.
type Dashboard struct {
	mu      sync.Mutex
	enabled bool
	mode    string
	file    string
	total   int
	workers int
	done    int
	errors  int
	recent  []string
	drawn   int
	stop    chan struct{}
	doneCh  chan struct{}
	started bool
}

// IsTerminal reports whether f is a character device.
func IsTerminal(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// New returns a dashboard. enabled should already include --no-tui and the
// terminal check.
func New(enabled bool, mode, file string, total, workers int) *Dashboard {
	return &Dashboard{
		enabled: enabled,
		mode:    mode,
		file:    file,
		total:   total,
		workers: workers,
		stop:    make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
}

// Enabled reports whether the dashboard will draw anything.
func (d *Dashboard) Enabled() bool { return d != nil && d.enabled }

// Start begins periodic redraws.
func (d *Dashboard) Start() {
	if !d.Enabled() || d.started {
		return
	}
	d.started = true
	go func() {
		defer close(d.doneCh)
		t := time.NewTicker(400 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-d.stop:
				d.draw(true)
				return
			case <-t.C:
				d.draw(false)
			}
		}
	}()
}

// Close stops redraws and leaves the final frame on screen.
func (d *Dashboard) Close() {
	if !d.Enabled() {
		return
	}
	if d.started {
		select {
		case <-d.stop:
		default:
			close(d.stop)
		}
		<-d.doneCh
	}
	fmt.Fprintln(os.Stderr)
}

// SetTotal updates the denominator (e.g. when --fbf discovers the real frame
// count differs from its estimate).
func (d *Dashboard) SetTotal(n int) {
	if !d.Enabled() {
		return
	}
	d.mu.Lock()
	d.total = n
	d.mu.Unlock()
}

// Progress sets the completed and failed counts.
func (d *Dashboard) Progress(done, errors int) {
	if !d.Enabled() {
		return
	}
	d.mu.Lock()
	d.done, d.errors = done, errors
	d.mu.Unlock()
}

// Message records one line for the recent-events pane.
func (d *Dashboard) Message(format string, args ...any) {
	if !d.Enabled() {
		return
	}
	line := fmt.Sprintf(format, args...)
	d.mu.Lock()
	d.recent = append(d.recent, line)
	if len(d.recent) > 3 {
		d.recent = d.recent[len(d.recent)-3:]
	}
	d.mu.Unlock()
}

// Attempt records a per-candidate event (used by --fbf and the batch path).
func (d *Dashboard) Attempt(label string, crf int, vmaf, target float64, verdict string) {
	d.Message("%s q=%d vmaf=%.2f target=%.2f %s", label, crf, vmaf, target, verdict)
}

// Done records a completed unit.
func (d *Dashboard) Done(label, status, msg string) {
	d.Message("%s %s %s", label, status, msg)
}

func (d *Dashboard) draw(final bool) {
	d.mu.Lock()
	total, done, errors := d.total, d.done, d.errors
	recent := append([]string(nil), d.recent...)
	file, mode, workers := d.file, d.mode, d.workers
	d.mu.Unlock()

	pct := 0.0
	if total > 0 {
		pct = float64(done) / float64(total) * 100
		if pct > 100 {
			pct = 100
		}
	}
	width := 34
	n := int(float64(width)*pct/100 + 0.5)
	bar := strings.Repeat("#", n) + strings.Repeat("-", width-n)
	lines := []string{
		fmt.Sprintf("mins %s: %s", mode, truncate(file, 60)),
		fmt.Sprintf("  [%s] %5.1f%% %d/%d  %d error(s)  %d worker(s)", bar, pct, done, total, errors, workers),
		"",
	}
	for i := 0; i < 3; i++ {
		msg := ""
		if i < len(recent) {
			msg = "  " + truncate(recent[len(recent)-1-i], 78)
		}
		lines = append(lines, msg)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.drawn > 0 {
		fmt.Fprintf(os.Stderr, "\x1b[%dA", d.drawn)
	}
	for _, line := range lines {
		fmt.Fprint(os.Stderr, "\x1b[2K"+line+"\n")
	}
	d.drawn = len(lines)
	_ = final
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 4 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
