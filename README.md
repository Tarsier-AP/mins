# mins (Go rewrite)

`mins` is a self-contained media minimizer: it re-encodes video to AV1, images
to WebP/AVIF/JPEG XL, and audio to a bitrate-gated lossy codec, keeping only
the smallest result that still clears a quality gate.

This repository is a Go rewrite of `mins.sh` v1.17.0. It keeps the shell
script's command line, defaults, output names, JSON reports, run log layout,
summary table and exit codes, but ships as one executable per OS with the
whole media toolchain embedded. No system FFmpeg, ImageMagick, Python, or
codec package is required at runtime.

## Features

- **Video → AV1** with `libsvtav1`, `libaom-av1` or `av1_nvenc`, a binary CRF/CQ
  search, trim windows, audio track selection, `--audio-search`, subtitle
  policy, `--refine`, and frame-by-frame `--fbf`.
- **Images → WebP / AVIF / JPEG XL** through `libwebp`, `libavif` and `libjxl`
  (still and animated), gated by VMAF against the source.
- **Video → animated image** with `--mode animate`.
- **Standalone audio** with a bitrate search gated by ViSQOL or FFmpeg's
  `asisdr` / `asdr` / `apsnr`.
- **Batch parallelism** with goroutines (`--instances`), a built-in live TUI,
  and `--no-tui` for plain output.
- **JSON reports** (`.vmaf.json`, `.fbf.vmaf.json`, `.audio.json`), a run log,
  and a summary table identical to v1.17.0.

## Build

Requires Go 1.24+. The Go code uses the standard library only.

```sh
make build            # host binary at ./mins (uses PATH tools if no assets)
make test             # offline unit tests
make fixtures         # synthetic media under testdata/generated/
make integration      # end-to-end tests against the built binary
```

A development build has no embedded assets and resolves tools from `PATH`.
To point it at a locally built toolchain:

```sh
export MINS_TOOLS_DIR=/opt/mins-tools
# or per tool:
export MINS_FFMPEG=/usr/local/bin/ffmpeg
export MINS_FFPROBE=/usr/local/bin/ffprobe
export MINS_VISQOL=/usr/local/bin/visqol
export MINS_VISQOL_MODEL=/usr/local/share/visqol/libsvm_nu_svr_model.txt
```

## Usage

The CLI is a drop-in for `mins.sh` v1.17.0:

```sh
mins clip.mp4                      # -> clip.av1.mp4 + clip.vmaf.json
mins photo.jpg                     # -> photo.webp + photo.vmaf.json
mins --img-fmt avif photo.jpg      # -> photo.avif
mins --mode animate clip.mp4       # -> clip.webp (animated)
mins track.flac                    # -> track.opus + track.audio.json
mins --target 97 --encoder aom *.mkv
```

Run `mins --help` for every flag. `--img-encoder magick` is rejected on
purpose: ImageMagick is not bundled or used.

Exit codes match the shell script: `0` success, `1` one or more files failed
their gate or processing, `2` usage/fatal error, `130` interrupted.

## Bundled toolchain

Release binaries embed compressed copies of:

| tool | used for |
|---|---|
| FFmpeg + ffprobe | decode/encode, libvmaf, audio metrics, muxing |
| libwebp (`cwebp`, `dwebp`, `img2webp`, `anim_dump`) | WebP |
| libavif (`avifenc`, `avifdec`) | AVIF |
| libjxl (`cjxl`, `djxl`) | JPEG XL |
| libheif (`heif-convert`, `heif-info`) | HEIC/HEIF still inputs |
| ViSQOL | audio MOS-LQO gate |

On first run they are extracted to
`<user cache dir>/mins/2.0.0-<buildhash>/`, verified against an embedded
SHA-256 manifest, and reused afterwards. Set `MINS_TOOLS_DIR` to choose a
different location. A corrupt or half-written cache entry is re-extracted
automatically.

## Limits

- Camera RAW input is not supported (no libraw).
- HEIC/HEIF support is still-image only; animated HEIF is not decoded.
- Windows ViSQOL is built on a best-effort basis. If it is not available, set
  `VISQOL_BIN` (and `VISQOL_MODEL`), or use `--audio-metric asisdr`; mins fails
  with a clear error instead of silently changing the metric.

## Release

`.github/workflows/release.yml` builds the toolchain on native runners for
`linux/amd64`, `linux/arm64`, `windows/amd64`, `darwin/amd64` and
`darwin/arm64`, then packages:

```
mins_2.0.0_<os>_<arch>.tar.gz   (or .zip on Windows)
  mins[.exe]
  LICENSE
  THIRD_PARTY_NOTICES.md
sha256sums.txt
```

Pinned tool versions live in `tools/versions.env`. macOS binaries are
unsigned; this is a personal-use project, so there is no notarization.

## Layout

```
cmd/mins/            entry point
internal/cli/        drop-in flag parser
internal/config/     validation and resolved settings
internal/tools/      embedded assets, cache, SHA-256 manifests
internal/ffmpeg/     ffmpeg/ffprobe runner
internal/media/      probing helpers
internal/video/      CRF search, refine, fbf
internal/image/      still/animated image and animate paths
internal/audio/      audio engine and metrics
internal/audioproc/  standalone audio path
internal/report/     JSON schemas
internal/tui/        built-in dashboard
docs/adr/            architecture decisions
tools/               toolchain build/package scripts
```
