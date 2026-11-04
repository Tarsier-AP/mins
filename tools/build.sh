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

SRC=${SRC:-$root/tools/.build/src}
PREFIX=${PREFIX:-$root/tools/.build/prefix}
JOBS=${JOBS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)}
BUILD=${BUILD:-$root/tools/.build/obj}
WITH_VISQOL=${WITH_VISQOL:-1}
OS=$(uname -s)
BIN_EXT=""
case "$OS" in MINGW*|MSYS*|CYGWIN*) BIN_EXT=.exe ;; esac
# Prefer static archives when CMake looks for optional image dependencies
# (libpng/libjpeg/libgif). Otherwise a macOS/Windows release would inherit
# Homebrew/MSYS2 DLLs that users do not have.
STATIC_LIB_SUFFIXES=".a;.dylib;.so"

mkdir -p "$SRC" "$PREFIX" "$BUILD"
export PKG_CONFIG_PATH="$PREFIX/lib/pkgconfig:$PREFIX/lib64/pkgconfig:$PREFIX/share/pkgconfig:${PKG_CONFIG_PATH:-}"
export CMAKE_PREFIX_PATH="$PREFIX:${CMAKE_PREFIX_PATH:-}"

log() { printf '\n==> %s\n' "$*"; }

# audit_binary refuses to package an executable that still depends on a
# non-system shared library. Release archives contain only executables, so a
# stray Homebrew/MSYS2 dylib or DLL would make the bundle fail on a clean
# machine.
audit_binary() { # path
  local f=$1 bad=''
  [[ -f $f ]] || { echo "audit: missing $f" >&2; return 1; }
  case "$OS" in
    Linux)
      local lddout
      lddout=$(ldd "$f" 2>&1 || true)
      if grep -qE 'not a dynamic executable|statically linked' <<<"$lddout"; then
        return 0
      fi
      # Keep only the loader and the minimal runtime that every distro
      # ships. libpng/libjpeg/libz/Homebrew libraries must be static.
      bad=$( { grep -E '=>' <<<"$lddout" \
          | sed -E 's/.*=>[[:space:]]*([^[:space:]]+).*/\1/';
        grep -E '^[[:space:]]*/' <<<"$lddout" | awk '{print $1}'; } \
        | grep -vE '^/(lib|lib64|usr/lib|usr/lib64)/([^/]+/)?(ld-linux[^/]*|libc\.so\.[0-9]+|libm\.so\.[0-9]+|libstdc\+\+\.so\.[0-9]+|libgcc_s\.so\.[0-9]+|libpthread\.so\.[0-9]+|libdl\.so\.[0-9]+|librt\.so\.[0-9]+|libatomic\.so\.[0-9]+)$' || true)
      ;;
    Darwin)
      bad=$(otool -L "$f" | tail -n +2 | awk '{print $1}' \
        | grep -vE '^(/usr/lib/|/System/Library/)' || true)
      ;;
    MINGW*|MSYS*|CYGWIN*)
      bad=$(objdump -p "$f" | awk '/DLL Name:/{print $3}' \
        | grep -viE '^(KERNEL32|USER32|GDI32|ADVAPI32|SHELL32|ole32|OLEAUT32|WS2_32|WSOCK32|IPHLPAPI|bcrypt|crypt32|secur32|COMCTL32|PSAPI|msvcrt|ucrtbase|ntdll|dbghelp|DWMAPI|MF|MFPlat|MFReadWrite)\.dll$' || true)
      ;;
  esac
  if [[ -n $bad ]]; then
    printf 'audit: %s has non-system shared dependencies:\n%s\n' "$f" "$bad" >&2
    return 1
  fi
}

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
  # GNUInstallDirs defaults to lib64 on a 64-bit host that is not Debian.
  # Keep the prefix layout flat (prefix/lib) unless a project overrides it.
  local libdir=lib args=() a
  for a in "$@"; do
    case $a in
      -DCMAKE_INSTALL_LIBDIR=*) libdir=${a#-DCMAKE_INSTALL_LIBDIR=} ;;
      *) args+=("$a") ;;
    esac
  done
  local flags=(-DCMAKE_FIND_LIBRARY_SUFFIXES="$STATIC_LIB_SUFFIXES")
  case "$OS" in
    Linux|MINGW*|MSYS*|CYGWIN*) flags+=(-DCMAKE_EXE_LINKER_FLAGS=-static) ;;
  esac
  rm -rf "$dir"
  cmake -S "$src" -B "$dir" -G Ninja \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_INSTALL_PREFIX="$PREFIX" \
    -DCMAKE_INSTALL_LIBDIR="$libdir" \
    -DCMAKE_POSITION_INDEPENDENT_CODE=ON \
    -DBUILD_SHARED_LIBS=OFF \
    -DBUILD_TESTING=OFF \
    -DCMAKE_POLICY_VERSION_MINIMUM=3.5 \
    "${flags[@]}" \
    "${args[@]}"
  cmake --build "$dir" -j "$JOBS"
  cmake --install "$dir"
}

# ---------------------------------------------------------------- libvmaf
log "libvmaf $LIBVMAF_VERSION"
unpack_top "$SRC/libvmaf-$LIBVMAF_VERSION.tar.gz" "$BUILD/vmaf"
vmaf_src=$BUILD/vmaf
[[ -f $vmaf_src/meson.build ]] || vmaf_src=$BUILD/vmaf/libvmaf
rm -rf "$BUILD/vmaf/build"
meson setup "$BUILD/vmaf/build" "$vmaf_src" \
  --prefix="$PREFIX" --buildtype=release --libdir=lib \
  --default-library=static \
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
# libaom's source tree contains its own build/cmake directory, so the CMake
# build tree must live outside it.
cmake_build "$BUILD/aom" "$BUILD/aom-obj" \
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
    -DJPEGXL_ENABLE_TCMALLOC=OFF \
    -DJPEGXL_ENABLE_PLUGINS=OFF \
    -DJPEGXL_ENABLE_DOXYGEN=OFF \
    -DJPEGXL_ENABLE_JNI=OFF \
    -DJPEGXL_STATIC=ON \
    -DJPEGXL_BUNDLE_LIBPNG=ON \
    -DZLIB_BUILD_EXAMPLES=OFF \
    -DPNG_TESTS=OFF \
    -DPNG_TOOLS=OFF \
    -DJPEGXL_FORCE_SYSTEM_BROTLI=OFF \
    -DJPEGXL_FORCE_SYSTEM_HWY=OFF \
    -DCMAKE_FIND_LIBRARY_SUFFIXES="$STATIC_LIB_SUFFIXES"
else
  echo "libjxl source missing; run tools/fetch-sources.sh" >&2
  exit 1
fi

# The zlib bundled with libjxl always defines a shared target and installs it.
# Drop it so the later CMake builds and FFmpeg link the static archive.
rm -f "$PREFIX"/lib/libz.so* "$PREFIX"/lib/libz*.dylib \
  "$PREFIX"/lib/libz*.dll.a "$PREFIX"/bin/libz*.dll "$PREFIX"/bin/zlib*.dll

# ---------------------------------------------------------------- libpng / libjpeg-turbo
# The WebP and HEIF CLIs decode PNG/JPEG input. Build static copies from the
# versions vendored by libjxl, so the bundle does not depend on a distro
# shipping libpng.a/libjpeg.a.
log "libpng + libjpeg-turbo (libjxl vendored)"
cmake_build "$SRC/libjxl/third_party/libpng" "$BUILD/libpng" \
  -DPNG_SHARED=OFF -DPNG_STATIC=ON -DPNG_TESTS=OFF -DPNG_TOOLS=OFF \
  -DZLIB_ROOT="$PREFIX" -DZLIB_LIBRARY="$PREFIX/lib/libz.a" \
  -DZLIB_INCLUDE_DIR="$PREFIX/include"
cmake_build "$SRC/libjxl/third_party/libjpeg-turbo" "$BUILD/libjpeg-turbo" \
  -DCMAKE_INSTALL_LIBDIR="$PREFIX/lib" \
  -DENABLE_SHARED=OFF -DENABLE_STATIC=ON -DWITH_TURBOJPEG=ON

# CMake's FindPNG/FindJPEG prefer shared libraries even when the matching
# static archive is already in $PREFIX. Point them at the archives explicitly
# so -static links do not fail on a build host that also has the .so.
STATIC_IMG_DEPS=(
  -DZLIB_LIBRARY="$PREFIX/lib/libz.a"
  -DZLIB_LIBRARY_RELEASE="$PREFIX/lib/libz.a"
  -DZLIB_INCLUDE_DIR="$PREFIX/include"
  -DPNG_LIBRARY="$PREFIX/lib/libpng16.a"
  -DPNG_LIBRARY_RELEASE="$PREFIX/lib/libpng16.a"
  -DPNG_PNG_INCLUDE_DIR="$PREFIX/include"
  -DJPEG_LIBRARY="$PREFIX/lib/libjpeg.a"
  -DJPEG_LIBRARY_RELEASE="$PREFIX/lib/libjpeg.a"
  -DJPEG_INCLUDE_DIR="$PREFIX/include"
  -DGIF_LIBRARY="$PREFIX/lib/libgif.a"
  -DGIF_INCLUDE_DIR="$PREFIX/include"
)

# ---------------------------------------------------------------- giflib
# libwebp's anim_dump links giflib. Build the static archive here so the
# bundle does not depend on the host shipping libgif.a next to libgif.so.
log "giflib $GIFLIB_VERSION"
unpack_top "$SRC/giflib-$GIFLIB_VERSION.tar.gz" "$BUILD/giflib"
make -C "$BUILD/giflib" libgif.a \
  CC="${CC:-cc}" AR="${AR:-ar}" \
  CFLAGS="-O2 -std=gnu99 -fPIC"
install -d "$PREFIX/lib" "$PREFIX/include"
install -m644 "$BUILD/giflib/libgif.a" "$PREFIX/lib/libgif.a"
install -m644 "$BUILD/giflib/gif_lib.h" "$PREFIX/include/gif_lib.h"

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
  -DWEBP_BUILD_WEBPMUX=OFF \
  "${STATIC_IMG_DEPS[@]}"
# libwebp builds anim_dump but does not install it.
install -m755 "$BUILD/webp/build/anim_dump$BIN_EXT" "$PREFIX/bin/anim_dump$BIN_EXT"

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
  -DWITH_X264=OFF \
  -DWITH_AOM_DECODER=ON \
  -DWITH_AOM_ENCODER=OFF \
  -DWITH_DAV1D=OFF \
  -DWITH_RAV1E=OFF \
  -DWITH_SvtEnc=OFF \
  -DWITH_KVAZAAR=OFF \
  -DWITH_UVG266=OFF \
  -DWITH_VVDEC=OFF \
  -DWITH_VVENC=OFF \
  -DWITH_OpenH264_DECODER=OFF \
  -DWITH_OpenJPEG_ENCODER=OFF \
  -DWITH_OpenJPEG_DECODER=OFF \
  -DWITH_OPENJPH_ENCODER=OFF \
  -DWITH_FFMPEG_DECODER=OFF \
  -DWITH_UNCOMPRESSED_CODEC=OFF \
  -DWITH_WEBCODECS=OFF \
  -DWITH_LIBSHARPYUV=OFF \
  -DWITH_EXAMPLES=ON \
  -DWITH_EXAMPLE_HEIF_VIEW=OFF \
  -DWITH_EXAMPLE_HEIF_THUMB=OFF \
  -DWITH_GDK_PIXBUF=OFF \
  -DBUILD_DOCUMENTATION=OFF \
  -DWITH_JPEG_DECODER=OFF \
  -DWITH_JPEG_ENCODER=OFF \
  -DENABLE_PLUGIN_LOADING=OFF \
  -DCMAKE_DISABLE_FIND_PACKAGE_TIFF=ON \
  -DCMAKE_FIND_LIBRARY_SUFFIXES="$STATIC_LIB_SUFFIXES" \
  -DAOM_LIBRARY="$PREFIX/lib/libaom.a" -DAOM_INCLUDE_DIR="$PREFIX/include" \
  -DLIBDE265_LIBRARY="$PREFIX/lib/libde265.a" \
  -DLIBDE265_INCLUDE_DIR="$PREFIX/include" \
  "${STATIC_IMG_DEPS[@]}"

# ---------------------------------------------------------------- libavif
log "libavif $LIBAVIF_VERSION"
unpack_top "$SRC/libavif-$LIBAVIF_VERSION.tar.gz" "$BUILD/avif"
cmake_build "$BUILD/avif" "$BUILD/avif/build" \
  -DAVIF_CODEC_AOM=SYSTEM \
  -DAVIF_LIBYUV=OFF \
  -DAVIF_BUILD_APPS=ON \
  -DAVIF_BUILD_EXAMPLES=OFF \
  -DAVIF_BUILD_TESTS=OFF \
  -DAVIF_ENABLE_WERROR=OFF \
  -DAOM_LIBRARY="$PREFIX/lib/libaom.a" -DAOM_INCLUDE_DIR="$PREFIX/include" \
  "${STATIC_IMG_DEPS[@]}"

# ---------------------------------------------------------------- FFmpeg
log "FFmpeg $FFMPEG_VERSION"
unpack_top "$SRC/ffmpeg-$FFMPEG_VERSION.tar.xz" "$BUILD/ffmpeg"
(
  cd "$BUILD/ffmpeg"
  extra=()
  case "$OS" in
    Linux)
      # Fully static: libvmaf/libjxl are C++, so the C++ runtime must be
      # named explicitly when the final link is driven by gcc.
      extra+=(--extra-ldflags=-static --extra-libs="-lstdc++ -lm")
      ;;
    Darwin)
      # Apple does not support a fully static libSystem; only the third-party
      # libraries are static. audit_binary allows /usr/lib and /System/Library.
      extra+=(--enable-videotoolbox --enable-audiotoolbox --extra-libs="-lc++")
      ;;
    MINGW*|MSYS*|CYGWIN*)
      extra+=(--target-os=mingw32 --arch=x86_64 \
        --extra-ldflags=-static --extra-libs="-lstdc++ -lwinpthread -lm")
      ;;
  esac
  ./configure \
    --prefix="$PREFIX" \
    --disable-doc --disable-debug --disable-ffplay \
    --disable-shared --enable-static \
    --disable-autodetect \
    --enable-zlib \
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

log "verifying packaged executables"
audit_failed=0
for t in ffmpeg ffprobe cwebp dwebp img2webp anim_dump \
  avifenc avifdec cjxl djxl heif-convert heif-info; do
  audit_binary "$PREFIX/bin/$t$BIN_EXT" || audit_failed=1
done
if [[ -x $PREFIX/bin/visqol$BIN_EXT ]]; then
  if ! audit_binary "$PREFIX/bin/visqol$BIN_EXT"; then
    # ViSQOL is optional: a Bazel toolchain may inject a run-time DLL on
    # Windows. Dropping the binary makes mins ask for VISQOL_BIN instead of
    # shipping something that cannot start on a clean machine.
    echo "audit: dropping optional visqol from the bundle" >&2
    rm -f "$PREFIX/bin/visqol$BIN_EXT"
  fi
fi
[[ $audit_failed == 0 ]] || { echo "audit: required tools are not portable" >&2; exit 1; }
log "toolchain installed under $PREFIX"
