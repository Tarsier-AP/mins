# ADR 0005 — Built-in TUI replaces minsview

**Status:** accepted

## Context

The shell script launched an external `minsview` binary and streamed a JSON
event protocol to it for `--fbf` and parallel batches.

## Decision

Draw the dashboard in-process with goroutines and ANSI output. Drop the JSON
event protocol entirely. `--no-tui` keeps plain output; the TUI is only enabled
when stderr is a terminal and the run is parallel or FBF.

## Consequences

- One less binary and no protocol to keep compatible.
- The dashboard is best-effort: it never affects the summary, reports or exit
  code.
- FFmpeg progress bars are suppressed while the dashboard owns the terminal.
