#!/usr/bin/env bash
# Build the licensing source and relink archives.
#
#   bash tools/collect-sources.sh sources <version> [outdir]
#   bash tools/collect-sources.sh relink <target> <version> [outdir]
#
# `sources` is release-wide: pinned upstream sources, the mins Go source, the
# build scripts, patches, and LICENSES/. It is built once per release.
#
# `relink` is target-specific: the static archives and object files needed to
# relink heif-convert (heif-dec) and heif-info against a modified libheif or
# libde265, plus the exact commands. It must run on the target runner, after
# tools/build.sh.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
# versions.env is a simple KEY=VALUE file with ${VAR} references. Eval a
# CR-stripped copy instead of sourcing it: macOS ships bash 3.2, where
# `source <(...)` dies with "sed: stdout: Broken pipe" and leaves every
# variable unset. .gitattributes already enforces LF, but a stale
# checkout must not poison the version values either.
eval "$(sed 's/\r$//' "$root/tools/versions.env")"

SRC=${SRC:-$root/tools/.build/src}
PREFIX=${PREFIX:-$root/tools/.build/prefix}
BUILD=${BUILD:-$root/tools/.build/obj}
OS=$(uname -s)

usage() {
  echo "usage: $0 sources <version> [outdir]" >&2
  echo "       $0 relink <target> <version> [outdir]" >&2
  exit 2
}

[[ $# -ge 2 ]] || usage
mode=$1
case "$mode" in
  sources)
    version=$2
    out=${3:-$root/tools/dist}
    ;;
  relink)
    [[ $# -ge 3 ]] || usage
    target=$(printf '%s' "$2" | tr '-' '_')
    version=$3
    out=${4:-$root/tools/dist}
    ;;
  *) usage ;;
esac

mkdir -p "$out"
out=$(cd "$out" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/mins-sources.XXXXXX")
trap 'rm -rf "$work"' EXIT

hash_cmd() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$@"
  else
    shasum -a 256 "$@"
  fi
}

# tar -J is the release format; fail early with a useful message instead of
# silently producing a .tar.gz with an .tar.xz name.
if ! command -v xz >/dev/null 2>&1; then
  echo "sources: xz is required to create the .tar.xz archive" >&2
  echo "sources: install xz (xz-utils on Debian/Ubuntu, xz on macOS/Homebrew, xz on MSYS2)" >&2
  exit 1
fi

make_archive() { # stage-name out-file
  local name=$1 archive=$2
  (
    cd "$work"
    tar -cJf "$archive" "$name"
  )
  echo "sources: $(basename "$archive") ($(du -h "$archive" | awk '{print $1}'))"
}

write_checksums() { # stage
  local stage=$1
  (
    cd "$stage"
    find . -type f ! -name SHA256SUMS -print0 \
      | while IFS= read -r -d '' f; do hash_cmd "$f"; done >SHA256SUMS
  )
}

copy_licenses() { # stage
  local stage=$1
  cp "$root/LICENSE" "$stage/LICENSE"
  cp "$root/THIRD_PARTY_NOTICES.md" "$stage/THIRD_PARTY_NOTICES.md"
  cp -R "$root/LICENSES" "$stage/LICENSES"
}

copy_tools() { # stage
  local stage=$1
  mkdir -p "$stage/tools"
  cp "$root/tools/build.sh" "$root/tools/build-visqol.sh" \
    "$root/tools/fetch-sources.sh" "$root/tools/package-assets.sh" \
    "$root/tools/package-release.sh" "$root/tools/check-licenses.sh" \
    "$root/tools/gen-notices.sh" "$root/tools/audit.sh" \
    "$root/tools/collect-sources.sh" "$root/tools/versions.env" \
    "$root/tools/bundle-tools.txt" "$stage/tools/"
  cp -R "$root/tools/patches" "$root/tools/licenses" "$stage/tools/"
}

# ---------------------------------------------------------------- sources ---
if [[ $mode == sources ]]; then
  stage=$work/mins_${version}_sources
  mkdir -p "$stage"

  # The Go source lets a recipient rebuild mins itself; the asset payloads
  # are omitted because the toolchain build regenerates them.
  cp "$root/go.mod" "$root/Makefile" "$stage/"
  [[ -f $root/go.sum ]] && cp "$root/go.sum" "$stage/"
  cp "$root/README.md" "$root/PLAN.md" "$root/CONTEXT.md" "$stage/"
  cp -R "$root/cmd" "$root/internal" "$root/docs" "$stage/"
  if [[ -d $stage/internal/tools/assets ]]; then
    find "$stage/internal/tools/assets" -mindepth 2 -type f ! -name .keep -delete
  fi
  copy_tools "$stage"
  copy_licenses "$stage"

  # Pre-populate tools/.build/src exactly as fetch-sources.sh would, so the
  # archive's own tools/build.sh works without network access for the pinned
  # tarballs. libjxl is a directory because build.sh expects a checkout.
  mkdir -p "$stage/tools/.build/src"
  found=0
  for f in "$SRC"/*.tar.gz "$SRC"/*.tar.xz "$SRC"/*.tgz "$SRC"/*.zip; do
    [[ -f $f ]] || continue
    cp "$f" "$stage/tools/.build/src/"
    found=$((found + 1))
  done
  [[ $found -gt 0 ]] || {
    echo "sources: no source archives in $SRC; run tools/fetch-sources.sh first" >&2
    exit 1
  }
  if [[ ! -d $SRC/libjxl ]]; then
    echo "sources: missing $SRC/libjxl; run tools/fetch-sources.sh first" >&2
    exit 1
  fi
  mkdir -p "$stage/tools/.build/src/libjxl"
  tar -cf - -C "$SRC/libjxl" --exclude='.git' --exclude='*/.git' . \
    | tar -xf - -C "$stage/tools/.build/src/libjxl"
  find "$stage/tools/.build/src/libjxl" -name .git -exec rm -rf {} + 2>/dev/null || true

  # Verify the pinned archives that the build actually consumes.
  for f in \
    "ffmpeg-$FFMPEG_VERSION.tar.xz" "libvmaf-$LIBVMAF_VERSION.tar.gz" \
    "svtav1-$SVTAV1_VERSION.tar.gz" "libaom-$LIBAOM_VERSION.tar.gz" \
    "libwebp-$LIBWEBP_VERSION.tar.gz" "giflib-$GIFLIB_VERSION.tar.gz" \
    "libavif-$LIBAVIF_VERSION.tar.gz" "libheif-$LIBHEIF_VERSION.tar.gz" \
    "libde265-$LIBDE265_VERSION.tar.gz" "libopus-$LIBOPUS_VERSION.tar.gz" \
    "nasm-$NASM_VERSION.tar.xz" "visqol-$VISQOL_VERSION.tar.gz"; do
    [[ -f $stage/tools/.build/src/$f ]] || {
      echo "sources: missing source archive: $f" >&2
      exit 1
    }
  done

  cat >"$stage/SOURCES.md" <<EOF
# mins $version source archive

This archive contains the source code and build scripts for \`mins\` and every
third-party component embedded in the $version release. It is the source
material required by the GNU Lesser General Public License for the statically
linked LGPL components, and it is sufficient to rebuild the bundled tools.

## Rebuilding

The pinned sources are already in \`tools/.build/src\`, so no download step is
required for them. Install a C/C++ compiler, CMake, Ninja, Meson,
pkg-config, GNU make, and Bazel (for ViSQOL), then run:

\`\`\`sh
PREFIX=\$PWD/prefix bash tools/build.sh
bash tools/package-assets.sh <os>_<arch>
go build -trimpath -o mins ./cmd/mins
\`\`\`

\`tools/build.sh\` applies the patches in \`tools/patches/\` and builds FFmpeg
without \`--enable-gpl\`, \`--enable-nonfree\`, or \`--enable-version3\`.
ViSQOL's own Bazel workspace downloads its pinned dependencies (Abseil,
Protobuf, Armadillo, libsvm, PFFFT) on first build; those downloads are
unchanged upstream sources.

## Versions

| component | version |
|---|---|
| FFmpeg | $FFMPEG_VERSION |
| libvmaf | $LIBVMAF_VERSION |
| SVT-AV1 | $SVTAV1_VERSION |
| libaom | $LIBAOM_VERSION |
| libwebp | $LIBWEBP_VERSION |
| giflib | $GIFLIB_VERSION |
| libavif | $LIBAVIF_VERSION |
| libjxl | $LIBJXL_VERSION |
| libheif | $LIBHEIF_VERSION |
| libde265 | $LIBDE265_VERSION |
| libopus | $LIBOPUS_VERSION |
| ViSQOL | $VISQOL_VERSION |
| NASM (build tool) | $NASM_VERSION |
| Bazel (build tool) | $BAZEL_VERSION |

Vendored versions inside libjxl $LIBJXL_VERSION: Brotli 1.2.0, Highway 1.2.0,
skcms, libpng 1.6.47, zlib 1.3.1, and libjpeg-turbo 2.1.5.1.

## LGPL relink materials

The release also publishes one archive per target:

\`\`\`
mins_${version}_relink_<os>_<arch>.tar.xz
\`\`\`

It contains the static archives and object files needed to relink
\`heif-convert\` (the upstream \`heif-dec\` example) and \`heif-info\` against a
modified libheif or libde265, plus \`RELINK.md\` and executable link scripts.
FFmpeg does not link a separate LGPL library; its complete source is in this
archive and \`tools/build.sh\` rebuilds it.

## License

\`mins\` is MIT-licensed; see \`LICENSE\`. Third-party components remain under
their own licenses. The full texts and the component inventory are in
\`THIRD_PARTY_NOTICES.md\` and \`LICENSES/\`.
EOF

  write_checksums "$stage"
  archive=$out/mins_${version}_sources.tar.xz
  make_archive "mins_${version}_sources" "$archive"
  printf 'sources: sha256 %s\n' "$(hash_cmd "$archive" | awk '{print $1}')"
  exit 0
fi

# ----------------------------------------------------------------- relink ---
case "$target" in
  linux_amd64|linux_arm64|windows_amd64|darwin_amd64|darwin_arm64) ;;
  *)
    echo "relink: unsupported target: $target" >&2
    exit 1
    ;;
esac

stage=$work/mins_${version}_relink_${target}
mkdir -p "$stage/relink"

copy_licenses "$stage"

need() { # path label
  [[ -f $1 ]] || { echo "relink: missing $2: ${1#"$root"/}" >&2; exit 1; }
  cp "$1" "$stage/relink/"
}

need "$PREFIX/lib/libheif.a" "libheif.a"
need "$PREFIX/lib/libde265.a" "libde265.a"
need "$PREFIX/lib/libaom.a" "libaom.a"
need "$PREFIX/lib/libjpeg.a" "libjpeg.a"
need "$PREFIX/lib/libpng16.a" "libpng16.a"
need "$PREFIX/lib/libsharpyuv.a" "libsharpyuv.a"
need "$PREFIX/lib/libwebp.a" "libwebp.a"
need "$PREFIX/lib/libwebpmux.a" "libwebpmux.a"
need "$PREFIX/lib/libz.a" "libz.a"
need "$BUILD/heif/build/heifio/libheifio.a" "libheifio.a"

mkdir -p "$stage/relink/objects/heif-dec" "$stage/relink/objects/heif-info"
# CMake names objects .o on Unix and .obj on Windows, so copy them into the
# archive under a stable .o name for the relink scripts.
object_for() { # dir stem
  local ext
  for ext in o obj; do
    [[ -f $1/$2.$ext ]] && { echo "$1/$2.$ext"; return 0; }
  done
  echo "relink: missing object: ${1#"$root"/}/$2.o (or .obj)" >&2
  return 1
}
take_object() { # dir stem destdir name
  local src
  src=$(object_for "$1" "$2") || exit 1
  cp "$src" "$3/$4.o"
}
take_object "$BUILD/heif/build/examples/CMakeFiles/heif-dec.dir" heif_dec.cc "$stage/relink/objects/heif-dec" heif_dec.cc
take_object "$BUILD/heif/build/examples/CMakeFiles/heif-dec.dir" common.cc "$stage/relink/objects/heif-dec" common.cc
take_object "$BUILD/heif/build/examples/CMakeFiles/heif-info.dir" heif_info.cc "$stage/relink/objects/heif-info" heif_info.cc
take_object "$BUILD/heif/build/examples/CMakeFiles/heif-info.dir" common.cc "$stage/relink/objects/heif-info" common.cc

# Reproduce the upstream link commands, but with overridable library paths.
cat >"$stage/relink/relink-heif-dec.sh" <<'EOF'
#!/usr/bin/env bash
# Relink heif-dec (installed as heif-convert) against the archives in this
# directory or against rebuilt libraries supplied by the caller.
set -euo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
CXX=${CXX:-c++}
LIBHEIF_A=${LIBHEIF_A:-$here/libheif.a}
LIBDE265_A=${LIBDE265_A:-$here/libde265.a}
static=()
late=()
out=$here/heif-dec
case $(uname -s) in
  Darwin) ;;
  MINGW*|MSYS*|CYGWIN*) out=$here/heif-dec.exe; static=(-static); late=(-lwinpthread) ;;
  *) static=(-static) ;;
esac
exec "$CXX" -DLIBDE265_STATIC_BUILD -O3 -DNDEBUG "${static[@]}" \
  "$here/objects/heif-dec/heif_dec.cc.o" "$here/objects/heif-dec/common.cc.o" \
  -o "$out" \
  "$LIBHEIF_A" "$here/libheifio.a" "$LIBHEIF_A" "$LIBDE265_A" \
  "$here/libaom.a" "$here/libjpeg.a" "$here/libpng16.a" "$here/libz.a" \
  "$here/libwebpmux.a" "$here/libwebp.a" "$here/libsharpyuv.a" -lm "${late[@]}"
EOF

cat >"$stage/relink/relink-heif-info.sh" <<'EOF'
#!/usr/bin/env bash
# Relink heif-info against the archives in this directory or against rebuilt
# libraries supplied by the caller.
set -euo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
CXX=${CXX:-c++}
LIBHEIF_A=${LIBHEIF_A:-$here/libheif.a}
LIBDE265_A=${LIBDE265_A:-$here/libde265.a}
static=()
late=()
out=$here/heif-info
case $(uname -s) in
  Darwin) ;;
  MINGW*|MSYS*|CYGWIN*) out=$here/heif-info.exe; static=(-static); late=(-lwinpthread) ;;
  *) static=(-static) ;;
esac
exec "$CXX" -DLIBDE265_STATIC_BUILD -O3 -DNDEBUG "${static[@]}" \
  "$here/objects/heif-info/heif_info.cc.o" "$here/objects/heif-info/common.cc.o" \
  -o "$out" \
  "$LIBHEIF_A" "$LIBDE265_A" "$here/libaom.a" -lm "${late[@]}"
EOF
chmod +x "$stage/relink/relink-heif-dec.sh" "$stage/relink/relink-heif-info.sh"

cat >"$stage/RELINK.md" <<EOF
# Relinking the LGPL HEIF tools: $target

\`heif-convert\` and \`heif-info\` are statically linked with two LGPL
libraries: **libheif** (LGPL-3.0-or-later) and **libde265**
(LGPL-3.0-or-later). This archive contains the object files of those two
executables, the static archives they were linked with, and scripts that
relink them. It is the "Corresponding Application Code" required by
LGPL-3.0 section 4(d)(0).

## What is included

\`\`\`
relink/
  libheif.a  libde265.a  libheifio.a
  libaom.a  libjpeg.a  libpng16.a  libsharpyuv.a  libwebp.a  libwebpmux.a  libz.a
  objects/heif-dec/heif_dec.cc.o  objects/heif-dec/common.cc.o
  objects/heif-info/heif_info.cc.o  objects/heif-info/common.cc.o
  relink-heif-dec.sh  relink-heif-info.sh
\`\`\`

The other archives are the permissively licensed dependencies used by the
original link. They are included so the relink is self-contained.

## Rebuild a modified library

Get the complete sources from \`mins_${version}_sources.tar.xz\` (or the
upstream release) and build libde265 and libheif with the flags used by
\`tools/build.sh\`:

\`\`\`sh
# libde265
cmake -S libde265-$LIBDE265_VERSION -B build-de265 -G Ninja \\
  -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=\$PWD/prefix \\
  -DBUILD_SHARED_LIBS=OFF -DENABLE_SDL=OFF -DENABLE_DEC265=OFF
cmake --build build-de265 -j
cmake --install build-de265

# libheif
cmake -S libheif-$LIBHEIF_VERSION -B build-heif -G Ninja \\
  -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=\$PWD/prefix \\
  -DBUILD_SHARED_LIBS=OFF -DWITH_LIBDE265=ON -DWITH_LIBDE265_PLUGIN=OFF \\
  -DWITH_AOM_DECODER=ON -DWITH_AOM_ENCODER=OFF -DWITH_EXAMPLES=ON \\
  -DWITH_X265=OFF -DWITH_X264=OFF -DWITH_DAV1D=OFF -DWITH_RAV1E=OFF \\
  -DWITH_SvtEnc=OFF -DWITH_KVAZAAR=OFF -DWITH_UVG266=OFF -DWITH_VVDEC=OFF \\
  -DWITH_VVENC=OFF -DWITH_OpenH264_DECODER=OFF -DWITH_OPENJPEG_ENCODER=OFF \\
  -DWITH_OPENJPEG_DECODER=OFF -DWITH_OPENJPH_ENCODER=OFF \\
  -DWITH_FFMPEG_DECODER=OFF -DWITH_UNCOMPRESSED_CODEC=OFF \\
  -DWITH_WEBCODECS=OFF -DWITH_LIBSHARPYUV=OFF -DENABLE_PLUGIN_LOADING=OFF \\
  -DCMAKE_CXX_FLAGS=-DLIBDE265_STATIC_BUILD \\
  -DAOM_LIBRARY=\$PWD/relink/libaom.a -DAOM_INCLUDE_DIR=\$PWD/prefix/include \\
  -DLIBDE265_LIBRARY=\$PWD/prefix/lib/libde265.a \\
  -DLIBDE265_INCLUDE_DIR=\$PWD/prefix/include
cmake --build build-heif -j
\`\`\`

## Relink

\`\`\`sh
cd relink
LIBHEIF_A=\$PWD/prefix/lib/libheif.a \\
LIBDE265_A=\$PWD/prefix/lib/libde265.a \\
./relink-heif-dec.sh
LIBHEIF_A=\$PWD/prefix/lib/libheif.a \\
LIBDE265_A=\$PWD/prefix/lib/libde265.a \\
./relink-heif-info.sh
\`\`\`

The scripts default to the archives in this directory and write
\`heif-dec\`/\`heif-info\` (with an \`.exe\` suffix on Windows). Set \`CXX\` if
your compiler is not \`c++\`. The archived objects are normalized to \`.o\`
regardless of the platform they were built on.

## Install the result

\`mins\` extracts its tools to
\`<user cache dir>/mins/<version>-<buildhash>/\`; \`MINS_TOOLS_DIR\` overrides that
path. Replace \`heif-dec\`/\`heif-info\` there (and refresh the extraction
cache's checksums or use a fresh \`MINS_TOOLS_DIR\`), then run \`mins\` with
\`--no-tui\` to test. \`heif-convert\` is a symlink to \`heif-dec\` in the
upstream install; keep that relationship.

## FFmpeg

FFmpeg is itself an LGPL work and does not link a separate LGPL library. Its
complete source is in \`mins_${version}_sources.tar.xz\`; rebuild it with
\`tools/build.sh\`, which configures it without \`--enable-gpl\`,
\`--enable-nonfree\`, or \`--enable-version3\`.
EOF

write_checksums "$stage"
archive=$out/mins_${version}_relink_${target}.tar.xz
make_archive "mins_${version}_relink_${target}" "$archive"
printf 'sources: sha256 %s\n' "$(hash_cmd "$archive" | awk '{print $1}')"
