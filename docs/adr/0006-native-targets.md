# ADR 0006 — Five native targets, CI-built toolchains

**Status:** accepted (partially superseded by ADR 0008)

## Context

Static media toolchains are platform-specific and painful to cross-compile.
The supported targets are linux/amd64, linux/arm64, windows/amd64,
darwin/amd64 and darwin/arm64.

## Decision

Build the Go binary and the toolchain on a native runner for each target
(`ubuntu-24.04`, `ubuntu-24.04-arm`, `windows-2022`, `macos-15-intel`,
`macos-15`). `macos-14` was dropped when GitHub stopped supporting the
image (`The configuration 'macos-14-arm64-us-default' is not supported`).
Pinned versions live in `tools/versions.env`. Each job packages
`mins_2.0.0_<os>_<arch>.tar.gz` (`.zip` on Windows) with `LICENSE`,
`THIRD_PARTY_NOTICES.md`, and a `sha256sums.txt`.

ViSQOL on Windows is attempted but not required: `VISQOL_BIN` can override it,
and a missing binary is a clear runtime error rather than a silent metric
switch.

**Superseded by ADR 0008:** ViSQOL is now required on every target, Windows
included. A Windows build without a portable ViSQOL blocks the release.

## Consequences

- No cross-compilation of native code; each runner builds what it runs.
- CI time and runner cost are higher, and Windows is the weakest link.
- macOS binaries are unsigned (personal use).
