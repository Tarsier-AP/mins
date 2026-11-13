# AGENTS.md

Go rewrite of `mins.sh` v1.17.0: one self-contained media minimizer with the
FFmpeg/codec toolchain embedded. Module `mins`, Go 1.24+, standard library only
(no cgo, no third-party Go deps).

## Before changing behavior

- **Drop-in parity is the core constraint.** CLI flags, output names, JSON
  report schemas/float shape, `[mins]` log lines, `mins-logs/` filenames,
  summary widths, and exit codes must match v1.17.0. Quirks (trailing `_` in log
  tags, `numfmt` ceiling rounding/locale decimal separator, `.min` collision
  suffix) are deliberate.
- Read `CONTEXT.md` (glossary + invariants) and the relevant `docs/adr/` before
  touching output, reports, or format decisions. `CONTEXT.md` is authoritative
  for domain vocabulary.
- `internal/report.F` reproduces Python `json.dump` floats (`95.0`, no trailing
  newline, `crf`/`cq` present even when 0). Don't swap it for `encoding/json`
  floats.
- ImageMagick is intentionally absent; `--img-encoder magick` must error.
- WebP candidates must be decoded with bundled libwebp tools (`dwebp`,
  `anim_dump`), not FFmpeg — FFmpeg's VP8 RGB differs and would move the gate.
  Alpha is flattened over `gray50`; VMAF compares 8-bit `yuv420p`.

## Commands

```sh
make check                 # go vet ./... && go test ./... (default verification)
make test                  # offline unit tests, no media tools needed
go test ./internal/image -run TestSearch -v   # single package / test
make build                 # dev binary ./mins, NO embedded assets
make fixtures              # synthetic media in testdata/generated/
make integration           # builds, then go test -tags=integration ...
make parity MINS_SH=/path/to/mins.sh          # v1.17.0 parity diff
```

- Integration tests are behind `//go:build integration`; plain `go test ./...`
  silently skips them and `go test ./internal/integration` without the tag
  fails. Always use `make integration` or `-tags=integration`.
- Integration prerequisites: `ffmpeg` on `PATH` (tests skip otherwise).
  Fixtures are created on demand; `make fixtures` creates missing ones
  (`make clean` removes `testdata/generated/`).
- `make parity` needs `MINS_SH`; `MINS_REAL_CLIP` enables real-clip parity.
  Video parity tests are skipped in `-short` mode.
- No lint/format config: use `gofmt` and `make vet`.

## Build and toolchain

- `make build` produces a development binary that resolves tools via
  `MINS_<NAME>` override → `MINS_TOOLS_DIR` → embedded cache → `PATH`. A fresh
  clone has no embedded assets (they are gitignored), so dev runs need FFmpeg
  and codec tools on `PATH` or `MINS_*` overrides.
- `make toolchain` compiles FFmpeg/libaom/libjxl/libvmaf/ViSQOL from pinned
  sources — slow and dependency-heavy. Only needed for `make bundle`/release;
  don't run it unless the task requires embedded assets.
- `make bundle` packages the host toolchain into
  `internal/tools/assets/<os>_<arch>/`, then builds. `make release` requires
  assets for all five targets.
- `tools/versions.env` is the version source of truth. Its hash becomes
  `BuildHash`, which names the runtime extraction cache
  (`<user cache>/mins/2.0.0-<hash>/`); bumping a version invalidates caches.
- Only five targets compile: linux/amd64, linux/arm64, windows/amd64,
  darwin/amd64, darwin/arm64. `internal/tools/platform_<os>_<arch>.go` selects
  the embedded FS via build tags; adding a target requires a new platform file,
  asset dir, and CI runner. Release builds cross-compile Go with
  `CGO_ENABLED=0`.
- `tools/build.sh` reruns a static-link/portability audit; it refuses
  non-system (Homebrew/MSYS2) libraries.

## Architecture

`cmd/mins` → `cli.Parse` → `config.Normalize` → `app.Run`. `app.Run` resolves
tools and builds a shared `core.Env`, then dispatches whole files to
`internal/video`, `internal/image` (via `internal/native` backends), and
`internal/audioproc`. All media work is subprocess orchestration through
`internal/ffmpeg`; probes in `internal/media`; escaping in `internal/escape`;
results/logs/TUI in `internal/result`, `internal/logx`, `internal/tui`.

- Workers are goroutines inside one process (`--instances`, `--fbf-workers`);
  there is no recursive `mins` invocation.
- TUI is best-effort: enabled only when stderr is a TTY and the run is parallel
  or FBF, and must never affect reports, summary, or exit code. `--no-tui` is
  plain output (integration tests always use it).
- Exit codes: 0 success, 1 one or more gate/processing failures, 2 usage/fatal,
  130 interrupted.

## Release

Pushing a `v*` tag triggers `.github/workflows/release.yml`, which builds the
native toolchain per target, embeds assets, smoke-tests the self-contained
binary, and publishes archives with `sha256sums.txt`. There is no separate
local release path beyond `make release` with assets present.
