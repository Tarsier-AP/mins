package events

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func readEvents(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
	s := bufio.NewScanner(f)
	for s.Scan() {
		var ev map[string]any
		if err := json.Unmarshal(s.Bytes(), &ev); err != nil {
			t.Fatalf("invalid NDJSON line %q: %v", s.Text(), err)
		}
		out = append(out, ev)
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestWriterEmitsNDJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "run.ndjson")
	w, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	w.Emit("run_start", map[string]any{"total": 2})
	w.Log("[mins] hello")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	evs := readEvents(t, path)
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2", len(evs))
	}
	for _, ev := range evs {
		if ev["v"] != float64(Version) {
			t.Errorf("v = %v, want %d", ev["v"], Version)
		}
		if _, err := time.Parse(time.RFC3339Nano, ev["ts"].(string)); err != nil {
			t.Errorf("bad ts %v: %v", ev["ts"], err)
		}
	}
	if evs[0]["t"] != "run_start" || evs[0]["total"] != float64(2) {
		t.Errorf("run_start = %v", evs[0])
	}
	if evs[1]["t"] != "log" || evs[1]["line"] != "[mins] hello" {
		t.Errorf("log = %v", evs[1])
	}
}

func TestWriterConcurrentLinesStayWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.ndjson")
	w, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w.Emit("file_start", map[string]any{"index": i})
		}(i)
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got := len(readEvents(t, path)); got != 50 {
		t.Fatalf("got %d events, want 50", got)
	}
}

func TestNilWriterIsNoop(t *testing.T) {
	w, err := Open("")
	if err != nil || w != nil {
		t.Fatalf("Open(\"\") = %v, %v", w, err)
	}
	w.Emit("run_start", nil)
	w.Log("x")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}
