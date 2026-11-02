package vmaf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAndMean(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v.json")
	data := `{"pooled_metrics":{"vmaf":{"min":90.0,"max":99.0,"mean":95.25}},"frames":[` +
		`{"frameNum":1,"metrics":{"vmaf":96.0}},` +
		`{"frameNum":0,"metrics":{"vmaf":94.5}}]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	mean, n := l.Mean()
	if mean != 95.25 || n != 2 {
		t.Errorf("Mean = %v, %d", mean, n)
	}
	scores := l.Scores()
	if len(scores) != 2 || scores[0] != 94.5 || scores[1] != 96.0 {
		t.Errorf("Scores = %v", scores)
	}
}

func TestParseRejectsMissingMetric(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v.json")
	if err := os.WriteFile(path, []byte(`{"pooled_metrics":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(path); err == nil {
		t.Fatal("expected an error")
	}
}
