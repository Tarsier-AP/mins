# ADR 0003 — Embedded assets, cache extraction, SHA-256 manifests

**Status:** accepted

## Context

A single executable per OS cannot execute tools from inside itself, and
re-extracting the whole toolchain on every run is wasteful.

## Decision

Assets live under `internal/tools/assets/<os>_<arch>/`, gzip-compressed, and
are selected at build time by a `//go:build` file. On first use they are
extracted atomically (`.part` + rename) to
`os.UserCacheDir()/mins/2.0.0-<buildhash>/`. Each platform directory carries a
`sha256sums.txt`; cached files are verified on every start and re-extracted
when missing or corrupt. `MINS_TOOLS_DIR` overrides the cache location and
per-tool `MINS_<NAME>` variables override individual paths.

## Consequences

- Offline, reproducible runtime with no system packages.
- A corrupted cache self-heals.
- Release builds must generate `sha256sums.txt` alongside the assets.
- Development builds (no assets) fall back to `PATH`.
