# ADR 0002 — Drop ImageMagick, use native codecs

**Status:** accepted

## Context

The shell script used ImageMagick for input decoding, frame coalescing,
flattening over gray, and APNG assembly. The rewrite is required to bundle
libwebp/libavif/libjxl and exclude ImageMagick entirely.

## Decision

- FFmpeg (and `heif-convert` for HEIC/HEIF stills) decodes inputs.
- `cwebp`/`img2webp`, `avifenc`, `cjxl` encode; `dwebp`/`anim_dump`,
  `avifdec`, `djxl` decode candidates for the VMAF gate.
- Alpha is flattened over `gray50` (0x7f7f7f) in RGB before libvmaf.
- Animated JPEG XL is assembled as APNG by the Go code, then encoded by `cjxl`.
- `--img-encoder magick` fails with a clear error.

## Consequences

- No ImageMagick dependency or policy/security surface.
- WebP frames must be decoded with libwebp (`dwebp`/`anim_dump`), not FFmpeg:
  FFmpeg's VP8 decoder produces different RGB and would move the gate.
- Animated HEIF is not supported.
- The help text differs from the shell script where it described ImageMagick.
