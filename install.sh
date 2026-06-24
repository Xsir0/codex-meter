#!/bin/sh
set -eu

PROGRAM="codex-meter"
# End users may override this with CODEX_METER_REPOSITORY=owner/repository.
DEFAULT_REPOSITORY="Xsir0/codex-meter"
REPOSITORY="${CODEX_METER_REPOSITORY:-$DEFAULT_REPOSITORY}"
INSTALL_DIR="${CODEX_METER_INSTALL_DIR:-/usr/local/bin}"
REQUESTED_VERSION="latest"
UNINSTALL=0

usage() {
  cat <<'USAGE'
Install codex-meter from a bundled release or GitHub Releases.

Usage:
  install.sh [--version VERSION] [--install-dir DIR]
  install.sh --uninstall [--install-dir DIR]

Options:
  --version VERSION   Install a release such as 0.3.0 or v0.3.0.
  --install-dir DIR   Installation directory (default: /usr/local/bin).
  --uninstall         Remove codex-meter from the installation directory.
  -h, --help          Show this help.

Environment:
  CODEX_METER_REPOSITORY  GitHub repository in owner/name form.
  CODEX_METER_INSTALL_DIR Installation directory.
USAGE
}

fail() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || fail "--version requires a value"
      REQUESTED_VERSION=$2
      shift 2
      ;;
    --install-dir)
      [ "$#" -ge 2 ] || fail "--install-dir requires a value"
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

case "$INSTALL_DIR" in
  ~/*) INSTALL_DIR="$HOME/${INSTALL_DIR#~/}" ;;
esac
TARGET="$INSTALL_DIR/$PROGRAM"

run_privileged() {
  if [ -w "$INSTALL_DIR" ] || { [ ! -e "$INSTALL_DIR" ] && [ -w "$(dirname "$INSTALL_DIR")" ]; }; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    fail "$INSTALL_DIR is not writable and sudo is unavailable; use --install-dir \"$HOME/.local/bin\""
  fi
}

if [ "$UNINSTALL" -eq 1 ]; then
  if [ ! -e "$TARGET" ]; then
    printf '%s is not installed at %s.\n' "$PROGRAM" "$TARGET"
    exit 0
  fi
  run_privileged rm -f "$TARGET"
  printf 'Removed %s.\n' "$TARGET"
  exit 0
fi

OS=$(uname -s 2>/dev/null || true)
case "$OS" in
  Darwin) OS=darwin ;;
  Linux) OS=linux ;;
  *) fail "unsupported operating system: ${OS:-unknown}" ;;
esac

ARCH=$(uname -m 2>/dev/null || true)
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) fail "unsupported architecture: ${ARCH:-unknown}" ;;
esac

TMP_DIR=$(mktemp -d 2>/dev/null || mktemp -d -t codex-meter)
trap 'rm -rf "$TMP_DIR"' EXIT HUP INT TERM

SCRIPT_DIR=""
case "$0" in
  */*) SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" 2>/dev/null && pwd || true) ;;
esac
BUNDLED_BINARY="${SCRIPT_DIR:+$SCRIPT_DIR/bin/codex-meter-$OS-$ARCH}"
SOURCE_BINARY=""

if [ -n "$BUNDLED_BINARY" ] && [ -f "$BUNDLED_BINARY" ]; then
  SOURCE_BINARY=$BUNDLED_BINARY
  printf 'Using the bundled %s/%s binary.\n' "$OS" "$ARCH"
else
  case "$REPOSITORY" in
    */*) ;;
    *) fail "CODEX_METER_REPOSITORY must be in owner/repository form" ;;
  esac

  if command -v curl >/dev/null 2>&1; then
    download() { curl -fL --retry 3 --connect-timeout 15 --silent --show-error "$1" -o "$2"; }
  elif command -v wget >/dev/null 2>&1; then
    download() { wget -q --tries=3 --timeout=15 -O "$2" "$1"; }
  else
    fail "curl or wget is required"
  fi

  if [ "$REQUESTED_VERSION" = "latest" ]; then
    API_URL="https://api.github.com/repos/$REPOSITORY/releases/latest"
    download "$API_URL" "$TMP_DIR/release.json"
    TAG=$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$TMP_DIR/release.json" | head -n 1)
    [ -n "$TAG" ] || fail "could not determine the latest release tag"
  else
    TAG=$REQUESTED_VERSION
    case "$TAG" in v*) ;; *) TAG="v$TAG" ;; esac
  fi

  VERSION=${TAG#v}
  ARCHIVE="codex-meter-v$VERSION-$OS-$ARCH.tar.gz"
  BASE_URL="https://github.com/$REPOSITORY/releases/download/$TAG"
  printf 'Downloading %s %s for %s/%s...\n' "$PROGRAM" "$VERSION" "$OS" "$ARCH"
  download "$BASE_URL/$ARCHIVE" "$TMP_DIR/$ARCHIVE"
  download "$BASE_URL/SHA256SUMS" "$TMP_DIR/SHA256SUMS"

  EXPECTED=$(awk -v file="$ARCHIVE" '$2 == file || $2 == "./" file { print $1; exit }' "$TMP_DIR/SHA256SUMS")
  [ -n "$EXPECTED" ] || fail "the checksum file does not contain $ARCHIVE"
  if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL=$(sha256sum "$TMP_DIR/$ARCHIVE" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    ACTUAL=$(shasum -a 256 "$TMP_DIR/$ARCHIVE" | awk '{print $1}')
  else
    fail "sha256sum or shasum is required to verify the download"
  fi
  [ "$EXPECTED" = "$ACTUAL" ] || fail "checksum verification failed"

  tar -xzf "$TMP_DIR/$ARCHIVE" -C "$TMP_DIR"
  SOURCE_BINARY="$TMP_DIR/codex-meter-v$VERSION-$OS-$ARCH/codex-meter"
  [ -f "$SOURCE_BINARY" ] || fail "the release archive did not contain codex-meter"
fi

run_privileged mkdir -p "$INSTALL_DIR"
run_privileged install -m 0755 "$SOURCE_BINARY" "$TARGET"

if [ "$OS" = "darwin" ] && command -v xattr >/dev/null 2>&1; then
  if [ -w "$TARGET" ]; then
    xattr -d com.apple.quarantine "$TARGET" 2>/dev/null || true
  elif command -v sudo >/dev/null 2>&1; then
    sudo xattr -d com.apple.quarantine "$TARGET" 2>/dev/null || true
  fi
fi

printf '\nInstalled %s to %s\n' "$PROGRAM" "$TARGET"
"$TARGET" --version

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    printf '\n%s is not currently in PATH. Add this line to your shell profile:\n' "$INSTALL_DIR"
    printf '  export PATH="%s:$PATH"\n' "$INSTALL_DIR"
    ;;
esac

printf '\nRun:\n  %s\n' "$PROGRAM"
