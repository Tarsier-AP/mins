#!/usr/bin/env bash
# Download the pinned toolchain sources into tools/.build/src.
#
# Run from anywhere; requires curl (or wget) and tar. libjxl is cloned with
# submodules because its release tarball does not include them.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=versions.env
source "$root/tools/versions.env"

src=${SRC:-$root/tools/.build/src}
mkdir -p "$src"

download() {
  local url=$1 dest=$2 fallback=${3:-}
  if [[ -s $dest ]]; then
    echo "sources: $(basename "$dest") (cached)"
    return
  fi
  echo "sources: $(basename "$dest") <- $url"
  local ok=0
  if command -v curl >/dev/null 2>&1; then
    curl -fL --retry 5 --retry-delay 3 --retry-all-errors \
      --connect-timeout 30 -o "$dest.part" "$url" && ok=1
  else
    wget --tries=5 --timeout=30 -O "$dest.part" "$url" && ok=1
  fi
  if [[ $ok == 0 && -n $fallback ]]; then
    echo "sources: $(basename "$dest") <- $fallback (fallback)" >&2
    rm -f "$dest.part"
    if command -v curl >/dev/null 2>&1; then
      curl -fL --retry 5 --retry-delay 3 --retry-all-errors \
        --connect-timeout 30 -o "$dest.part" "$fallback" && ok=1
    else
      wget --tries=5 --timeout=30 -O "$dest.part" "$fallback" && ok=1
    fi
  fi
  [[ $ok == 1 ]] || return 1
  mv "$dest.part" "$dest"
}

download "$FFMPEG_URL"    "$src/ffmpeg-$FFMPEG_VERSION.tar.xz"
download "$LIBVMAF_URL"   "$src/libvmaf-$LIBVMAF_VERSION.tar.gz"
download "$SVTAV1_URL"    "$src/svtav1-$SVTAV1_VERSION.tar.gz"
download "$LIBAOM_URL"    "$src/libaom-$LIBAOM_VERSION.tar.gz"
download "$LIBWEBP_URL"   "$src/libwebp-$LIBWEBP_VERSION.tar.gz"
download "$GIFLIB_URL"    "$src/giflib-$GIFLIB_VERSION.tar.gz" "$GIFLIB_FALLBACK_URL"
download "$LIBAVIF_URL"   "$src/libavif-$LIBAVIF_VERSION.tar.gz"
download "$LIBHEIF_URL"   "$src/libheif-$LIBHEIF_VERSION.tar.gz"
download "$LIBDE265_URL"  "$src/libde265-$LIBDE265_VERSION.tar.gz"
download "$LIBOPUS_URL"   "$src/libopus-$LIBOPUS_VERSION.tar.gz"
download "$VISQOL_URL"    "$src/visqol-$VISQOL_VERSION.tar.gz"

# libjxl: submodules (highway, brotli, skcms, ...) are required.
if [[ ! -d $src/libjxl/.git ]]; then
  echo "sources: libjxl $LIBJXL_VERSION (recursive clone)"
  git clone --depth 1 --branch "v$LIBJXL_VERSION" --recursive \
    https://github.com/libjxl/libjxl.git "$src/libjxl"
fi

echo "sources: $src"
