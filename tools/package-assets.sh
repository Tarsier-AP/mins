#!/usr/bin/env bash
# Package $PREFIX/bin into internal/tools/assets/<os>_<arch>/.
#
#   bash tools/package-assets.sh linux_amd64
#
# Every tool is required: a release bundle is either complete or not produced
# (ADR 0008). ViSQOL is no exception — the default audio metric needs it, and
# its SVR model ships with it.
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
  windows_*) ext=.exe ;;
  *) ext="" ;;
esac

tools=(ffmpeg ffprobe visqol cwebp dwebp img2webp anim_dump avifenc avifdec cjxl djxl heif-convert heif-info)
model_name=libsvm_nu_svr_model.txt

rm -rf "$dest"
mkdir -p "$dest"
: >"$dest/sha256sums.txt"

add() { # name
  local name=$1
  local src="$PREFIX/bin/$name$ext"
  if [[ ! -f $src ]]; then
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

# The SVR model is part of ViSQOL: a binary without it cannot run, and a model
# without the binary is dead weight. Requiring both here keeps that promise.
model="$PREFIX/share/$model_name"
if [[ ! -f $model ]]; then
  echo "package: missing required ViSQOL model: $model" >&2
  exit 1
fi
out="$dest/$model_name"
cp "$model" "$out"
hash=$(sha256sum "$out" 2>/dev/null | cut -d' ' -f1 || shasum -a 256 "$out" | cut -d' ' -f1)
gzip -9 -n -c "$out" >"$out.gz"
rm -f "$out"
printf '%s  %s\n' "$hash" "$model_name" >>"$dest/sha256sums.txt"
echo "package: $model_name"

# The .keep file keeps the empty asset directory in git.
: >"$dest/.keep"
echo "package: $dest"
