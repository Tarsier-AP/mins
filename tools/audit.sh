#!/usr/bin/env bash
# Shared build/packaging audits. Sourced by tools/build.sh and
# tools/check-licenses.sh; not executable on its own.
#
# Requires OS and BIN_EXT (set by the caller) and the usual MSYS/Darwin tools.

# audit_binary refuses to package an executable that still depends on a
# non-system shared library. Release archives contain only executables, so a
# stray Homebrew/MSYS2 dylib or DLL would make the bundle fail on a clean
# machine.
audit_binary() { # path
  local f=$1 bad=''
  [[ -f $f ]] || { echo "audit: missing $f" >&2; return 1; }
  case "$OS" in
    Linux)
      local lddout
      lddout=$(ldd "$f" 2>&1 || true)
      if grep -qE 'not a dynamic executable|statically linked' <<<"$lddout"; then
        return 0
      fi
      # Keep only the loader and the minimal runtime that every distro
      # ships. libpng/libjpeg/libz/Homebrew libraries must be static.
      bad=$( { grep -E '=>' <<<"$lddout" \
          | sed -E 's/.*=>[[:space:]]*([^[:space:]]+).*/\1/';
        grep -E '^[[:space:]]*/' <<<"$lddout" | awk '{print $1}'; } \
        | grep -vE '^/(lib|lib64|usr/lib|usr/lib64)/([^/]+/)?(ld-linux[^/]*|libc\.so\.[0-9]+|libm\.so\.[0-9]+|libstdc\+\+\.so\.[0-9]+|libgcc_s\.so\.[0-9]+|libpthread\.so\.[0-9]+|libdl\.so\.[0-9]+|librt\.so\.[0-9]+|libatomic\.so\.[0-9]+)$' || true)
      ;;
    Darwin)
      bad=$(otool -L "$f" | tail -n +2 | awk '{print $1}' \
        | grep -vE '^(/usr/lib/|/System/Library/)' || true)
      ;;
    MINGW*|MSYS*|CYGWIN*)
      bad=$(objdump -p "$f" | awk '/DLL Name:/{print $3}' \
        | grep -viE '^(KERNEL32|USER32|GDI32|ADVAPI32|SHELL32|SHLWAPI|ole32|OLEAUT32|WS2_32|WSOCK32|IPHLPAPI|bcrypt|crypt32|secur32|COMCTL32|PSAPI|msvcrt|ucrtbase|ntdll|dbghelp|DWMAPI|MF|MFPlat|MFReadWrite|AVICAP32)\.dll$' || true)
      ;;
  esac
  if [[ -n $bad ]]; then
    printf 'audit: %s has non-system shared dependencies:\n%s\n' "$f" "$bad" >&2
    return 1
  fi
}

# audit_ffmpeg_license verifies that the linked FFmpeg is the LGPL-only build
# described in THIRD_PARTY_NOTICES.md. It is deliberately strict: a release
# archive whose notice says LGPL must not contain a GPL, non-free, or
# version-3-upgraded FFmpeg.
audit_ffmpeg_license() { # path
  local f=$1 conf lic want
  [[ -x $f ]] || { echo "audit: missing $f" >&2; return 1; }
  conf=$("$f" -hide_banner -buildconf 2>/dev/null) || {
    echo "audit: $f -buildconf failed" >&2
    return 1
  }
  if grep -qE -- '--enable-(gpl|nonfree|version3)' <<<"$conf"; then
    echo "audit: $f has a non-LGPL-only configuration:" >&2
    grep -E -- '--enable-(gpl|nonfree|version3)' <<<"$conf" >&2
    return 1
  fi
  for want in \
    --enable-libvmaf --enable-libsvtav1 --enable-libaom --enable-libjxl \
    --enable-libwebp --enable-libopus --enable-zlib; do
    if ! grep -q -- "$want" <<<"$conf"; then
      echo "audit: $f configuration is missing $want (notices would be wrong)" >&2
      return 1
    fi
  done
  # FFmpeg wraps the banner across lines, and the wrapping changes between
  # releases, so flatten it before matching phrases. Windows writes the banner
  # with CRLF; removing only the newline would leave a CR in the middle of the
  # phrase and make the audit fail on an otherwise correct LGPL build.
  lic=$("$f" -hide_banner -L 2>/dev/null | tr -d '\r' | tr '\n' ' ' || true)
  if ! grep -q 'GNU Lesser General Public License' <<<"$lic"; then
    echo "audit: $f does not report the GNU Lesser General Public License" >&2
    return 1
  fi
  if grep -q 'under the terms of the GNU General Public License' <<<"$lic"; then
    echo "audit: $f reports the GNU General Public License" >&2
    return 1
  fi
}
