#!/usr/bin/env bash
# Drop the optional visqol binary from the prefix when it is not portable.
#
# On CI ViSQOL is built outside tools/build.sh (best effort), so it misses
# build.sh's portability audit. This reruns the same check for visqol only;
# keep the allowlists in sync with audit_binary in tools/build.sh. A
# non-portable visqol is dropped rather than shipped: mins then asks for
# VISQOL_BIN at runtime instead of failing to start on a clean machine.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
PREFIX=${PREFIX:-$root/tools/.build/prefix}

os=""
case $(uname -s) in
  Linux) os=Linux ;;
  Darwin) os=Darwin ;;
  MINGW*|MSYS*|CYGWIN*) os=MINGW ;;
esac
if [[ -z $os ]]; then
  echo "audit-visqol: unknown OS $(uname -s); skipping" >&2
  exit 0
fi

ext=""
if [[ $os == MINGW ]]; then ext=.exe; fi
bin="$PREFIX/bin/visqol$ext"
if [[ ! -f $bin ]]; then
  echo "audit-visqol: no visqol binary; nothing to audit" >&2
  exit 0
fi

bad=""
case $os in
  Linux)
    # Same rule as audit_binary in tools/build.sh: only the loader and the
    # minimal runtime every distro ships may stay dynamic.
    lddout=$(ldd "$bin" 2>&1 || true)
    if ! grep -qE 'not a dynamic executable|statically linked' <<<"$lddout"; then
      bad=$( { grep -E '=>' <<<"$lddout" \
          | sed -E 's/.*=>[[:space:]]*([^[:space:]]+).*/\1/';
        grep -E '^[[:space:]]*/' <<<"$lddout" | awk '{print $1}'; } \
        | grep -vE '^/(lib|lib64|usr/lib|usr/lib64)/([^/]+/)?(ld-linux[^/]*|libc\.so\.[0-9]+|libm\.so\.[0-9]+|libstdc\+\+\.so\.[0-9]+|libgcc_s\.so\.[0-9]+|libpthread\.so\.[0-9]+|libdl\.so\.[0-9]+|librt\.so\.[0-9]+|libatomic\.so\.[0-9]+)$' || true)
    fi
    ;;
  Darwin)
    bad=$(otool -L "$bin" | tail -n +2 | awk '{print $1}' \
      | grep -vE '^(/usr/lib/|/System/Library/)' || true)
    ;;
  MINGW)
    bad=$(objdump -p "$bin" | awk '/DLL Name:/{print $3}' \
      | grep -viE '^(KERNEL32|USER32|GDI32|ADVAPI32|SHELL32|ole32|OLEAUT32|WS2_32|WSOCK32|IPHLPAPI|bcrypt|crypt32|secur32|COMCTL32|PSAPI|msvcrt|ucrtbase|ntdll|dbghelp|DWMAPI|MF|MFPlat|MFReadWrite)\.dll$' || true)
    ;;
esac

if [[ -n $bad ]]; then
  printf 'audit: %s has non-system shared dependencies:\n%s\n' "$bin" "$bad" >&2
  echo "audit: dropping optional visqol from the bundle" >&2
  rm -f "$bin"
fi
