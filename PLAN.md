# PLAN — `mins` (Go rewrite)

A Go rewrite of `~/shell-scripts/mins/mins.sh` v1.17.0. One self-contained
executable per OS, with the media toolchain embedded.

## Scope

**In scope**

- Video → AV1 CRF search (`libsvtav1`, `libaom-av1`, `av1_nvenc`), trim windows,
  audio track selection / `--audio-search` / ViSQOL and ffmpeg audio metrics,
  subtitle policy, `--refine`, `--fbf`, `--instances` batch parallelism.
- Still and animated images → WebP / AVIF / JPEG XL through `libwebp`,
  `libavif`, and `libjxl` (no ImageMagick).
- `--mode animate`: video → animated WebP / AVIF / JPEG XL.
- Standalone audio → bitrate-gated encode.
- JSON reports, run logs, summary table, live TUI, `--no-tui`.
- Bundled FFmpeg 9.0.2 + codec tools + ViSQOL, embedded compressed and
  extracted to a per-user cache on first run.
- Five targets: linux/amd64, linux/arm64, windows/amd64, darwin/amd64,
  darwin/arm64.

**Out of scope**

- Camera RAW input (no libraw).
- Animated HEIF sequences (still HEIC only).
- ImageMagick (removed entirely).
- The old `minsview` JSON event protocol.
- Public binary distribution/licensing compliance (personal use for now).

## Architecture

```
cmd/mins/main.go            wiring, signal handling, exit codes
internal/cli/               flag parsing (drop-in mins.sh CLI)
internal/config/            resolved options, validation, derived settings
internal/tools/             embedded/external tool resolution, cache, extraction
internal/ffmpeg/            ffmpeg/ffprobe runner, progress, log capture
internal/media/             probing (resolution, fps, duration, streams)
internal/escape/            ffmpeg filtergraph + concat-list escaping
internal/audio/             track selection, codec args, metrics, bitrate search
internal/audioproc/         standalone audio path
internal/subtitle/          subtitle track policy per container
internal/video/             process_file (CRF search), fbf, refine, reports
internal/image/             native still/animated image path, animate, APNG
internal/report/            JSON report structs
internal/tui/               live dashboard
internal/logx/              run log + stderr/stdout reporting
internal/util/              small helpers (time parsing, human size, etc.)
```

## Behaviour

- CLI is a drop-in for `mins.sh`; `--img-encoder magick` is rejected.
- Defaults match v1.17.0 (`--encoder svt`, `--vid-fmt mp4`, `--img-fmt webp`,
  `--target 95`, `--audio-metric visqol`, `--target-audio 4.4`, etc.).
- Output naming, report schemas, `mins-logs/` layout, summary columns, and
  exit codes match v1.17.0.
- Tool resolution order: `MINS_*` override → `MINS_TOOLS_DIR` → embedded
  cache → `PATH` (development fallback).
- Embedded tools are gzip-compressed, extracted atomically to
  `<UserCacheDir>/mins/<version>-<hash>/`, SHA-256 verified, and chmod +x.
- Goroutines replace `xargs -P`; no recursive `mins` processes.
- TUI is built in; `--no-tui` keeps plain progress lines.

## Toolchain

Pinned in `tools/versions.env`:

| tool | version |
|---|---|
| FFmpeg | 9.0.2 |
| libwebp | 1.6.0 |
| libavif | 1.4.2 |
| libjxl | 0.12.0 |
| libheif | 1.23.7 |
| ViSQOL | 3.3.3 |

CI (`.github/workflows/release.yml`) builds the tools on native runners,
then cross-compiles the Go binary with the per-target asset directory.

## Checklist

- [x] project scaffold, docs, ADRs
- [x] CLI + config validation
- [x] tool resolver + cache extraction + SHA-256 manifests
- [x] ffmpeg runner + probing + escaping
- [x] video CRF search path
- [x] image still/animated path
- [x] animate path
- [x] standalone audio path
- [x] fbf path
- [x] refine path
- [x] batch parallelism
- [x] TUI
- [x] reports + summary + logging
- [x] unit tests
- [x] integration fixtures + optional parity/real-clip hooks
- [x] toolchain build scripts + CI
- [x] release archives
- [ ] first green CI run (native toolchain builds need runner validation)

## Verification

1. `make test` — offline unit tests.
2. `make build` — local binary using `PATH` tools or `MINS_*` overrides.
3. `make fixtures` — generated media under `testdata/generated/`.
4. `make integration` — runs the binary against fixtures.
5. `make parity MINS_SH=...` — diffs logs/reports against v1.17.0.
6. `make release` — five archives + `sha256sums.txt` (needs embedded assets).

