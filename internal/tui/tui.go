// Package tui implements the built-in live dashboard that replaces the
// external minsview renderer from mins.sh 1.17.0.
//
// The dashboard is best-effort: it never changes the summary, reports or exit
// code. On a capable terminal it owns the alternate screen and draws a worker
// table, a live log pane and a 32-square progress wall. Everywhere else it
// falls back to the original inline block, and with --no-tui it draws nothing.
package tui

import (
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"mins/internal/util"
)

const (
	tick = 400 * time.Millisecond

	wallCols  = 8
	wallRows  = 4
	wallCells = wallCols * wallRows
	wallInner = wallCols*2 + (wallCols - 1)
	wallWidth = wallInner + 2

	logCap = 2000
)

type rowState struct {
	job    string
	mode   string
	q      string
	vmaf   string
	size   string
	status string
	note   string
	active bool
	out    int64
}

// Dashboard draws a live progress view on stderr.
type Dashboard struct {
	mu     sync.Mutex
	drawMu sync.Mutex

	enabled   bool
	full      bool
	color     bool
	truecolor bool
	mode      string
	file      string

	total   int
	workers int
	done    int
	errors  int

	frac     float64
	phase    string
	phaseLo  float64
	phaseHi  float64
	started  time.Time
	rate     float64
	lastFrac float64
	lastETA  time.Time

	rows  []*rowState
	index map[string]*rowState
	logs  []string

	inBytes int64

	perm []int
	seed uint64

	out    io.Writer
	keys   io.Reader
	sizeFn func() (int, int)
	now    func() time.Time

	stop   chan struct{}
	wake   chan struct{}
	doneCh chan struct{}

	startedFlag bool
	closed      bool
	rawRestore  func()
	drawn       int
	lastW       int
	lastH       int
}

var (
	quitMu sync.Mutex
	quitFn func()
)

// SetQuit installs the callback used when the user presses q. main points it
// at the interrupt path so a keypress exits with the same cleanup as Ctrl-C.
func SetQuit(fn func()) {
	quitMu.Lock()
	quitFn = fn
	quitMu.Unlock()
}

func requestQuit() {
	quitMu.Lock()
	fn := quitFn
	quitMu.Unlock()
	if fn != nil {
		fn()
	}
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
// stderr terminal check.
func New(enabled bool, mode, file string, total, workers int) *Dashboard {
	d := &Dashboard{
		enabled: enabled,
		mode:    mode,
		file:    file,
		total:   total,
		workers: workers,
		out:     os.Stderr,
		keys:    os.Stdin,
		sizeFn:  defaultSize,
		now:     time.Now,
		seed:    uint64(time.Now().UnixNano()) ^ uint64(os.Getpid())<<17,
		index:   map[string]*rowState{},
		stop:    make(chan struct{}),
		wake:    make(chan struct{}, 1),
		doneCh:  make(chan struct{}),
	}
	if d.workers < 1 {
		d.workers = 1
	}
	d.full = enabled && IsTerminal(os.Stderr) && IsTerminal(os.Stdout) && colorsAllowed()
	d.color = d.full && os.Getenv("NO_COLOR") == ""
	d.truecolor = d.color && (strings.Contains(strings.ToLower(os.Getenv("COLORTERM")), "truecolor") ||
		strings.Contains(strings.ToLower(os.Getenv("COLORTERM")), "24bit"))
	d.perm = randomPerm(d.seed)
	return d
}

// Enabled reports whether the dashboard will draw anything.
func (d *Dashboard) Enabled() bool { return d != nil && d.enabled }

// FullScreen reports whether the dashboard owns the alternate screen.
func (d *Dashboard) FullScreen() bool { return d != nil && d.full }

// Start begins periodic redraws.
func (d *Dashboard) Start() {
	if !d.Enabled() || d.startedFlag {
		return
	}
	d.mu.Lock()
	d.startedFlag = true
	d.started = d.now()
	d.lastETA = d.started
	d.mu.Unlock()

	if d.full {
		enterScreen(d.out)
		register(d)
		d.startKeys()
	}
	go d.loop()
}

// startKeys puts stdin in single-key mode and watches for q. It is best-effort:
// terminals that reject the mode simply keep the old Ctrl-C behaviour.
func (d *Dashboard) startKeys() {
	if d.keys == nil {
		return
	}
	f, ok := d.keys.(*os.File)
	if !ok || !IsTerminal(f) {
		return
	}
	restore, ok := rawMode(f.Fd())
	if !ok {
		return
	}
	d.mu.Lock()
	d.rawRestore = restore
	d.mu.Unlock()
	go d.readKeys(d.keys)
}

func (d *Dashboard) readKeys(r io.Reader) {
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 && (buf[0] == 'q' || buf[0] == 'Q') {
			requestQuit()
			return
		}
		if err != nil {
			return
		}
	}
}

func (d *Dashboard) loop() {
	defer close(d.doneCh)
	t := time.NewTicker(tick)
	defer t.Stop()
	var resize <-chan os.Signal
	if d.full {
		resize = resizeSignal()
	}
	for {
		select {
		case <-d.stop:
			d.draw()
			return
		case <-d.wake:
			d.draw()
		case <-t.C:
			d.draw()
		case <-resize:
			d.draw()
		}
	}
}

// Close stops redraws and restores the terminal.
func (d *Dashboard) Close() {
	if !d.Enabled() {
		return
	}
	d.mu.Lock()
	started := d.startedFlag
	d.mu.Unlock()
	if started {
		select {
		case <-d.stop:
		default:
			close(d.stop)
		}
		<-d.doneCh
	}

	d.drawMu.Lock()
	defer d.drawMu.Unlock()
	d.mu.Lock()
	already := d.closed
	d.closed = true
	full := d.full
	restore := d.rawRestore
	d.rawRestore = nil
	d.mu.Unlock()
	if already {
		return
	}
	if restore != nil {
		restore()
	}
	if full {
		leaveScreen(d.out)
		unregister(d)
		return
	}
	fmt.Fprintln(d.out)
}

// RestoreAll restores every active dashboard's terminal. It is used from the
// signal and panic paths, where the normal Close may not run.
func RestoreAll() {
	activeMu.Lock()
	ds := make([]*Dashboard, 0, len(active))
	for d := range active {
		ds = append(ds, d)
	}
	activeMu.Unlock()
	for _, d := range ds {
		d.mu.Lock()
		d.closed = true
		restore := d.rawRestore
		d.rawRestore = nil
		d.mu.Unlock()
		if restore != nil {
			restore()
		}
		d.drawMu.Lock()
		leaveScreen(d.out)
		d.drawMu.Unlock()
	}
}

// SetTotal updates the denominator (batch files, FBF frames, or 1).
func (d *Dashboard) SetTotal(n int) {
	if !d.Enabled() || n <= 0 {
		return
	}
	d.mu.Lock()
	d.total = n
	d.mu.Unlock()
	d.notify()
}

// Progress sets the completed and failed counts.
func (d *Dashboard) Progress(done, errors int) {
	if !d.Enabled() {
		return
	}
	d.mu.Lock()
	d.done, d.errors = done, errors
	d.mu.Unlock()
	d.notify()
}

// StartFile registers a live row. input is the source path, used only for the
// aggregate input-byte count.
func (d *Dashboard) StartFile(job, input, mode string) {
	if !d.Enabled() || job == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if r, ok := d.index[job]; ok {
		r.active = true
		r.status = "running"
		return
	}
	r := &rowState{
		job: job, mode: mode,
		q: "-", vmaf: "-", size: "-", status: "running", active: true,
	}
	if st, err := os.Stat(input); err == nil {
		d.inBytes += st.Size()
	}
	d.rows = append(d.rows, r)
	d.index[job] = r
	d.notify()
}

// Phase sets the current phase and overall fraction for a single-file run.
// It is ignored for batches, where progress is file-count based.
func (d *Dashboard) Phase(job, name string, frac float64) {
	if !d.Enabled() {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.total > 1 {
		return
	}
	frac = clamp01(frac)
	d.phase = name
	if frac > d.frac {
		d.frac = frac
	}
	d.notify()
}

// PhaseRange marks the start of an encoder-driven phase. EncodeFraction maps
// raw ffmpeg progress into [lo, hi].
func (d *Dashboard) PhaseRange(name string, lo, hi float64) {
	if !d.Enabled() {
		return
	}
	d.mu.Lock()
	d.phase = name
	d.phaseLo, d.phaseHi = clamp01(lo), clamp01(hi)
	if d.phaseLo > d.frac {
		d.frac = d.phaseLo
	}
	d.mu.Unlock()
	d.notify()
}

// EncodeFraction receives raw ffmpeg progress (0..1) for the current file.
func (d *Dashboard) EncodeFraction(raw float64) {
	if !d.Enabled() {
		return
	}
	d.mu.Lock()
	if d.total <= 1 {
		f := d.phaseLo + (d.phaseHi-d.phaseLo)*clamp01(raw)
		if f > d.frac {
			d.frac = clamp01(f)
		}
	}
	d.mu.Unlock()
	d.notify()
}

// Attempt records one search/final candidate.
func (d *Dashboard) Attempt(job, label string, q int, vmaf float64, size int64, verdict string) {
	if !d.Enabled() {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	r := d.index[job]
	if r == nil {
		r = &rowState{job: job, q: "-", vmaf: "-", size: "-", status: "running", active: true}
		d.rows = append(d.rows, r)
		d.index[job] = r
	}
	if q >= 0 {
		r.q = strconv.Itoa(q)
	}
	r.vmaf = fmt.Sprintf("%.2f", vmaf)
	if size > 0 {
		r.size = util.HumanSize(size)
		if label == "final" {
			r.out = size
		}
	}
	r.note = fmt.Sprintf("%s %s", label, verdict)
	d.notify()
}

// Message records one line for the log pane.
func (d *Dashboard) Message(job, format string, args ...any) {
	if !d.Enabled() {
		return
	}
	d.Log(fmt.Sprintf(format, args...))
}

// Log appends a line to the log pane.
func (d *Dashboard) Log(line string) {
	if !d.Enabled() {
		return
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return
	}
	d.mu.Lock()
	for _, part := range strings.Split(line, "\n") {
		d.logs = append(d.logs, part)
	}
	if len(d.logs) > logCap {
		d.logs = append([]string(nil), d.logs[len(d.logs)-logCap:]...)
	}
	d.mu.Unlock()
	d.notify()
}

// Done records a completed unit.
func (d *Dashboard) Done(job, status string, outSize int64) {
	if !d.Enabled() {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if r := d.index[job]; r != nil {
		r.active = false
		r.status = status
		r.note = ""
		if outSize > 0 {
			r.out = outSize
		}
	}
	if d.total <= 1 {
		d.frac = 1
		d.done = 1
		d.phase = "done"
	}
	d.notify()
}

func (d *Dashboard) notify() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *Dashboard) sumOutLocked() int64 {
	var n int64
	for _, r := range d.rows {
		n += r.out
	}
	return n
}

func (d *Dashboard) fractionLocked() float64 {
	if d.total > 1 {
		f := float64(d.done) / float64(d.total)
		if d.done >= d.total {
			return 1
		}
		return clamp01(f)
	}
	if d.total == 1 {
		return clamp01(d.frac)
	}
	return 0
}

func (d *Dashboard) draw() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	w, h := 80, 24
	if d.sizeFn != nil {
		if tw, th := d.sizeFn(); tw > 0 && th > 0 {
			w, h = tw, th
		}
	}
	d.updateRateLocked()
	var out string
	if d.full {
		out = d.renderFullLocked(w, h)
	} else {
		out = d.renderInlineLocked(w)
	}
	d.mu.Unlock()

	d.drawMu.Lock()
	d.mu.Lock()
	closed := d.closed
	d.mu.Unlock()
	if !closed {
		_, _ = io.WriteString(d.out, out)
	}
	d.drawMu.Unlock()
}

func (d *Dashboard) updateRateLocked() {
	now := d.now()
	if d.lastETA.IsZero() || now.Sub(d.lastETA) < 300*time.Millisecond {
		return
	}
	dt := now.Sub(d.lastETA).Seconds()
	f := d.fractionLocked()
	if dt > 0 && f > d.lastFrac {
		inst := (f - d.lastFrac) / dt
		if d.rate == 0 {
			d.rate = inst
		} else {
			d.rate = 0.7*d.rate + 0.3*inst
		}
	}
	d.lastFrac = f
	d.lastETA = now
}

func (d *Dashboard) etaLocked() string {
	f := d.fractionLocked()
	if f <= 0.005 || f >= 0.999 || d.rate <= 0 || d.started.IsZero() {
		return "--:--"
	}
	return fmtDur((1 - f) / d.rate)
}

func (d *Dashboard) headerLocked() string {
	var b strings.Builder
	b.WriteString(" mins ")
	b.WriteString(d.mode)
	b.WriteString(" · ")
	if d.workers > 1 && d.total > 1 {
		fmt.Fprintf(&b, "%d workers · ", d.workers)
	}
	if d.total <= 0 {
		b.WriteString("scanning…")
		return b.String()
	}
	f := d.fractionLocked()
	if d.total > 1 {
		fmt.Fprintf(&b, "%d/%d · ", d.done, d.total)
	}
	fmt.Fprintf(&b, "%3.0f%% · ETA %s", f*100, d.etaLocked())
	if d.phase != "" && d.total <= 1 {
		b.WriteString(" · ")
		b.WriteString(d.phase)
	}
	if d.errors > 0 {
		fmt.Fprintf(&b, " · %d failed", d.errors)
	}
	if d.inBytes > 0 {
		out := d.sumOutLocked()
		if out > 0 {
			fmt.Fprintf(&b, " · %s→%s", util.HumanSize(d.inBytes), util.HumanSize(out))
		}
	}
	return b.String()
}

func (d *Dashboard) renderFullLocked(w, h int) string {
	lines := make([]string, 0, h)
	lines = append(lines, clip(d.headerLocked(), w))
	lines = append(lines, strings.Repeat("─", maxInt(w, 1)))

	content := h - 2
	if content < 1 {
		content = 1
	}
	wall := w >= 100 && h >= 16
	wl := w
	if wall {
		wl = w - wallWidth - 1
		if wl < 60 {
			wall = false
			wl = w
		}
	}
	left := d.leftLinesLocked(wl, content)
	if wall {
		wallLines := d.wallLinesLocked()
		for i := 0; i < content; i++ {
			var l string
			if i < len(left) {
				l = left[i]
			}
			if i < len(wallLines) {
				lines = append(lines, pad(l, wl)+" "+wallLines[i])
			} else {
				lines = append(lines, l)
			}
		}
	} else {
		lines = append(lines, left...)
	}

	var b strings.Builder
	if d.lastW != w || d.lastH != h {
		b.WriteString("\x1b[2J")
	}
	b.WriteString("\x1b[H")
	for i := 0; i < h; i++ {
		b.WriteString("\x1b[2K")
		if i < len(lines) {
			b.WriteString(lines[i])
		}
		if i < h-1 {
			b.WriteByte('\n')
		}
	}
	b.WriteString("\x1b[0m")
	d.lastW, d.lastH = w, h
	return b.String()
}

func (d *Dashboard) leftLinesLocked(width, height int) []string {
	if width < 12 {
		return nil
	}
	var lines []string
	if height >= 4 && len(d.rows) > 0 {
		lines = append(lines, d.tableLinesLocked(width)...)
		lines = append(lines, "")
	}
	remain := height - len(lines)
	if remain > 0 {
		lines = append(lines, d.logLinesLocked(width, remain)...)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

func (d *Dashboard) tableLinesLocked(width int) []string {
	if len(d.rows) == 0 {
		return nil
	}
	maxRows := d.workers
	if maxRows < 1 {
		maxRows = 1
	}
	rows := d.rows
	if len(rows) > maxRows {
		rows = rows[len(rows)-maxRows:]
	}
	wide := width >= 74
	var out []string
	if wide {
		statusW := width - (2 + 1 + 5 + 1 + 5 + 1 + 6 + 1 + 13 + 1)
		if statusW < 6 {
			wide = false
		}
	}
	if wide {
		fileW := width - (2 + 1 + 5 + 1 + 5 + 1 + 6 + 1 + 13 + 1 + 8)
		if fileW < 8 {
			fileW = 8
		}
		hdr := fmt.Sprintf("%2s %-*s %-5s %-5s %-6s %-13s %s",
			"#", fileW, "FILE", "MODE", "Q", "VMAF", "SIZE", "STATUS")
		out = append(out, clip(hdr, width))
		for _, r := range rows {
			status := d.rowStatusLocked(r)
			line := fmt.Sprintf("%2d %-*s %-5s %-5s %-6s %-13s %s",
				d.ordinalLocked(r), fileW, clip(r.job, fileW), clip(r.mode, 5),
				clip(r.q, 5), clip(r.vmaf, 6), clip(r.size, 13), status)
			out = append(out, clip(line, width))
		}
		return out
	}
	fileW := width - (2 + 1 + 5 + 1 + 6 + 1 + 6)
	if fileW < 8 {
		fileW = 8
	}
	hdr := fmt.Sprintf("%2s %-*s %-5s %-6s %s", "#", fileW, "FILE", "Q", "VMAF", "STATUS")
	out = append(out, clip(hdr, width))
	for _, r := range rows {
		line := fmt.Sprintf("%2d %-*s %-5s %-6s %s",
			d.ordinalLocked(r), fileW, clip(r.job, fileW),
			clip(r.q, 5), clip(r.vmaf, 6), d.rowStatusLocked(r))
		out = append(out, clip(line, width))
	}
	return out
}

func (d *Dashboard) ordinalLocked(r *rowState) int {
	for n, x := range d.rows {
		if x == r {
			return n + 1
		}
	}
	return 0
}

func (d *Dashboard) rowStatusLocked(r *rowState) string {
	if r.active {
		if r.note != "" {
			return r.note
		}
		if d.total <= 1 && d.phase != "" {
			return d.phase
		}
		return "running"
	}
	return r.status
}

func (d *Dashboard) logLinesLocked(width, height int) []string {
	if height <= 0 || len(d.logs) == 0 {
		return nil
	}
	var wrapped []string
	for _, line := range d.logs {
		wrapped = append(wrapped, wrap(line, width)...)
	}
	if len(wrapped) > height {
		wrapped = wrapped[len(wrapped)-height:]
	}
	return wrapped
}

func (d *Dashboard) wallLinesLocked() []string {
	f := d.fractionLocked()
	filled := int(f * wallCells)
	if filled > wallCells {
		filled = wallCells
	}
	pos := make(map[int]int, wallCells)
	for k, v := range d.perm {
		pos[v] = k
	}
	lines := []string{"┌" + strings.Repeat("─", wallInner) + "┐"}
	for r := 0; r < wallRows; r++ {
		var b strings.Builder
		b.WriteString("│")
		for c := 0; c < wallCols; c++ {
			idx := r*wallCols + c
			shade := 0
			if p := pos[idx]; p < filled {
				shade = 2
			} else if f > 0 && p == filled && f < 1 {
				shade = 1
			}
			b.WriteString(d.cell(shade))
			if c < wallCols-1 {
				b.WriteByte(' ')
			}
		}
		b.WriteString("│")
		lines = append(lines, b.String())
	}
	lines = append(lines, "└"+strings.Repeat("─", wallInner)+"┘")
	lines = append(lines, clip("██ done  ░░ pending", wallWidth))
	return lines
}

func (d *Dashboard) cell(shade int) string {
	if !d.color {
		return [...]string{"░░", "▒▒", "██"}[shade]
	}
	var code string
	if d.truecolor {
		code = [...]string{
			"\x1b[48;2;38;38;44m",
			"\x1b[48;2;31;158;63m",
			"\x1b[48;2;102;255;153m",
		}[shade]
	} else {
		code = [...]string{
			"\x1b[48;5;236m",
			"\x1b[48;5;34m",
			"\x1b[48;5;84m",
		}[shade]
	}
	return code + "  \x1b[0m"
}

func (d *Dashboard) renderInlineLocked(width int) string {
	if width < 40 {
		width = 40
	}
	f := d.fractionLocked()
	barW := 34
	n := int(float64(barW)*f + 0.5)
	if n > barW {
		n = barW
	}
	bar := strings.Repeat("#", n) + strings.Repeat("-", barW-n)
	lines := []string{
		fmt.Sprintf("mins %s: %s", d.mode, clip(d.file, 60)),
		fmt.Sprintf("  [%s] %5.1f%% %d/%d  %d error(s)  %d worker(s)",
			bar, f*100, d.done, d.total, d.errors, d.workers),
		"",
	}
	start := len(d.logs) - 3
	if start < 0 {
		start = 0
	}
	for i := 0; i < 3; i++ {
		msg := ""
		if start+i < len(d.logs) {
			msg = "  " + clip(d.logs[start+i], 78)
		}
		lines = append(lines, msg)
	}
	var b strings.Builder
	if d.drawn > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", d.drawn)
	}
	for _, line := range lines {
		b.WriteString("\x1b[2K")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	d.drawn = len(lines)
	return b.String()
}

func randomPerm(seed uint64) []int {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	return r.Perm(wallCells)
}

func defaultSize() (int, int) {
	return terminalSize(os.Stdout.Fd())
}

func colorsAllowed() bool {
	return os.Getenv("TERM") != "dumb"
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

func fmtDur(sec float64) string {
	if sec < 0 || sec > 359999 {
		return "--:--"
	}
	t := int(sec + 0.5)
	if t >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", t/3600, (t%3600)/60, t%60)
	}
	return fmt.Sprintf("%02d:%02d", t/60, t%60)
}

// clip truncates s to n runes, adding an ellipsis when it does not fit.
func clip(s string, n int) string {
	if n <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	if n < 4 {
		return string(rs[:n])
	}
	return string(rs[:n-1]) + "…"
}

func pad(s string, n int) string {
	w := len([]rune(s))
	if w >= n {
		return clip(s, n)
	}
	return s + strings.Repeat(" ", n-w)
}

func wrap(s string, width int) []string {
	if width < 4 {
		return []string{clip(s, width)}
	}
	rs := []rune(s)
	var out []string
	for len(rs) > width {
		out = append(out, string(rs[:width]))
		rs = rs[width:]
	}
	out = append(out, string(rs))
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
