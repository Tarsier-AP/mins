package tools

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func gz(s string) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.Bytes()
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func assets(t *testing.T) fstest.MapFS {
	t.Helper()
	tool := []byte("tool-binary")
	other := []byte("other-binary")
	manifest := []byte(sum(tool) + "  tool\n" + sum(other) + "  other\n")
	return fstest.MapFS{
		"assets/testplat/tool.gz":        {Data: gz("tool-binary")},
		"assets/testplat/other":          {Data: other},
		"assets/testplat/sha256sums.txt": {Data: manifest},
		"assets/testplat/.keep":          {Data: nil},
	}
}

func TestPrepareExtractsAndVerifies(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MINS_TOOLS_DIR", dir)
	r := New(assets(t), "testplat")
	if err := r.Prepare(); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"tool": "tool-binary", "other": "other-binary"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
		if st, _ := os.Stat(filepath.Join(dir, name)); st.Mode().Perm()&0o100 == 0 {
			t.Errorf("%s is not executable", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".keep")); !os.IsNotExist(err) {
		t.Errorf(".keep should not be extracted")
	}
}

func TestPrepareReExtractsCorruptCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MINS_TOOLS_DIR", dir)
	if err := New(assets(t), "testplat").Prepare(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tool"), []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := New(assets(t), "testplat").Prepare(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "tool"))
	if string(got) != "tool-binary" {
		t.Errorf("corrupt cache was not repaired: %q", got)
	}
}

func TestPrepareRejectsHashMismatch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MINS_TOOLS_DIR", dir)
	m := assets(t)
	m["assets/testplat/sha256sums.txt"] = &fstest.MapFile{
		Data: []byte("0000000000000000000000000000000000000000000000000000000000000000  tool\n"),
	}
	err := New(m, "testplat").Prepare()
	if err == nil {
		t.Fatal("expected a SHA-256 verification error")
	}
}

func TestPathUsesToolsDirAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MINS_TOOLS_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "other"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := New(fstest.MapFS{}, "testplat")
	if p, err := r.Path("other"); err != nil || p != filepath.Join(dir, "other") {
		t.Fatalf("Path(other) = %q, %v", p, err)
	}
	override := filepath.Join(dir, "custom-ffmpeg")
	if err := os.WriteFile(override, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MINS_FFMPEG", override)
	if p, err := r.Path("ffmpeg"); err != nil || p != override {
		t.Fatalf("Path(ffmpeg) = %q, %v", p, err)
	}
	t.Setenv("MINS_FFMPEG", filepath.Join(dir, "missing"))
	if _, err := New(fstest.MapFS{}, "testplat").Path("ffmpeg"); err == nil {
		t.Fatal("expected an error for a missing override")
	}
}
