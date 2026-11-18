#!/usr/bin/env bash
# Build ViSQOL 3.3.3 with Bazel and install it into $PREFIX.
#
# ViSQOL is required by every release bundle (ADR 0008), so a missing Bazel or
# source tarball is fatal here. WITH_VISQOL=0 in build.sh is a local-dev escape
# only; tools/package-assets.sh refuses to package a prefix without visqol and
# its SVR model.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=versions.env
source "$root/tools/versions.env"

SRC=${SRC:-$root/tools/.build/src}
PREFIX=${PREFIX:-$root/tools/.build/prefix}
BUILD=${BUILD:-$root/tools/.build/obj}
# Bazelisk installs as "bazelisk"; a distro package or a symlink is "bazel".
# MSYS2/Git Bash may expose only the .exe form. Accept any of them, plus an
# explicit BAZEL=/path/to/bazel (what CI uses on Windows).
BAZEL=${BAZEL:-}
if [[ -z $BAZEL ]]; then
  for candidate in bazel bazelisk bazel.exe bazelisk.exe; do
    if command -v "$candidate" >/dev/null 2>&1; then
      BAZEL=$candidate
      break
    fi
  done
fi
if [[ -z $BAZEL ]]; then
  echo "visqol: bazel/bazelisk not found; install Bazelisk before building the toolchain" >&2
  exit 1
fi
# `command -v` only searches PATH; BAZEL may legitimately be a full path.
if [[ $BAZEL == */* ]]; then
  if [[ ! -x $BAZEL ]]; then
    echo "visqol: BAZEL=$BAZEL is not an executable file" >&2
    exit 1
  fi
elif ! command -v "$BAZEL" >/dev/null 2>&1; then
  echo "visqol: bazel/bazelisk not found; install Bazelisk before building the toolchain" >&2
  exit 1
fi
export USE_BAZEL_VERSION=${USE_BAZEL_VERSION:-${BAZEL_VERSION:-6.5.0}}
PATCHES=${PATCHES:-$root/tools/patches}
OS=$(uname -s)
EXT=""
BAZEL_ARGS=()
BAZEL_STARTUP_ARGS=()
case "$OS" in
  Linux)
    EXT=""
    # Static linking is required so the bundled binary runs on distributions
    # older than the runner. Bazel's own link line carries -Wl,--gc-sections;
    # on Ubuntu 24.04's static glibc that discards .text while leaving the
    # SystemTap .note.stapsdt relocations behind, and ld aborts with
    # "relocation refers to local symbol .text ... defined in a discarded
    # section". --no-gc-sections comes later on the command line and wins.
    BAZEL_ARGS+=(--linkopt=-static --linkopt=-Wl,--no-gc-sections)
    ;;
  Darwin)
    # zlib 1.2.11 (pinned by Bazel's WORKSPACE deps) installs its own
    # `#define fdopen(fd,mode) NULL` fallback for Mac OS whenever fdopen is
    # not already a macro (zutil.h:140). Modern macOS SDKs declare fdopen as
    # a plain function, so the guard fires; every later mention of fdopen then
    # expands to NULL and stops parsing:
    #   zutil.h:140:33: note: expanded from macro 'fdopen'
    #   _stdio.h:318:7: error: expected identifier or '('
    # Defining fdopen on the command line makes zlib's `#ifndef fdopen` guard
    # false, so the fallback is never installed. A self-referential
    # -Dfdopen=fdopen counts as defined but expands to itself (the
    # preprocessor will not re-expand it), so real fdopen tokens still work.
    # --copt rather than --per_file_copt: the flag has to hold for every
    # zlib translation unit, and a missed one silently compiles gz_open's
    # error path to `return NULL`.
    BAZEL_ARGS+=(--copt=-Dfdopen=fdopen --host_copt=-Dfdopen=fdopen)
    ;;
  MINGW*|MSYS*|CYGWIN*) EXT=.exe ;;
esac
case "$OS" in
  MINGW*|MSYS*|CYGWIN*)
    # The MSVC toolchain enables dynamic_link_msvcrt, whose /MD and
    # /DEFAULTLIB:msvcrt.lib flag sets are gated on
    # `not_features = ["static_link_msvcrt"]`. Turning the static feature on
    # therefore swaps in /MT and /DEFAULTLIB:libcmt.lib and drops the dynamic
    # ones, which is what the portability audit requires: the binary must not
    # depend on vcruntime140.dll/msvcp140.dll.
    #
    # Do not disable dynamic_link_msvcrt instead: Bazel implies it, and
    # disabling an implied feature is rejected with "unconditionally implies
    # feature ... which is unsupported by this rule". Do not pass /MT through
    # --copt either: MSYS2 rewrites the /MT value into a drive path
    # (D:/.../msys64/MT) before Bazel ever sees it.
    BAZEL_ARGS+=(--features=static_link_msvcrt)
    # Bazel's default output base under the runner's profile plus the
    # external-repo paths can exceed Windows' MAX_PATH during this build. Use
    # a short root on the workspace drive: Bazel refuses to create junctions
    # when the output base lives on another drive.
    if command -v cygpath >/dev/null 2>&1; then
      drive=$(cygpath -m "$BUILD")
      drive=${drive%%:*}
      BAZEL_STARTUP_ARGS+=(--output_user_root="$drive:/bazel-visqol")
    fi
    ;;
esac
# Optional shared download cache: CI restores it between runs so the
# 2022-era WORKSPACE dependencies do not have to be fetched every time.
if [[ -n ${BAZEL_REPOSITORY_CACHE:-} ]]; then
  BAZEL_ARGS+=(--repository_cache="$BAZEL_REPOSITORY_CACHE")
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

model="$BUILD/visqol/model/libsvm_nu_svr_model.txt"
if [[ ! -f $model ]]; then
  echo "visqol: source tree has no model/libsvm_nu_svr_model.txt" >&2
  exit 1
fi
if [[ -n ${VISQOL_MODEL_SHA256:-} ]]; then
  if command -v sha256sum >/dev/null 2>&1; then
    got=$(sha256sum "$model" | cut -d' ' -f1)
  else
    got=$(shasum -a 256 "$model" | cut -d' ' -f1)
  fi
  if [[ $got != "$VISQOL_MODEL_SHA256" ]]; then
    echo "visqol: SVR model SHA-256 mismatch (got $got, want $VISQOL_MODEL_SHA256)" >&2
    exit 1
  fi
fi

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
  "$BAZEL" ${BAZEL_STARTUP_ARGS[@]+"${BAZEL_STARTUP_ARGS[@]}"} \
    build -c opt ${BAZEL_ARGS[@]+"${BAZEL_ARGS[@]}"} //:visqol
  mkdir -p "$PREFIX/bin" "$PREFIX/share"
  cp "bazel-bin/visqol$EXT" "$PREFIX/bin/visqol$EXT"
  chmod 755 "$PREFIX/bin/visqol$EXT"
  cp "$model" "$PREFIX/share/libsvm_nu_svr_model.txt"
)
echo "visqol: installed $PREFIX/bin/visqol$EXT (bazel $USE_BAZEL_VERSION)"
