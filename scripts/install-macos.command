#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

pause_before_close() {
  if [[ -t 0 ]]; then
    echo
    read -r -p "Press Return to close this window..." _
  fi
}

on_error() {
  status=$?
  set +e
  echo
  echo "Installation failed. Review the message above and try again." >&2
  pause_before_close
  exit "$status"
}
trap on_error ERR

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "This installer is for macOS only." >&2
  exit 1
fi

case "$(uname -m)" in
  arm64)
    SOURCE="$SCRIPT_DIR/bin/codex-meter-darwin-arm64"
    PLATFORM="Apple Silicon"
    ;;
  x86_64)
    SOURCE="$SCRIPT_DIR/bin/codex-meter-darwin-amd64"
    PLATFORM="Intel"
    ;;
  *)
    echo "Unsupported Mac architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

if [[ ! -f "$SOURCE" ]]; then
  echo "The bundled $PLATFORM binary is missing." >&2
  exit 1
fi

TARGET_DIR="/usr/local/bin"
TARGET="$TARGET_DIR/codex-meter"

echo "Installing codex-meter for $PLATFORM..."
sudo mkdir -p "$TARGET_DIR"
sudo install -m 0755 "$SOURCE" "$TARGET"
sudo xattr -d com.apple.quarantine "$TARGET" 2>/dev/null || true

trap - ERR
echo
echo "Installation complete."
"$TARGET" --version
echo
echo "Open a new Terminal window and run:"
echo "  codex-meter"
pause_before_close
