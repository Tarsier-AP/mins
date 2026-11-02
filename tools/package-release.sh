#!/usr/bin/env bash
# Turn the per-target build directories into release archives.
#
#   bash tools/package-release.sh tools/dist 2.0.0
#
# Expects tools/dist/<os>-<arch>/{mins,mins.exe,LICENSE,THIRD_PARTY_NOTICES.md}
# and writes tools/dist/mins_<version>_<os>_<arch>.{tar.gz,zip} plus
# tools/dist/sha256sums.txt.
set -euo pipefail

dist=${1:?usage: package-release.sh <dist-dir> <version>}
version=${2:?usage: package-release.sh <dist-dir> <version>}
dist=$(cd "$dist" && pwd)

rm -f "$dist"/mins_"$version"_* "$dist/sha256sums.txt"

for dir in "$dist"/*/; do
  [[ -d $dir ]] || continue
  name=$(basename "$dir")
  os=${name%%-*}
  arch=${name##*-}
  if [[ $os == windows ]]; then
    archive="$dist/mins_${version}_${os}_${arch}.zip"
    (cd "$dir" && zip -q -9 "$archive" mins.exe LICENSE THIRD_PARTY_NOTICES.md)
  else
    archive="$dist/mins_${version}_${os}_${arch}.tar.gz"
    tar -czf "$archive" -C "$dir" mins LICENSE THIRD_PARTY_NOTICES.md
  fi
  echo "release: $(basename "$archive")"
done

(
  cd "$dist"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum mins_"$version"_* >sha256sums.txt
  else
    shasum -a 256 mins_"$version"_* >sha256sums.txt
  fi
)
echo "release: $dist/sha256sums.txt"
