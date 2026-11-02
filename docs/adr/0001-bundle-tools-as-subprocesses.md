# ADR 0001 — Bundle tools and run them as subprocesses

**Status:** accepted

## Context

`mins.sh` depends on a system FFmpeg with libvmaf/libsvtav1/libaom/libjxl/
libwebp, on ImageMagick for image work, on Python for parsing and APNG
assembly, and on `numfmt`/`xargs`. The rewrite must ship as one executable per
OS and must not depend on whatever FFmpeg happens to be installed.

## Decision

Go orchestrates statically linked tool binaries as subprocesses. The tools are
gzip-compressed inside the Go executable, extracted to a per-user cache on
first run, SHA-256 verified, and executed. No cgo and no linking against
libav*/libwebp/libavif/libjxl.

## Consequences

- The Go binary stays pure Go and cross-compiles trivially.
- Tool crashes are isolated; the process model matches the shell script.
- Release builds are larger, and first run pays an extraction cost.
- Every tool must exist as a standalone executable (FFmpeg, ffprobe, cwebp,
  dwebp, img2webp, anim_dump, avifenc, avifdec, cjxl, djxl, heif-convert,
  heif-info, visqol).
