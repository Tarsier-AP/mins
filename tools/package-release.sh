#!/usr/bin/env bash
# Turn the per-target build directories into release archives.
#
#   bash tools/package-release.sh tools/dist 2.0.0
#
# Expects tools/dist/<os>-<arch>/{mins,mins.exe} and writes
# tools/dist/mins_<version>_<os>_<arch>.{tar.gz,zip} plus
# tools/dist/sha256sums.txt.
#
# Every archive carries LICENSE, THIRD_PARTY_NOTICES.md, and the full
# LICENSES/ tree. The source is staged in a temporary directory so the archive
# layout is identical on GNU tar, bsdtar, and MSYS2 zip.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
dist=${1:?usage: package-release.sh <dist-dir> <version>}
version=${2:?usage: package-release.sh <dist-dir> <version>}
dist=$(cd "$dist" && pwd)

[[ -f $root/THIRD_PARTY_NOTICES.md ]] || {
  echo "release: missing THIRD_PARTY_NOTICES.md (run 'make licenses')" >&2
  exit 1
}
[[ -d $root/LICENSES ]] || {
  echo "release: missing LICENSES/ (run 'make licenses')" >&2
  exit 1
}

# Only remove the archives this script owns. A sources or relink archive in
# the same directory is built by tools/collect-sources.sh and must survive a
# re-package.
rm -f "$dist"/mins_"$version"_*.tar.gz "$dist"/mins_"$version"_*.zip \
  "$dist/sha256sums.txt"

stage=$(mktemp -d "${TMPDIR:-/tmp}/mins-release.XXXXXX")
trap 'rm -rf "$stage"' EXIT

for dir in "$dist"/*/; do
  [[ -d $dir ]] || continue
  name=$(basename "$dir")
  os=${name%%-*}
  arch=${name##*-}
  bin=mins
  [[ $os == windows ]] && bin=mins.exe
  [[ -f $dir/$bin ]] || {
    echo "release: missing $dir$bin" >&2
    exit 1
  }

  rm -rf "$stage"/*
  cp "$dir/$bin" "$stage/"
  cp "$root/LICENSE" "$stage/LICENSE"
  cp "$root/THIRD_PARTY_NOTICES.md" "$stage/THIRD_PARTY_NOTICES.md"
  cp -R "$root/LICENSES" "$stage/LICENSES"

  if [[ $os == windows ]]; then
    archive="$dist/mins_${version}_${os}_${arch}.zip"
    (cd "$stage" && zip -q -9 -r "$archive" mins.exe LICENSE THIRD_PARTY_NOTICES.md LICENSES)
  else
    archive="$dist/mins_${version}_${os}_${arch}.tar.gz"
    tar -czf "$archive" -C "$stage" mins LICENSE THIRD_PARTY_NOTICES.md LICENSES
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
