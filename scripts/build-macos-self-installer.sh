#!/bin/sh
set -eu

VERSION="${VERSION:-0.3.1}"
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DIST="$ROOT/dist"
OUTPUT="${1:-$ROOT/release/codex-meter-v$VERSION-macos-installer.command}"
ARM64="$DIST/codex-meter-darwin-arm64"
AMD64="$DIST/codex-meter-darwin-amd64"

[ -f "$ARM64" ] || { echo "missing $ARM64" >&2; exit 1; }
[ -f "$AMD64" ] || { echo "missing $AMD64" >&2; exit 1; }
mkdir -p "$(dirname "$OUTPUT")"

hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

encode_file() {
  gzip -9c "$1" | base64
}

ARM64_SHA=$(hash_file "$ARM64")
AMD64_SHA=$(hash_file "$AMD64")

cat > "$OUTPUT" <<'EOF_HEADER'
#!/bin/bash
set -euo pipefail

decode_base64() {
  if base64 --decode </dev/null >/dev/null 2>&1; then
    base64 --decode
  else
    base64 -D
  fi
}

extract_arm64() {
  decode_base64 <<'CODEX_METER_ARM64' | gzip -dc
EOF_HEADER
encode_file "$ARM64" >> "$OUTPUT"
cat >> "$OUTPUT" <<'EOF_MIDDLE'
CODEX_METER_ARM64
}

extract_amd64() {
  decode_base64 <<'CODEX_METER_AMD64' | gzip -dc
EOF_MIDDLE
encode_file "$AMD64" >> "$OUTPUT"
cat >> "$OUTPUT" <<'EOF_AFTER_PAYLOAD'
CODEX_METER_AMD64
}
EOF_AFTER_PAYLOAD

cat >> "$OUTPUT" <<EOF_META

PROGRAM="codex-meter"
VERSION="$VERSION"
ARM64_SHA="$ARM64_SHA"
AMD64_SHA="$AMD64_SHA"
EOF_META

cat >> "$OUTPUT" <<'EOF_MAIN'
INSTALL_DIR="${CODEX_METER_INSTALL_DIR:-/usr/local/bin}"
UNINSTALL=0

usage() {
  cat <<'USAGE'
Install codex-meter on macOS.

Usage:
  Install Codex Meter.command [--install-dir DIR]
  Install Codex Meter.command --uninstall [--install-dir DIR]

Options:
  --install-dir DIR  Installation directory (default: /usr/local/bin).
  --uninstall        Remove codex-meter.
  -h, --help         Show this help.
USAGE
}

fail() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --install-dir)
      [[ $# -ge 2 ]] || fail "--install-dir requires a value"
      INSTALL_DIR=$2
      shift 2
      ;;
    --uninstall)
      UNINSTALL=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown option: $1"
      ;;
  esac
done

if [[ "$INSTALL_DIR" == ~/* ]]; then
  INSTALL_DIR="$HOME/${INSTALL_DIR#~/}"
fi
TARGET="$INSTALL_DIR/$PROGRAM"

run_privileged() {
  if [[ -w "$INSTALL_DIR" ]] || { [[ ! -e "$INSTALL_DIR" ]] && [[ -w "$(dirname "$INSTALL_DIR")" ]]; }; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    fail "$INSTALL_DIR is not writable; use --install-dir \"$HOME/.local/bin\""
  fi
}

if [[ $UNINSTALL -eq 1 ]]; then
  if [[ ! -e "$TARGET" ]]; then
    printf '%s is not installed at %s.\n' "$PROGRAM" "$TARGET"
    exit 0
  fi
  run_privileged rm -f "$TARGET"
  printf 'Removed %s.\n' "$TARGET"
  exit 0
fi

[[ "$(uname -s)" == "Darwin" ]] || fail "this installer supports macOS only"

TMP_DIR=$(mktemp -d -t codex-meter)
trap 'rm -rf "$TMP_DIR"' EXIT HUP INT TERM
SOURCE="$TMP_DIR/codex-meter"
EXPECTED_SHA=""

case "$(uname -m)" in
  arm64)
    EXPECTED_SHA="$ARM64_SHA"
    extract_arm64 > "$SOURCE"
    PLATFORM="Apple Silicon"
    ;;
  x86_64)
    EXPECTED_SHA="$AMD64_SHA"
    extract_amd64 > "$SOURCE"
    PLATFORM="Intel"
    ;;
  *)
    fail "unsupported Mac architecture: $(uname -m)"
    ;;
esac

ACTUAL_SHA=$(shasum -a 256 "$SOURCE" | awk '{print $1}')
[[ "$ACTUAL_SHA" == "$EXPECTED_SHA" ]] || fail "embedded binary checksum verification failed"
chmod 0755 "$SOURCE"

printf 'Installing %s %s for %s...\n' "$PROGRAM" "$VERSION" "$PLATFORM"
run_privileged mkdir -p "$INSTALL_DIR"
run_privileged install -m 0755 "$SOURCE" "$TARGET"

printf '\nInstalled %s to %s\n' "$PROGRAM" "$TARGET"
"$TARGET" --version
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    printf '\n%s is not currently in PATH. Add this line to your shell profile:\n' "$INSTALL_DIR"
    printf '  export PATH="%s:$PATH"\n' "$INSTALL_DIR"
    ;;
esac
printf '\nRun:\n  codex-meter\n'
EOF_MAIN

chmod 0755 "$OUTPUT"
printf 'standalone macOS installer written to %s\n' "$OUTPUT"
