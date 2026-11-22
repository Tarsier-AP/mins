# ADR 0010 — Full-screen live dashboard

**Status:** accepted

## Context

ADR 0005 replaced the external `minsview` with a six-line inline ANSI block.
That block only shows aggregate counts and three truncated messages, it is
enabled only for `--fbf` and explicit parallel batches, FBF's total is set
after the frame work finishes, and FFmpeg's `\r` bar still paints over it
during FBF assembly. There are no terminal-restore or rendering tests.

## Decision

Replace the inline block with an adaptive full-screen dashboard, still
hand-rolled on the standard library (no TUI framework, no cgo).

- **Enablement.** Every TTY run (`batch`, single-file video/image/audio,
  animate, `--fbf`), not just parallel/FBF. `--no-tui`, non-TTY stderr and
  non-TTY stdout fall back to the old inline block or plain output unchanged.
- **Screen.** Alternate screen plus hidden cursor when stdout and stderr are
  TTYs and `TERM` is not `dumb`; the last frame is not left behind. Exit
  restores the screen and cursor on normal exit, `q`, `Ctrl-C`, `SIGTERM`,
  panic and fatal error, then returns the existing 0/1/2/130 code.
- **Layout.** Header (mode, workers, counts, percent, ETA, failures, live
  bytes), a worker table using the summary's Q/VMAF/SIZE/STATUS semantics,
  a wrapped, auto-scrolled `[mins]` log pane below it, and a right-docked
  32-square progress wall (8×4, two-column blocks, one-column gutters).
  The wall is hidden below 100 columns or 16 rows.
- **Wall semantics.** Always exactly 32 squares, progress only. A random,
  seeded permutation fixes the fill order; each 1/32 of progress completes
  the next square, with a dark → mid → bright green ramp. Failures are not
  painted on the wall; they stay in the header count and the table.
- **Progress.** Batches count completed files; `--fbf` counts frames;
  single-file runs use phase weights (probe, search, final encode) with
  FFmpeg's `out_time/duration` driving the encode phase. ETA is an EWMA over
  progress with `--:--` until it has a sample.
- **Events.** The dashboard consumes structured events (file start, attempt
  with quality/VMAF/bytes/verdict, phase, totals, log line) instead of the
  old three-string ring. `Env.TUIJob` scopes events to a row per worker.
- **Logs.** In full-screen mode `logx` routes `[mins]` and `mins: error:`
  lines into the log pane and leaves stdout alone when redirected. The run
  log and `mins-logs/` files are unchanged.
- **Keys.** `q` only, and only when stdin is a terminal; stdin switches to
  single-key mode (echo and line buffering off, Ctrl-C untouched) while the
  dashboard runs. `q` takes the existing interrupt path (cleanup, terminal
  restore, exit 130). No scrolling, toggles, mouse or themes.
- **Degradation.** `NO_COLOR` draws the wall with `░░ ▒▒ ██` glyphs instead
  of colour; `TERM=dumb` keeps the inline dashboard. `COLORTERM=truecolor`
  selects 24-bit colours, otherwise 256-colour codes are used.
- **Fixes.** FBF reports its frame total before processing starts, and
  FFmpeg's own bar is suppressed whenever the dashboard is active.

## Consequences

- The dashboard is still best-effort: it never changes the summary, reports,
  `mins-logs/` names, log-line formats or exit codes.
- `--no-tui` and non-TTY behaviour remain byte-identical; integration and
  parity tests keep using `--no-tui`.
- Rendering is tested with an injected writer, size and clock (golden-style
  layout assertions, wall ramp, resize, small terminal, `NO_COLOR`, restore).
- The dashboard no longer shows a per-file wall in large batches: the wall is
  always aggregate, while the worker table and log pane carry per-file detail.
