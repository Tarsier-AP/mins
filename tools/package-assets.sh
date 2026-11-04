#!/usr/bin/env bash
# Package $PREFIX/bin into internal/tools/assets/<os>_<arch>/.
#
#   bash tools/package-assets.sh linux_amd64
#
# Each tool is gzip-compressed (gzip -9 -n) and a sha256sums.txt of the
# *decompressed* files is written next to it; internal/tools verifies cached
# copies against that manifest.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
target=${1:?usage: package-assets.sh <os>_<arch>}
PREFIX=${PREFIX:-$root/tools/.build/prefix}
dest="$root/internal/tools/assets/$target"

case $target in
  windows_*) ext=.exe; suffix="" ;;
  *) ext=""; suffix="" ;;
esac

tools=(ffmpeg ffprobe cwebp dwebp img2webp anim_dump avifenc avifdec cjxl djxl heif-convert heif-info)
optional=(visqol)

rm -rf "$dest"
mkdir -p "$dest"
: >"$dest/sha256sums.txt"

add() { # name [optional]
  local name=$1 optional=${2:-0}
  local src="$PREFIX/bin/$name$ext"
  if [[ ! -f $src ]]; then
    if [[ $optional == 1 ]]; then
      echo "package: $name not built; skipping (optional)" >&2
      return
    fi
    echo "package: missing required tool: $src" >&2
    exit 1
  fi
  local out="$dest/$name$ext"
  cp "$src" "$out"
  chmod 755 "$out"
  local hash
  hash=$(sha256sum "$out" 2>/dev/null | cut -d' ' -f1 || shasum -a 256 "$out" | cut -d' ' -f1)
  gzip -9 -n -c "$out" >"$out.gz"
  rm -f "$out"
  printf '%s  %s\n' "$hash" "$name$ext" >>"$dest/sha256sums.txt"
  echo "package: $name$ext"
}

for t in "${tools[@]}"; do add "$t"; done
for t in "${optional[@]}"; do add "$t" 1; done

model="$PREFIX/share/libsvm_nu_svr_model.txt"
if [[ -f $model ]]; then
  out="$dest/libsvm_nu_svr_model.txt"
  cp "$model" "$out"
  hash=$(sha256sum "$out" 2>/dev/null | cut -d' ' -f1 || shasum -a 256 "$out" | cut -d' ' -f1)
  gzip -9 -n -c "$out" >"$out.gz"
  rm -f "$out"
  printf '%s  %s\n' "$hash" "libsvm_nu_svr_model.txt" >>"$dest/sha256sums.txt"
  echo "package: libsvm_nu_svr_model.txt"
else
  echo "package: ViSQOL model not built; skipping (optional)" >&2
fi

# The .keep file keeps the empty asset directory in git.
: >"$dest/.keep"
echo "package: $dest"
