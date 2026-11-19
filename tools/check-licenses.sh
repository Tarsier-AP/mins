#!/usr/bin/env bash
# Fail the build or release when the licensing materials drift.
#
#   bash tools/check-licenses.sh          # offline checks (make check)
#   bash tools/check-licenses.sh --built  # also audit a built $PREFIX
#
# Offline checks verify the inventory, license texts, notices, FFmpeg
# configure flags, and release wiring. The --built checks additionally run the
# built executables and verify the FFmpeg license banner, the static link
# audit, and the relink inputs that tools/collect-sources.sh packages.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
# versions.env is a simple KEY=VALUE file with ${VAR} references. Eval a
# CR-stripped copy instead of sourcing it: macOS ships bash 3.2, where
# `source <(...)` dies with "sed: stdout: Broken pipe" and leaves every
# variable unset. .gitattributes already enforces LF, but a stale
# checkout must not poison the version values either.
eval "$(sed 's/\r$//' "$root/tools/versions.env")"

built=0
case "${1:-}" in
  --built) built=1 ;;
  "") ;;
  *) echo "usage: $0 [--built]" >&2; exit 2 ;;
esac

OS=$(uname -s)
BIN_EXT=""
case "$OS" in MINGW*|MSYS*|CYGWIN*) BIN_EXT=.exe ;; esac
# shellcheck source=audit.sh
source "$root/tools/audit.sh"

PREFIX=${PREFIX:-$root/tools/.build/prefix}
BUILD=${BUILD:-$root/tools/.build/obj}
inventory=$root/tools/licenses/inventory.tsv
bundle_tools=$root/tools/bundle-tools.txt

errors=0
fail() { echo "licenses: $*" >&2; errors=$((errors + 1)); }
need_file() { [[ -s $1 ]] || fail "missing or empty: ${1#"$root"/}"; }
need_tool() { [[ -f $1 ]] || fail "missing built tool: ${1#"$root"/}"; }

tmp=$(mktemp -d "${TMPDIR:-/tmp}/mins-licenses.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

# ---- 1. inventory and notices ---------------------------------------------
need_file "$inventory"
need_file "$bundle_tools"
[[ ${errors} -eq 0 ]] || { echo "licenses: cannot continue without the inventory" >&2; exit 1; }

if ! bash "$root/tools/gen-notices.sh" --check; then
  fail "THIRD_PARTY_NOTICES.md is stale (run 'make licenses')"
fi

# Every ${VAR} used in the inventory must exist, and every pinned runtime
# version must be represented in the inventory. NASM and Bazel are build-time
# tools and are not shipped.
awk -F'\t' '!/^#/ && NF >= 4 { print $3 }' "$inventory" \
  | grep -oE '\$\{[A-Za-z0-9_]+\}' | sed 's/[${}]//g' | sort -u >"$tmp/used"
grep -E '^[A-Z0-9_]+_VERSION=' "$root/tools/versions.env" \
  | cut -d= -f1 | grep -vE '^(NASM_VERSION|BAZEL_VERSION)$' \
  | sort -u >"$tmp/pinned"
while IFS= read -r var; do
  [[ -n $var ]] || continue
  if ! grep -q "^${var}=" "$root/tools/versions.env"; then
    fail "inventory references undefined version variable: $var"
  fi
done <"$tmp/used"
if ! diff -u "$tmp/pinned" "$tmp/used" >"$tmp/version.diff"; then
  fail "tools/versions.env and the inventory disagree about pinned versions"
  sed -n '1,60p' "$tmp/version.diff" >&2
fi

# Every shipped executable must be in the inventory, and the inventory must
# not claim an executable that is not shipped.
awk -F'\t' '!/^#/ && NF >= 5 { print $5 }' "$inventory" \
  | tr ';' '\n' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' \
  | sed '/^$/d' | sort -u >"$tmp/inventory_tools"
grep -v '^#' "$bundle_tools" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' \
  | sed '/^$/d' | sort -u >"$tmp/bundle_tools"
echo mins >>"$tmp/bundle_tools"
sort -u -o "$tmp/bundle_tools" "$tmp/bundle_tools"
if ! diff -u "$tmp/bundle_tools" "$tmp/inventory_tools" >"$tmp/tools.diff"; then
  fail "inventory link sets do not match tools/bundle-tools.txt"
  sed -n '1,60p' "$tmp/tools.diff" >&2
fi

# ---- 2. license texts ------------------------------------------------------
for f in \
  MIT.txt BSD-2-Clause.txt BSD-3-Clause.txt BSD-2-Clause-Patent.txt \
  BSD-3-Clause-Clear.txt Apache-2.0.txt LGPL-2.1-or-later.txt \
  LGPL-3.0-or-later.txt GPL-3.0.txt Zlib.txt Libpng-2.0.txt IJG.txt \
  LicenseRef-AOM-Patent-License-1.0.txt LicenseRef-FFTPACK.txt \
  Armadillo-NOTICE.txt; do
  need_file "$root/LICENSES/$f"
done
# The IJG and libjpeg-turbo notices are only correct if the documentation
# carries the required acknowledgement.
grep -q 'Independent JPEG Group' "$root/THIRD_PARTY_NOTICES.md" \
  || fail "THIRD_PARTY_NOTICES.md is missing the IJG acknowledgement"
grep -q 'Armadillo-NOTICE.txt' "$root/THIRD_PARTY_NOTICES.md" \
  || fail "THIRD_PARTY_NOTICES.md is missing Armadillo's NOTICE.txt"

# ---- 3. FFmpeg configuration and packaging wiring -------------------------
if grep -nE -- '--enable-(gpl|nonfree|version3)' "$root/tools/build.sh"; then
  fail "tools/build.sh enables a GPL, non-free, or version-3 FFmpeg feature"
fi
for want in \
  --disable-autodetect --enable-zlib --enable-libvmaf --enable-libsvtav1 \
  --enable-libaom --enable-libjxl --enable-libwebp --enable-libopus; do
  grep -q -- "$want" "$root/tools/build.sh" \
    || fail "tools/build.sh is missing $want (the notices describe it)"
done
grep -q 'bundle-tools.txt' "$root/tools/package-assets.sh" \
  || fail "tools/package-assets.sh does not use tools/bundle-tools.txt as its manifest"
grep -q 'LICENSES' "$root/tools/package-release.sh" \
  || fail "tools/package-release.sh does not ship LICENSES/ in the archives"
grep -q 'tools/check-licenses.sh' "$root/.github/workflows/release.yml" \
  || fail "the release workflow does not run tools/check-licenses.sh"
grep -q 'tools/collect-sources.sh' "$root/.github/workflows/release.yml" \
  || fail "the release workflow does not run tools/collect-sources.sh"
grep -q '^licenses:' "$root/Makefile" \
  || fail "the Makefile has no licenses target"
grep -q 'tools/check-licenses.sh' "$root/Makefile" \
  || fail "make check does not run tools/check-licenses.sh"

# ---- 4. built toolchain ----------------------------------------------------
if [[ $built == 1 ]]; then
  while IFS= read -r t; do
    t=${t%$'\r'}
    case "$t" in ''|'#'*) continue ;; esac
    f="$PREFIX/bin/$t$BIN_EXT"
    need_tool "$f"
    if [[ -f $f ]]; then
      audit_binary "$f" || fail "portability audit failed for $t"
    fi
  done <"$bundle_tools"

  if [[ -x $PREFIX/bin/ffmpeg$BIN_EXT ]]; then
    audit_ffmpeg_license "$PREFIX/bin/ffmpeg$BIN_EXT" \
      || fail "the built FFmpeg is not the LGPL-only configuration described in the notices"
  fi
  need_file "$PREFIX/share/libsvm_nu_svr_model.txt"

  # Relink materials for the statically linked LGPL libraries. The archive
  # builder uses these exact paths, so a missing object here means the release
  # would silently ship an incomplete relink archive.
  need_file "$PREFIX/lib/libheif.a"
  need_file "$PREFIX/lib/libde265.a"
  need_file "$BUILD/heif/build/heifio/libheifio.a"
  need_file "$BUILD/heif/build/examples/CMakeFiles/heif-dec.dir/heif_dec.cc.o"
  need_file "$BUILD/heif/build/examples/CMakeFiles/heif-dec.dir/common.cc.o"
  need_file "$BUILD/heif/build/examples/CMakeFiles/heif-info.dir/heif_info.cc.o"
  need_file "$BUILD/heif/build/examples/CMakeFiles/heif-info.dir/common.cc.o"
fi

if [[ $errors -ne 0 ]]; then
  echo "licenses: $errors check(s) failed" >&2
  exit 1
fi
echo "licenses: all checks passed"
