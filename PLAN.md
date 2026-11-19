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
- License-compliant release bundles: MIT for `mins`, LGPL-only FFmpeg,
  `LICENSES/` in every archive, a shared sources archive, per-target relink
  materials, and automated checks that fail the release (ADR 0009).

**Out of scope**

- Camera RAW input (no libraw).
- Animated HEIF sequences (still HEIC only).
- ImageMagick (removed entirely).
- The old `minsview` JSON event protocol.

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
| libvmaf | 3.0.0 (linked into FFmpeg) |
| SVT-AV1 | 3.1.0 |
| libaom | 3.13.1 |
| libopus | 1.6.1 |
| libwebp | 1.6.0 |
| giflib | 5.2.2 (for `anim_dump`) |
| libavif | 1.4.2 |
| libjxl | 0.12.0 |
| libheif | 1.23.7 |
| libde265 | 1.0.16 |
| ViSQOL | 3.3.3 (Bazel 6.5.0, patched) |

CI (`.github/workflows/release.yml`) builds the tools on native runners,
then cross-compiles the Go binary with the per-target asset directory.
`tools/build.sh` refuses to package any executable that still links a
non-system shared library. ViSQOL and its SVR model are required on every
target (ADR 0008): a missing or non-portable Bazel build fails the release
instead of producing a bundle whose default audio metric cannot run.

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
- [x] static-link / portability audit in the toolchain build
- [x] local linux/amd64 bundled-toolchain build and embedded smoke test
- [x] strict release bundles: packaging fails on missing required tools,
      ViSQOL and its model are required on every target, non-portable tools
      fail the build, and the bundle lists are pinned together by a unit test
- [x] licensing compliance: LGPL-only FFmpeg, `LICENSES/` in every archive,
      generated notices from a single inventory, sources and relink archives,
      and a blocking license gate in CI (ADR 0009)
- [ ] first green CI run for all five targets (native toolchain builds need
      runner validation; ViSQOL/Bazel and macOS/Windows link closure are the
      remaining unknowns; ADR 0008)

## Verification

1. `make test` — offline unit tests.
2. `make build` — local binary using `PATH` tools or `MINS_*` overrides.
3. `make fixtures` — generated media under `testdata/generated/`.
4. `make integration` — runs the binary against fixtures.
5. `make parity MINS_SH=...` — diffs logs/reports against v1.17.0. Both
   sides must measure with the same toolchain: put the built prefix first
   on `PATH` (`PATH=$PWD/tools/.build/prefix/bin:$PATH`) or the Go binary
   compares its embedded FFmpeg/libvmaf against the system one, and the
   small VMAF deltas flip gate decisions at the boundary.
6. `make licenses` — regenerate `THIRD_PARTY_NOTICES.md` from the inventory
   (`make check` and CI fail if it is stale).
7. `make sources` / `make relink TOOLCHAIN_TARGET=<os>_<arch>` — build the
   license compliance archives (need fetched sources / a built toolchain).
8. `make release` — five binaries with `LICENSES/` and `sha256sums.txt`
   (needs embedded assets). The sources and relink archives are built per
   target by `tools/collect-sources.sh` in CI.

