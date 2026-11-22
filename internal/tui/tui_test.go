package tui

import (
	"bytes"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

func testDash(total int) *Dashboard {
	d := &Dashboard{
		enabled: true,
		full:    true,
		color:   true,
		mode:    "video",
		file:    "clip.mp4",
		total:   total,
		workers: 2,
		out:     &bytes.Buffer{},
		sizeFn:  func() (int, int) { return 120, 30 },
		now:     func() time.Time { return time.Unix(1700000000, 0) },
		seed:    7,
		index:   map[string]*rowState{},
		stop:    make(chan struct{}),
		wake:    make(chan struct{}, 1),
		doneCh:  make(chan struct{}),
	}
	d.perm = randomPerm(d.seed)
	d.started = d.now()
	d.lastETA = d.started
	return d
}

func wallText(d *Dashboard) string {
	lines := d.wallLinesLocked()
	return strings.Join(lines[:len(lines)-1], "\n") // drop the legend
}

func TestWallFillsInRandomOrder(t *testing.T) {
	d := testDash(4)
	d.color = false

	if got := strings.Count(wallText(d), "██"); got != 0 {
		t.Fatalf("fraction 0 has %d filled cells, want 0", got)
	}
	if got := strings.Count(wallText(d), "░░"); got != wallCells {
		t.Fatalf("fraction 0 has %d pending cells, want %d", got, wallCells)
	}

	d.frac = 0.5
	d.total = 1
	if got := strings.Count(wallText(d), "██"); got != wallCells/2 {
		t.Fatalf("fraction 0.5 has %d filled cells, want %d", got, wallCells/2)
	}
	if got := strings.Count(wallText(d), "▒▒"); got != 1 {
		t.Fatalf("fraction 0.5 has %d in-progress cells, want 1", got)
	}

	d.frac = 1
	if got := strings.Count(wallText(d), "██"); got != wallCells {
		t.Fatalf("fraction 1 has %d filled cells, want %d", got, wallCells)
	}
}

func TestWallOrderIsSeededAndNotSequential(t *testing.T) {
	a := randomPerm(1)
	b := randomPerm(1)
	c := randomPerm(2)
	if len(a) != wallCells {
		t.Fatalf("perm length = %d, want %d", len(a), wallCells)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("same seed produced different permutations: %v vs %v", a, b)
		}
	}
	sequential := true
	for i := range a {
		if a[i] != i {
			sequential = false
		}
	}
	if sequential {
		t.Fatal("wall order is sequential, want a randomised permutation")
	}
	same := true
	for i := range a {
		if a[i] != c[i] {
			same = false
		}
	}
	if same {
		t.Fatal("different seeds produced the same permutation")
	}
}

func TestFullLayoutFitsAndShowsEveryPane(t *testing.T) {
	d := testDash(3)
	d.StartFile("a.mp4", "a.mp4", "video")
	d.StartFile("b.webp", "b.webp", "image")
	d.Attempt("a.mp4", "search", 28, 94.31, 1200, "PASS")
	d.Log("[mins] PASS a.mp4 crf=28 vmaf=94.31")
	d.done = 1
	d.frac = 0.5

	out := d.renderFullLocked(120, 30)
	lines := strings.Split(out, "\n")
	if len(lines) != 30 {
		t.Fatalf("rendered %d lines, want 30 (one per terminal row)", len(lines))
	}
	text := plain(out)
	for _, want := range []string{"mins video", "a.mp4", "94.31", "[mins] PASS", "┌", "┐"} {
		if !strings.Contains(text, want) {
			t.Fatalf("frame missing %q:\n%s", want, text)
		}
	}
	for i, line := range strings.Split(text, "\n") {
		if n := len([]rune(line)); n > 120 {
			t.Fatalf("line %d is %d columns wide, want <= 120", i, n)
		}
	}
}

func TestWallHiddenOnSmallTerminals(t *testing.T) {
	d := testDash(2)
	out := plain(d.renderFullLocked(80, 24))
	if strings.Contains(out, "┌") {
		t.Fatalf("wall drawn on an 80-column terminal:\n%s", out)
	}
	if !strings.Contains(out, "mins video") {
		t.Fatal("header missing on small terminal")
	}
}

func TestResizeClearsTheScreen(t *testing.T) {
	d := testDash(2)
	d.renderFullLocked(120, 30)
	out := d.renderFullLocked(100, 20)
	if !strings.Contains(out, "\x1b[2J") {
		t.Fatal("resize did not clear the screen")
	}
	if got := len(strings.Split(out, "\n")); got != 20 {
		t.Fatalf("resized frame has %d lines, want 20", got)
	}
}

func TestLogIsCapped(t *testing.T) {
	d := testDash(1)
	for i := 0; i < logCap+50; i++ {
		d.Log("line")
	}
	if len(d.logs) != logCap {
		t.Fatalf("log len = %d, want %d", len(d.logs), logCap)
	}
}

func TestCloseRestoresFullScreen(t *testing.T) {
	var buf bytes.Buffer
	d := testDash(1)
	d.out = &buf
	d.Start()
	d.Close()

	got := buf.String()
	if !strings.Contains(got, "\x1b[?1049h") {
		t.Fatal("did not enter the alternate screen")
	}
	if !strings.Contains(got, "\x1b[?1049l") {
		t.Fatal("did not leave the alternate screen")
	}
	if !strings.Contains(got, "\x1b[?25h") {
		t.Fatal("did not show the cursor again")
	}
}

func TestRestoreAllRestoresTheTerminal(t *testing.T) {
	var buf bytes.Buffer
	d := testDash(1)
	d.out = &buf
	restored := false
	d.rawRestore = func() { restored = true }
	register(d)
	defer unregister(d)

	RestoreAll()

	got := buf.String()
	if !strings.Contains(got, "\x1b[?1049l") || !strings.Contains(got, "\x1b[?25h") {
		t.Fatalf("RestoreAll did not restore the screen/cursor: %q", got)
	}
	if !restored {
		t.Fatal("RestoreAll did not restore the raw keyboard mode")
	}
}

func TestQRequestsQuit(t *testing.T) {
	d := testDash(1)
	var quit atomic.Int32
	SetQuit(func() { quit.Add(1) })
	defer SetQuit(nil)

	d.readKeys(strings.NewReader("xq"))
	if got := quit.Load(); got != 1 {
		t.Fatalf("q fired the quit hook %d times, want 1", got)
	}

	quit.Store(0)
	d.readKeys(strings.NewReader("xy"))
	if got := quit.Load(); got != 0 {
		t.Fatalf("other keys fired the quit hook %d times, want 0", got)
	}
}

func TestInlineFallbackDrawsWithoutAltScreen(t *testing.T) {
	var buf bytes.Buffer
	d := testDash(2)
	d.full = false
	d.sizeFn = func() (int, int) { return 0, 0 }
	d.out = &buf
	d.Start()
	d.Close()

	got := buf.String()
	if strings.Contains(got, "\x1b[?1049") {
		t.Fatal("inline fallback used the alternate screen")
	}
	if !strings.Contains(plain(got), "mins video: clip.mp4") {
		t.Fatalf("inline fallback missing the title:\n%s", plain(got))
	}
}

func TestDoneMarksTheRow(t *testing.T) {
	d := testDash(1)
	d.StartFile("a.mp4", "a.mp4", "video")
	d.Done("a.mp4", "PASS", 100)
	d.mu.Lock()
	r := d.index["a.mp4"]
	d.mu.Unlock()
	if r.active || r.status != "PASS" {
		t.Fatalf("row = %+v, want inactive PASS", r)
	}
	if d.frac != 1 {
		t.Fatalf("single-file fraction = %v, want 1", d.frac)
	}
}

func TestPhaseIgnoredForBatches(t *testing.T) {
	d := testDash(4)
	d.Phase("a", "encode", 0.7)
	if d.frac != 0 {
		t.Fatalf("batch fraction moved to %v, want 0", d.frac)
	}
}

func TestEncodeFractionUsesRange(t *testing.T) {
	d := testDash(1)
	d.PhaseRange("encode", 0.5, 0.9)
	d.EncodeFraction(0.5)
	if d.frac < 0.69 || d.frac > 0.71 {
		t.Fatalf("fraction = %v, want about 0.7", d.frac)
	}
	d.EncodeFraction(0.1)
	if d.frac < 0.69 {
		t.Fatalf("fraction went backwards to %v", d.frac)
	}
}

func TestNoColorUsesBlockGlyphs(t *testing.T) {
	d := testDash(1)
	d.color = false
	d.frac = 1
	got := wallText(d)
	if strings.Contains(got, "\x1b[") {
		t.Fatal("NO_COLOR frame contains ANSI colour codes")
	}
	if !strings.Contains(got, "██") {
		t.Fatal("NO_COLOR frame missing filled block glyphs")
	}
}
