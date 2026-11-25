# ADR 0011 — Opt-in event stream and option surface for external frontends

**Status:** accepted

## Context

ADR 0005 removed the `minsview` JSON event protocol when the dashboard moved
in-process. That was right for the CLI: nothing else consumed the stream, and
an unused protocol is a compatibility burden.

A desktop frontend is now part of the distribution. It must show per-file state
and progress, and it must not re-implement encoding decisions, quality search,
or gate logic. Polling log text would make the GUI depend on wording that
parity deliberately keeps stable but that was never designed as an API.
Re-implementing the engine in the frontend would fork behaviour, which
ADR 0004 rules out.

## Decision

Add one opt-in flag, `--events FILE`, that writes newline-delimited JSON to
`FILE` while the run proceeds. An existing file is truncated.

- **Off by default.** Without the flag no file is opened, no event is formatted,
  and stdout, stderr, log wording, reports and exit codes are unchanged.
- **Derived, not authoritative.** Events carry facts the CLI already computed
  (`index`, `input`, `output`, `mode`, `q`, `vmaf`, `size`, `status`, counts).
  A consumer that ignores the stream sees exactly the old run.
- **One writer, ordered lines.** A single mutex guards writes, so lines are
  never interleaved even with `--instances` workers. Lines are written as they
  happen; the OS makes them visible to a tailer without an explicit flush.
- **`log` events mirror `[mins]` lines.** They are a tee of the logger, not the
  TUI sink, so a terminal that owns the screen still receives its normal
  output. The log file and the event stream are independent.
- **Additive, versioned.** Every line has `v` (schema version, currently 1),
  `t` (event type) and `ts` (RFC 3339 UTC). Unknown types and unknown fields
  must be ignored by consumers. Removing or retyping a field requires a version
  bump and an ADR; adding one does not.
- **No back pressure, no failure coupling.** An unwritable `--events` path is a
  fatal usage error (exit 2) reported before any media work. Write errors after
  that are ignored: the stream is observability, and a full disk must not turn
  a successful encode into a failed one.

Event types in version 1: `run_start`, `file_start`, `file_done`, `progress`,
`log`, `run_end`.

## Option surface: `--describe`

A second flag, `--describe`, prints the option surface as JSON to stdout and
exits 0 without needing an input file, a log, or a tool.

```json
{"version":"2.2.0","flags":[{"name":"target","kind":"number","default":"95","min":0,"max":100}],"exit_codes":{"ok":0,"failures":1,"usage":2,"interrupted":130}}
```

- **Facts only.** `kind` (`bool`, `number`, `enum`, `text`, `path`), `default`,
  `values` for enums, and `min`/`max` only where `config.Normalize` really
  enforces a bound. Prose lives in the frontend, so the CLI's own wording is
  never reused as UI copy.
- **No duplicated ranges.** A frontend that renders a slider from `min`/`max`
  cannot offer a value the engine rejects.
- **Drift is a test failure.** `cli` has tests asserting every described name
  and alias is consumed by `Parse`, every `--flag` in `--help` is described,
  and every described flag has GUI copy on the Rust side. Adding a flag without
  describing it fails `make test`; describing it without GUI copy fails
  `make gui-check`.
- **Frontend-only flags are excluded** (`--help`, `--version`, `--describe`,
  `--events`, `--no-tui`): they are set by the frontend or are process-level,
  not part of a media run.

## Consequences

- ADR 0005's "drop the protocol entirely" is narrowed to "the CLI does not
  require a protocol". The in-process TUI remains the default and is untouched.
- `mins-gui` becomes a thin client: it builds a normal `mins` command line,
  reads the stream, and renders. It cannot diverge from the engine because it
  never makes an encoding decision.
- The event schema is a public surface. It is documented here, has a version
  field, and gets a test that fails on malformed or torn lines.
- Third-party frontends may use the same flag. They get the same guarantees and
  the same "unknown fields are ignored" contract.
- `--describe` makes the GUI's option form a projection of the engine instead
  of a second list to maintain. The cost is a test in each language; the saving
  is that ranges and enums can only be wrong in one place.
