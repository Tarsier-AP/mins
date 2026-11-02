//go:build integration

// Package integration exercises the built mins binary end to end against
// synthetic FFmpeg fixtures, and optionally against mins.sh for parity.
//
// Run with:
//
//	go test -tags=integration ./internal/integration
//	MINS_SH=/path/to/mins.sh go test -tags=integration ./internal/integration
//	MINS_REAL_CLIP=/path/to/clip.mp4 MINS_SH=/path/to/mins.sh go test -tags=integration ./internal/integration
package integration

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var repoRoot string

func TestMain(m *testing.M) {
	var err error
	repoRoot, err = filepath.Abs("../..")
	if err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// minsBin builds the binary once per test run.
func minsBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "mins")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/mins")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// fixtures generates (or reuses) the synthetic media used by every test.
func fixtures(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH; run `make fixtures` on a machine that has it")
	}
	dir := filepath.Join(repoRoot, "testdata", "generated")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	type spec struct {
		name string
		args []string
	}
	specs := []spec{
		{"in.png", []string{"-f", "lavfi", "-i", "testsrc2=s=320x240:d=1", "-frames:v", "1"}},
		{"anim.gif", []string{"-f", "lavfi", "-i", "testsrc2=s=160x120:d=1:r=5", "-vf", "fps=5"}},
		{"clip.mp4", []string{"-f", "lavfi", "-i", "testsrc2=s=160x120:d=1:r=15",
			"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
			"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest"}},
		{"tone.flac", []string{"-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-c:a", "flac"}},
	}
	for _, s := range specs {
		path := filepath.Join(dir, s.name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		args := append([]string{"-hide_banner", "-loglevel", "error", "-y"}, s.args...)
		args = append(args, path)
		if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
			t.Fatalf("generate %s: %v\n%s", s.name, err, out)
		}
	}
	return dir
}

type result struct {
	code   int
	output string
}

func run(t *testing.T, bin, dir string, args ...string) result {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LC_NUMERIC=C", "LANG=C")
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run %s: %v", bin, err)
		}
	}
	return result{code: code, output: string(out)}
}

// messages keeps only the [mins] lines and the summary, dropping ffmpeg's
// progress bars (which use carriage returns and timing-dependent rates).
func messages(s string) []string {
	var out []string
	inSummary := false
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "[mins]"):
			out = append(out, line)
		case line == "OUTPUT" || strings.HasPrefix(line, "OUTPUT "):
			inSummary = true
			out = append(out, line)
		case inSummary && line != "":
			out = append(out, line)
		}
	}
	return out
}

func copyFixture(t *testing.T, src, dstDir, name string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(src, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dstDir, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readReport(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("report %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return m
}

func num(m map[string]any, key string) float64 {
	v, _ := m[key].(float64)
	return v
}

func TestGoImage(t *testing.T) {
	bin := minsBin(t)
	fix := fixtures(t)
	dir := t.TempDir()
	copyFixture(t, fix, dir, "in.png")
	r := run(t, bin, dir, "--no-tui", "--img-encoder", "libwebp", "in.png")
	if r.code != 0 {
		t.Fatalf("exit %d:\n%s", r.code, r.output)
	}
	rep := readReport(t, filepath.Join(dir, "in.vmaf.json"))
	if rep["output"] != filepath.Join(dir, "in.webp") {
		t.Errorf("output = %v", rep["output"])
	}
	if !rep["passed"].(bool) || num(rep, "mean_vmaf") < 95 {
		t.Errorf("gate: passed=%v mean=%v", rep["passed"], rep["mean_vmaf"])
	}
	if _, err := os.Stat(filepath.Join(dir, "in.webp")); err != nil {
		t.Error(err)
	}
	if !strings.Contains(r.output, "PASS") {
		t.Errorf("summary missing PASS:\n%s", r.output)
	}
}

func TestGoAnimated(t *testing.T) {
	bin := minsBin(t)
	fix := fixtures(t)
	dir := t.TempDir()
	copyFixture(t, fix, dir, "anim.gif")
	r := run(t, bin, dir, "--no-tui", "--img-encoder", "libwebp", "anim.gif")
	if r.code != 0 {
		t.Fatalf("exit %d:\n%s", r.code, r.output)
	}
	rep := readReport(t, filepath.Join(dir, "anim.vmaf.json"))
	if !rep["animated"].(bool) || num(rep, "frames") != 5 {
		t.Errorf("animated report: %v", rep)
	}
}

func TestGoAudio(t *testing.T) {
	bin := minsBin(t)
	fix := fixtures(t)
	dir := t.TempDir()
	copyFixture(t, fix, dir, "tone.flac")
	r := run(t, bin, dir, "--no-tui", "--audio-metric", "asisdr", "tone.flac")
	if r.code != 0 {
		t.Fatalf("exit %d:\n%s", r.code, r.output)
	}
	rep := readReport(t, filepath.Join(dir, "tone.audio.json"))
	if rep["mode"] != "audio" || !rep["passed"].(bool) {
		t.Errorf("audio report: %v", rep)
	}
	if _, err := os.Stat(filepath.Join(dir, "tone.opus")); err != nil {
		t.Error(err)
	}
}

// TestParity runs both implementations over the same fixture and compares the
// user-visible result. Set MINS_SH to the v1.17.0 script.
func TestParity(t *testing.T) {
	sh := os.Getenv("MINS_SH")
	if sh == "" {
		t.Skip("set MINS_SH=/path/to/mins.sh to run parity comparisons")
	}
	bin := minsBin(t)
	fix := fixtures(t)

	cases := []struct {
		name string
		args []string
		rep  string
	}{
		{"still image", []string{"--no-tui", "--img-encoder", "libwebp", "in.png"}, "in.vmaf.json"},
		{"animated image", []string{"--no-tui", "--img-encoder", "libwebp", "anim.gif"}, "anim.vmaf.json"},
		{"standalone audio", []string{"--no-tui", "--audio-metric", "asisdr", "tone.flac"}, "tone.audio.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shDir, goDir := t.TempDir(), t.TempDir()
			for _, d := range []string{shDir, goDir} {
				copyFixture(t, fix, d, tc.args[len(tc.args)-1])
			}
			shRes := run(t, "bash", shDir, append([]string{sh}, tc.args...)...)
			goRes := run(t, bin, goDir, tc.args...)
			if shRes.code != goRes.code {
				t.Fatalf("exit codes differ: sh=%d go=%d\n--- sh ---\n%s\n--- go ---\n%s",
					shRes.code, goRes.code, shRes.output, goRes.output)
			}
			shMsgs, goMsgs := messages(shRes.output), messages(goRes.output)
			if len(shMsgs) != len(goMsgs) {
				t.Fatalf("message count differs:\n--- sh ---\n%s\n--- go ---\n%s",
					strings.Join(shMsgs, "\n"), strings.Join(goMsgs, "\n"))
			}
			for i := range shMsgs {
				if shMsgs[i] != goMsgs[i] {
					t.Errorf("line %d differs:\n sh: %s\n go: %s", i, shMsgs[i], goMsgs[i])
				}
			}
			shRep := readReport(t, filepath.Join(shDir, tc.rep))
			goRep := readReport(t, filepath.Join(goDir, tc.rep))
			if shRep["passed"] != goRep["passed"] {
				t.Errorf("passed differs: %v vs %v", shRep["passed"], goRep["passed"])
			}
			for _, key := range []string{"mean_vmaf", "mean_mos", "mean_db"} {
				a, aok := shRep[key]
				b, bok := goRep[key]
				if !aok && !bok {
					continue
				}
				af, _ := a.(float64)
				bf, _ := b.(float64)
				if math.Abs(af-bf) > 0.1 {
					t.Errorf("%s differs: %v vs %v", key, af, bf)
				}
			}
			sa, _ := shRep["size_bytes"].(float64)
			sb, _ := goRep["size_bytes"].(float64)
			if sa > 0 && math.Abs(sa-sb)/sa > 0.03 {
				t.Errorf("size differs: %.0f vs %.0f", sa, sb)
			}
		})
	}
}

// TestParityVideo covers the AV1 search path. It is slow (several libvmaf
// passes per implementation) and skipped in -short mode.
func TestParityVideo(t *testing.T) {
	sh := os.Getenv("MINS_SH")
	if sh == "" {
		t.Skip("set MINS_SH=/path/to/mins.sh to run parity comparisons")
	}
	if testing.Short() {
		t.Skip("video parity is skipped in -short mode")
	}
	bin := minsBin(t)
	fix := fixtures(t)
	shDir, goDir := t.TempDir(), t.TempDir()
	for _, d := range []string{shDir, goDir} {
		copyFixture(t, fix, d, "clip.mp4")
	}
	args := []string{"--no-tui", "clip.mp4"}
	shRes := run(t, "bash", shDir, append([]string{sh}, args...)...)
	goRes := run(t, bin, goDir, args...)
	if shRes.code != goRes.code {
		t.Fatalf("exit codes differ: sh=%d go=%d\n--- sh ---\n%s\n--- go ---\n%s",
			shRes.code, goRes.code, shRes.output, goRes.output)
	}
	shMsgs, goMsgs := messages(shRes.output), messages(goRes.output)
	if strings.Join(shMsgs, "\n") != strings.Join(goMsgs, "\n") {
		t.Errorf("messages differ:\n--- sh ---\n%s\n--- go ---\n%s",
			strings.Join(shMsgs, "\n"), strings.Join(goMsgs, "\n"))
	}
	shRep := readReport(t, filepath.Join(shDir, "clip.vmaf.json"))
	goRep := readReport(t, filepath.Join(goDir, "clip.vmaf.json"))
	if shRep["passed"] != goRep["passed"] {
		t.Errorf("passed differs: %v vs %v", shRep["passed"], goRep["passed"])
	}
	if shRep["crf"] != goRep["crf"] {
		t.Errorf("crf differs: %v vs %v", shRep["crf"], goRep["crf"])
	}
	if math.Abs(num(shRep, "mean_vmaf")-num(goRep, "mean_vmaf")) > 0.1 {
		t.Errorf("mean_vmaf differs: %v vs %v", num(shRep, "mean_vmaf"), num(goRep, "mean_vmaf"))
	}
	sa, _ := shRep["size_bytes"].(float64)
	sb, _ := goRep["size_bytes"].(float64)
	if sa > 0 && math.Abs(sa-sb)/sa > 0.03 {
		t.Errorf("size differs: %.0f vs %.0f", sa, sb)
	}
}

// TestRealClip is the optional real-world hook. It compares a full video run
// on a clip supplied by the user (slow: several VMAF passes).
func TestRealClip(t *testing.T) {
	clip := os.Getenv("MINS_REAL_CLIP")
	sh := os.Getenv("MINS_SH")
	if clip == "" || sh == "" {
		t.Skip("set MINS_REAL_CLIP and MINS_SH to run the real-clip parity test")
	}
	if testing.Short() {
		t.Skip("real-clip parity is skipped in -short mode")
	}
	bin := minsBin(t)
	name := filepath.Base(clip)
	shDir, goDir := t.TempDir(), t.TempDir()
	for _, d := range []string{shDir, goDir} {
		b, err := os.ReadFile(clip)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"--no-tui", name}
	shRes := run(t, "bash", shDir, append([]string{sh}, args...)...)
	goRes := run(t, bin, goDir, args...)
	if shRes.code != goRes.code {
		t.Fatalf("exit codes differ: sh=%d go=%d\n--- sh ---\n%s\n--- go ---\n%s",
			shRes.code, goRes.code, shRes.output, goRes.output)
	}
	shMsgs, goMsgs := messages(shRes.output), messages(goRes.output)
	if strings.Join(shMsgs, "\n") != strings.Join(goMsgs, "\n") {
		t.Errorf("messages differ:\n--- sh ---\n%s\n--- go ---\n%s",
			strings.Join(shMsgs, "\n"), strings.Join(goMsgs, "\n"))
	}
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	shRep := readReport(t, filepath.Join(shDir, stem+".vmaf.json"))
	goRep := readReport(t, filepath.Join(goDir, stem+".vmaf.json"))
	if math.Abs(num(shRep, "mean_vmaf")-num(goRep, "mean_vmaf")) > 0.1 {
		t.Errorf("mean_vmaf differs: %v vs %v", num(shRep, "mean_vmaf"), num(goRep, "mean_vmaf"))
	}
	sa, _ := shRep["size_bytes"].(float64)
	sb, _ := goRep["size_bytes"].(float64)
	if sa > 0 && math.Abs(sa-sb)/sa > 0.03 {
		t.Errorf("size differs: %.0f vs %.0f", sa, sb)
	}
	fmt.Fprintf(os.Stderr, "real clip parity OK: %s\n", name)
}
