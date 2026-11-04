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

SRC=${SRC:-$root/tools/.build/src}
PREFIX=${PREFIX:-$root/tools/.build/prefix}
BUILD=${BUILD:-$root/tools/.build/obj}
BAZEL=${BAZEL:-bazel}
export USE_BAZEL_VERSION=${USE_BAZEL_VERSION:-${BAZEL_VERSION:-5.4.1}}
PATCHES=${PATCHES:-$root/tools/patches}
OS=$(uname -s)
EXT=""
BAZEL_ARGS=()
case "$OS" in
  Linux)
    EXT=""
    # Link the C++ runtime and glibc statically too, so the bundled visqol
    # runs on older distributions than the CI runner.
    BAZEL_ARGS+=(--linkopt=-static)
    ;;
  MINGW*|MSYS*|CYGWIN*) EXT=.exe ;;
esac

if ! command -v "$BAZEL" >/dev/null 2>&1; then
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

# 3.3.3 pulls TensorFlow/TFLite only for the speech lattice model. The patch
# removes that dependency and replaces two dead download URLs, so the tool can
# be built with a plain Bazel 5 toolchain on every release target.
patchfile="$PATCHES/visqol-$VISQOL_VERSION-no-tensorflow.patch"
[[ -f $patchfile ]] || { echo "visqol: missing $patchfile" >&2; exit 1; }
patch -d "$BUILD/visqol" -p1 <"$patchfile"

(
  cd "$BUILD/visqol"
  # Trim TensorFlow-era flags that Bazel 5 rejects or that only make sense on
  # one host: an unused experimental flag, a forced macOS platform, and -ldl.
  sed -i.bak \
    -e '/experimental_repo_remote_exec/d' \
    -e '/apple_platform_type/d' \
    -e '/linkopt=-ldl/d' \
    .bazelrc
  if [[ $OS == Linux || $OS == Darwin ]]; then
    # absl 20211102 relies on <cstdint> arriving transitively; newer GCC
    # dropped that, so force-include it for every C++ translation unit.
    printf '\nbuild --cxxopt=-include --cxxopt=cstdint\n' >>.bazelrc
  fi
  # The alternate expansion keeps `set -u` happy on bash 3.2 (macOS).
  "$BAZEL" build -c opt ${BAZEL_ARGS[@]+"${BAZEL_ARGS[@]}"} //:visqol
  mkdir -p "$PREFIX/bin" "$PREFIX/share"
  cp "bazel-bin/visqol$EXT" "$PREFIX/bin/visqol$EXT"
  chmod 755 "$PREFIX/bin/visqol$EXT"
  cp model/libsvm_nu_svr_model.txt "$PREFIX/share/libsvm_nu_svr_model.txt"
)
echo "visqol: installed $PREFIX/bin/visqol$EXT (bazel $USE_BAZEL_VERSION)"
