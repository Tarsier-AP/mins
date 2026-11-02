// Package logx implements the run log and the "[mins] ..." progress lines
// mins.sh produced. It is safe for concurrent use.
package logx

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Logger writes status lines to stdout and, when enabled, mirrors them to a
// run log. Raw append lines go to the log only.
type Logger struct {
	mu    sync.Mutex
	file  *os.File
	Path  string
	Dir   string
	RunID string
	Out   io.Writer
	Err   io.Writer
}

// Open opens (creating as needed) the run log at path. An empty path gives a
// logger that only writes to the terminal.
func Open(path string) (*Logger, error) {
	l := &Logger{Out: os.Stdout, Err: os.Stderr}
	if path == "" {
		return l, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	l.file = f
	l.Path = path
	l.Dir, _ = filepath.Abs(filepath.Dir(path))
	l.RunID = time.Now().Format("20060102-150405") + fmt.Sprintf("-%d", os.Getpid())
	_ = os.MkdirAll(filepath.Join(l.Dir, "mins-logs"), 0o755)
	return l, nil
}

// Close flushes and closes the run log.
func (l *Logger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}

// Enabled reports whether a run log is active.
func (l *Logger) Enabled() bool { return l != nil && l.file != nil }

// Header writes the "==== mins run ====" block once per top-level run.
func (l *Logger) Header(version string, argv []string, cwd string) {
	if !l.Enabled() {
		return
	}
	l.Append("==== mins run " + time.Now().Format("2006-01-02 15:04:05 MST") + " ====")
	l.Append("version: " + version)
	l.Append("argv: " + strings.Join(argv, " "))
	l.Append("cwd : " + cwd)
}

// Info prints "[mins] ..." to stdout and mirrors it to the log.
func (l *Logger) Info(format string, args ...any) {
	line := "[mins] " + fmt.Sprintf(format, args...)
	l.mu.Lock()
	fmt.Fprintln(l.Out, line)
	l.mu.Unlock()
	l.Append(line)
}

// Warn is Info with the "warning:" convention left to the caller.
func (l *Logger) Warn(format string, args ...any) {
	l.Info("warning: "+format, args...)
}

// Error prints "mins: error: ..." to stderr and the log.
func (l *Logger) Error(format string, args ...any) {
	line := "mins: error: " + fmt.Sprintf(format, args...)
	l.mu.Lock()
	fmt.Fprintln(l.Err, line)
	l.mu.Unlock()
	l.Append(line)
}

// Append writes one raw line to the run log (no prefix).
func (l *Logger) Append(line string) {
	if !l.Enabled() {
		return
	}
	l.mu.Lock()
	fmt.Fprintln(l.file, line)
	l.mu.Unlock()
}

// CopyLog preserves a tool's raw stderr under <log dir>/mins-logs/.
func (l *Logger) CopyLog(src, name string) {
	if !l.Enabled() || src == "" {
		return
	}
	data, err := os.ReadFile(src)
	if err != nil || len(data) == 0 {
		return
	}
	dst := filepath.Join(l.Dir, "mins-logs", l.RunID+"_"+name+".log")
	_ = os.WriteFile(dst, data, 0o644)
}

// Tail returns the last n lines of a log file, for error reporting.
func Tail(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
