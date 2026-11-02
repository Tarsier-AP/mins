#!/usr/bin/env bash
# Build ViSQOL 3.3.3 with Bazel and install it into $PREFIX.
#
# This is best-effort on Windows: CI runs it with `|| true` and then packages
# whatever exists. A missing visqol is reported by mins at runtime, not
# silently replaced by a different audio metric.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=versions.env
source "$root/tools/versions.env"

SRC=${SRC:-$root/tools/build/src}
PREFIX=${PREFIX:-$root/tools/build/prefix}
BUILD=${BUILD:-$root/tools/build/obj}

if ! command -v bazel >/dev/null 2>&1; then
  echo "visqol: bazel not found; skipping" >&2
  exit 0
fi
if [[ ! -f $SRC/visqol-$VISQOL_VERSION.tar.gz ]]; then
  echo "visqol: source tarball missing; run tools/fetch-sources.sh" >&2
  exit 1
fi

rm -rf "$BUILD/visqol"
mkdir -p "$BUILD/visqol"
tar -xf "$SRC/visqol-$VISQOL_VERSION.tar.gz" -C "$BUILD/visqol" --strip-components=1

(
  cd "$BUILD/visqol"
  bazel build -c opt //:visqol
  mkdir -p "$PREFIX/bin" "$PREFIX/share"
  cp bazel-bin/visqol "$PREFIX/bin/visqol"
  cp model/libsvm_nu_svr_model.txt "$PREFIX/share/libsvm_nu_svr_model.txt"
)
echo "visqol: installed $PREFIX/bin/visqol"
