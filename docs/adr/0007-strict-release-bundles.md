# ADR 0007 — Strict release bundles

**Status:** accepted

## Context

A release binary promises self-containment: the user installs nothing. The
bundle list has three independent copies — the resolver's `ToolNames`, the
packaging script's required/optional lists, and the toolchain build's audit
list — so they can drift. Drift in one direction ships a "self-contained"
binary that quietly falls back to `PATH` tools (which the user was told they
never need) or fails at runtime with a missing tool; drift in the other
direction packages a tool nothing calls. ViSQOL is additionally built
outside the toolchain build on CI, so its portability audit would never run,
and the binary could ship without its SVR model or with a non-startable
Windows build.

## Decision

Release bundles are strict:

- Packaging fails the build when any required tool is missing for the
  target. Only ViSQOL is optional.
- ViSQOL ships together with its SVR model or not at all.
- ViSQOL built outside the toolchain build is audited and dropped from the
  prefix when it is not portable, exactly like build.sh audits the required
  tools.
- A unit test pins the three bundle-list copies together, so adding,
  removing, or renaming a tool must update all of them at once.

## Consequences

- A release either contains a complete toolchain or does not get built;
  there is no partially self-contained artifact.
- Adding a tool means touching `ToolNames`, `package-assets.sh`,
  `build.sh`, and the build recipes — the consistency test makes the
  forgotten copies fail loudly in CI.
- Dropped ViSQOL falls back to the existing clear runtime error pointing at
  `VISQOL_BIN` or `--audio-metric asisdr`; the metric never changes silently.
