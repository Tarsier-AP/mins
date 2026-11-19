# ADR 0009 — Release bundles are license-compliant and self-contained

**Status:** accepted

## Context

Release archives embed compressed copies of FFmpeg, libheif/libde265, libjxl,
libwebp, libavif, ViSQOL, and their statically linked dependencies. `mins`
itself is MIT-licensed, but two facts made the shipped bundles hard to
redistribute:

1. `tools/build.sh` configured FFmpeg with `--enable-gpl`. That flag does more
   than include a few optional filters: it changes FFmpeg's reported license
   to GPL-2.0-or-later. The GPL-only code it enabled (`eq`, `hqdn3d`,
   `delogo`, `blackframe`, `cropdetect`, `nnedi`, `signature`, and similar)
   is never invoked by `mins`, which uses `pad`, `showinfo`, `fps`, `format`,
   `crop`, `volumedetect`, and `vmaf`.
2. libheif and libde265 are LGPL-3.0-or-later and are linked statically.
   Sharing the LGPL obligation with the binary requires the corresponding
   source and the materials to relink the executables against a modified
   library. FFmpeg is LGPL-2.1-or-later as a whole, so its complete source and
   reproducible build must also accompany the bundle.

The previous `THIRD_PARTY_NOTICES.md` was a hand-written table with coarse
license names, no texts, and no connection to what the build actually did. A
drifted notice would not fail anything.

`mins` executes every bundled tool as a separate process and communicates
through pipes and files, so the tools are not linked into the MIT Go binary.
That does not remove the LGPL obligations for the statically linked libraries
inside those executables.

## Decision

- **FFmpeg is built without `--enable-gpl`, `--enable-nonfree`, and
  `--enable-version3`.** The build asserts the resulting configuration and
  license banner; a GPL banner fails the build. `mins` loses no functionality
  because it uses none of the GPL-only components.
- **Static linking stays.** The self-contained bundle is the product. LGPL
  compliance is met with relink materials rather than by switching libraries
  to shared objects.
- **`tools/licenses/inventory.tsv` is the single source of truth** for
  shipped components: versions, SPDX expressions, which executables contain
  them, upstream sources, copyright holders, and license files.
  `tools/gen-notices.sh` renders `THIRD_PARTY_NOTICES.md` from it and
  `--check` fails when the rendered file is stale.
- **Every release archive ships `LICENSES/`** with the full text of every
  referenced license, plus `LICENSE` and `THIRD_PARTY_NOTICES.md`.
- **One release-wide `mins_<version>_sources.tar.xz`** contains the Go source,
  the build scripts and patches, the pinned upstream tarballs, the libjxl
  checkout with its vendored dependencies, `LICENSES/`, and `SOURCES.md` with
  rebuild instructions.
- **One `mins_<version>_relink_<os>_<arch>.tar.xz` per target** contains
  libheif, libde265, the other static archives used by the original link, the
  `heif-dec`/`heif-info` object files, and `RELINK.md` plus executable relink
  scripts. This is the LGPL-3.0 corresponding application code and relink
  path.
- **Compliance is automated.** `tools/check-licenses.sh` runs offline in
  `make check` and in CI before the toolchain build, and with `--built` after
  it. The release workflow publishes the sources job and refuses to publish
  if either gate fails.

## Consequences

- `tools/bundle-tools.txt` is the manifest shared by packaging, the build
  audit, and the license checker, so a newly shipped executable cannot be
  added without recording its components.
- The sources archive is large (the libjxl test data dominates) but it is the
  complete corresponding source for a fixed set of pinned versions.
- Relink archives are target-specific because static archives are; the
  sources archive is not.
- ViSQOL's Bazel workspace downloads its own pinned dependencies on first
  build. Those are unmodified upstream sources; `SOURCES.md` records this.
- The inventory must be updated with `tools/versions.env`. The checker fails
  if a pinned runtime version is unreferenced or an inventory version variable
  is undefined.
- This ADR records engineering decisions, not legal advice. The notices and
  archives are factual materials for recipients and their counsel.
