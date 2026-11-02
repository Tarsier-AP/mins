#!/usr/bin/env bash
# Build the pinned media toolchain from source into $PREFIX.
#
# Intended to run on a native runner (Linux, macOS, or MSYS2 on Windows).
# Requires: cmake, ninja, meson, pkg-config, nasm, git, a C/C++ compiler, and
# (for ViSQOL) bazel. Run tools/fetch-sources.sh first.
#
#   PREFIX=/opt/mins JOBS=8 bash tools/build.sh
#
# Windows ViSQOL is best-effort: set WITH_VISQOL=0 to skip it. The app then
# requires VISQOL_BIN at runtime, exactly as documented.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=versions.env
source "$root/tools/versions.env"

SRC=${SRC:-$root/tools/build/src}
PREFIX=${PREFIX:-$root/tools/build/prefix}
JOBS=${JOBS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)}
BUILD=${BUILD:-$root/tools/build/obj}
WITH_VISQOL=${WITH_VISQOL:-1}

mkdir -p "$SRC" "$PREFIX" "$BUILD"
export PKG_CONFIG_PATH="$PREFIX/lib/pkgconfig:$PREFIX/lib64/pkgconfig:$PREFIX/share/pkgconfig:${PKG_CONFIG_PATH:-}"
export CMAKE_PREFIX_PATH="$PREFIX:${CMAKE_PREFIX_PATH:-}"

log() { printf '\n==> %s\n' "$*"; }

unpack_top() { # tarball dest
  rm -rf "$2"; mkdir -p "$2"
  tar -xf "$1" -C "$2" --strip-components=1
}

unpack_flat() { # tarball dest (archives without a top-level directory)
  rm -rf "$2"; mkdir -p "$2"
  tar -xf "$1" -C "$2"
}

cmake_build() { # src builddir extra...
  local src=$1 dir=$2; shift 2
  rm -rf "$dir"
  cmake -S "$src" -B "$dir" -G Ninja \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_INSTALL_PREFIX="$PREFIX" \
    -DCMAKE_POSITION_INDEPENDENT_CODE=ON \
    -DBUILD_SHARED_LIBS=OFF \
    -DBUILD_TESTING=OFF \
    "$@"
  cmake --build "$dir" -j "$JOBS"
  cmake --install "$dir"
}

# ---------------------------------------------------------------- libvmaf
log "libvmaf $LIBVMAF_VERSION"
unpack_top "$SRC/libvmaf-$LIBVMAF_VERSION.tar.gz" "$BUILD/vmaf"
rm -rf "$BUILD/vmaf/build"
meson setup "$BUILD/vmaf/build" "$BUILD/vmaf" \
  --prefix="$PREFIX" --buildtype=release --libdir=lib \
  -Denable_tests=false -Denable_docs=false
ninja -C "$BUILD/vmaf/build"
ninja -C "$BUILD/vmaf/build" install

# ---------------------------------------------------------------- SVT-AV1
log "SVT-AV1 $SVTAV1_VERSION"
unpack_top "$SRC/svtav1-$SVTAV1_VERSION.tar.gz" "$BUILD/svtav1"
cmake_build "$BUILD/svtav1" "$BUILD/svtav1/build" \
  -DBUILD_APPS=OFF -DENABLE_AVX512=ON

# ---------------------------------------------------------------- libaom
log "libaom $LIBAOM_VERSION"
unpack_flat "$SRC/libaom-$LIBAOM_VERSION.tar.gz" "$BUILD/aom"
cmake_build "$BUILD/aom" "$BUILD/aom/build" \
  -DENABLE_TESTS=OFF -DENABLE_EXAMPLES=OFF -DENABLE_TOOLS=OFF \
  -DCONFIG_AV1_ENCODER=1 -DCONFIG_AV1_DECODER=1

# ---------------------------------------------------------------- libopus
log "libopus $LIBOPUS_VERSION"
unpack_top "$SRC/libopus-$LIBOPUS_VERSION.tar.gz" "$BUILD/opus"
(
  cd "$BUILD/opus"
  ./configure --prefix="$PREFIX" --disable-shared --enable-static \
    --disable-doc --disable-extra-programs
  make -j "$JOBS"
  make install
)

# ---------------------------------------------------------------- libwebp
log "libwebp $LIBWEBP_VERSION"
unpack_top "$SRC/libwebp-$LIBWEBP_VERSION.tar.gz" "$BUILD/webp"
cmake_build "$BUILD/webp" "$BUILD/webp/build" \
  -DWEBP_BUILD_ANIM_UTILS=ON \
  -DWEBP_BUILD_CWEBP=ON \
  -DWEBP_BUILD_DWEBP=ON \
  -DWEBP_BUILD_IMG2WEBP=ON \
  -DWEBP_BUILD_GIF2WEBP=OFF \
  -DWEBP_BUILD_VWEBP=OFF \
  -DWEBP_BUILD_WEBPINFO=OFF \
  -DWEBP_BUILD_WEBPMUX=OFF

# ---------------------------------------------------------------- libjxl
log "libjxl $LIBJXL_VERSION"
if [[ -d $SRC/libjxl ]]; then
  cmake_build "$SRC/libjxl" "$BUILD/jxl" \
    -DJPEGXL_ENABLE_TOOLS=ON \
    -DJPEGXL_ENABLE_DEVTOOLS=OFF \
    -DJPEGXL_ENABLE_BENCHMARK=OFF \
    -DJPEGXL_ENABLE_EXAMPLES=OFF \
    -DJPEGXL_ENABLE_MANPAGES=OFF \
    -DJPEGXL_ENABLE_SJPEG=OFF \
    -DJPEGXL_ENABLE_OPENEXR=OFF \
    -DJPEGXL_ENABLE_PLUGINS=OFF \
    -DJPEGXL_ENABLE_DOXYGEN=OFF \
    -DJPEGXL_ENABLE_JNI=OFF
else
  echo "libjxl source missing; run tools/fetch-sources.sh" >&2
  exit 1
fi

# ---------------------------------------------------------------- libde265
log "libde265 $LIBDE265_VERSION"
unpack_top "$SRC/libde265-$LIBDE265_VERSION.tar.gz" "$BUILD/de265"
cmake_build "$BUILD/de265" "$BUILD/de265/build" \
  -DENABLE_SDL=OFF -DENABLE_DEC265=OFF

# ---------------------------------------------------------------- libheif
log "libheif $LIBHEIF_VERSION"
unpack_top "$SRC/libheif-$LIBHEIF_VERSION.tar.gz" "$BUILD/heif"
cmake_build "$BUILD/heif" "$BUILD/heif/build" \
  -DWITH_LIBDE265=ON \
  -DWITH_LIBDE265_PLUGIN=OFF \
  -DWITH_X265=OFF \
  -DWITH_AOM_DECODER=ON \
  -DWITH_AOM_ENCODER=OFF \
  -DWITH_DAV1D=OFF \
  -DWITH_RAV1E=OFF \
  -DWITH_SVT_ENCODER=OFF \
  -DWITH_LIBSHARPYUV=OFF \
  -DWITH_EXAMPLES=ON \
  -DWITH_JPEG_DECODER=OFF \
  -DWITH_JPEG_ENCODER=OFF \
  -DENABLE_PLUGIN_LOADING=OFF

# ---------------------------------------------------------------- libavif
log "libavif $LIBAVIF_VERSION"
unpack_top "$SRC/libavif-$LIBAVIF_VERSION.tar.gz" "$BUILD/avif"
cmake_build "$BUILD/avif" "$BUILD/avif/build" \
  -DAVIF_CODEC_AOM=SYSTEM \
  -DAVIF_BUILD_APPS=ON \
  -DAVIF_BUILD_TESTS=OFF \
  -DAVIF_ENABLE_WERROR=OFF

# ---------------------------------------------------------------- FFmpeg
log "FFmpeg $FFMPEG_VERSION"
unpack_top "$SRC/ffmpeg-$FFMPEG_VERSION.tar.xz" "$BUILD/ffmpeg"
(
  cd "$BUILD/ffmpeg"
  extra=()
  case "$(uname -s)" in
    Linux) extra+=(--extra-ldflags=-static --extra-libs=-lm) ;;
    Darwin) extra+=(--enable-videotoolbox --enable-audiotoolbox) ;;
    MINGW*|MSYS*) extra+=(--target-os=mingw32 --arch=x86_64) ;;
  esac
  ./configure \
    --prefix="$PREFIX" \
    --disable-doc --disable-debug --disable-ffplay \
    --disable-shared --enable-static \
    --pkg-config-flags=--static \
    --enable-gpl \
    --enable-libvmaf \
    --enable-libsvtav1 \
    --enable-libaom \
    --enable-libjxl \
    --enable-libwebp \
    --enable-libopus \
    "${extra[@]}"
  make -j "$JOBS"
  make install
)

# ---------------------------------------------------------------- ViSQOL
if [[ $WITH_VISQOL == 1 ]]; then
  log "ViSQOL $VISQOL_VERSION"
  bash "$root/tools/build-visqol.sh"
else
  log "ViSQOL skipped (WITH_VISQOL=$WITH_VISQOL)"
fi

log "toolchain installed under $PREFIX"
