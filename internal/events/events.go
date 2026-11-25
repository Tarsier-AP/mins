// Package events writes the NDJSON progress stream consumed by external
// frontends such as mins-gui. It is additive: stdout and stderr are unchanged.
package events

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Version is the stream schema version. Bump it on breaking changes.
const Version = 1

// Writer appends one JSON object per line. A nil *Writer is a no-op, so callers
// never need to branch on whether --events was given.
type Writer struct {
	mu sync.Mutex
	f  *os.File
}

// Open truncates path and returns a writer. An empty path returns nil.
func Open(path string) (*Writer, error) {
	if path == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	return &Writer{f: f}, nil
}

// Emit writes one event line. Writes are flushed by the OS; a tailer sees each
// line as soon as it is written.
func (w *Writer) Emit(kind string, fields map[string]any) {
	if w == nil {
		return
	}
	ev := make(map[string]any, len(fields)+3)
	for k, v := range fields {
		ev[k] = v
	}
	ev["v"] = Version
	ev["t"] = kind
	ev["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	w.mu.Lock()
	_, _ = w.f.Write(append(b, '\n'))
	w.mu.Unlock()
}

// Log mirrors one already-formatted "[mins] ..." line.
func (w *Writer) Log(line string) {
	w.Emit("log", map[string]any{"line": line})
}

// Close closes the stream.
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}
