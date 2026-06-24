#!/bin/sh
set -eu

VERSION="${VERSION:-0.3.1}"
REPOSITORY="${REPOSITORY:-Xsir0/codex-meter}"
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
OUT="${1:-$ROOT/release/codex-meter.rb}"
RELEASE_DIR=$(dirname "$OUT")

hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

archive() {
  printf '%s/codex-meter-v%s-%s-%s.tar.gz' "$RELEASE_DIR" "$VERSION" "$1" "$2"
}

DARWIN_ARM64=$(archive darwin arm64)
DARWIN_AMD64=$(archive darwin amd64)
LINUX_ARM64=$(archive linux arm64)
LINUX_AMD64=$(archive linux amd64)

for file in "$DARWIN_ARM64" "$DARWIN_AMD64" "$LINUX_ARM64" "$LINUX_AMD64"; do
  [ -f "$file" ] || { echo "missing release archive: $file" >&2; exit 1; }
done

mkdir -p "$(dirname "$OUT")"
cat > "$OUT" <<EOF_FORMULA
class CodexMeter < Formula
  desc "View ChatGPT Codex usage from the terminal"
  homepage "https://github.com/$REPOSITORY"
  version "$VERSION"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/$REPOSITORY/releases/download/v$VERSION/codex-meter-v$VERSION-darwin-arm64.tar.gz"
      sha256 "$(hash_file "$DARWIN_ARM64")"
    else
      url "https://github.com/$REPOSITORY/releases/download/v$VERSION/codex-meter-v$VERSION-darwin-amd64.tar.gz"
      sha256 "$(hash_file "$DARWIN_AMD64")"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/$REPOSITORY/releases/download/v$VERSION/codex-meter-v$VERSION-linux-arm64.tar.gz"
      sha256 "$(hash_file "$LINUX_ARM64")"
    else
      url "https://github.com/$REPOSITORY/releases/download/v$VERSION/codex-meter-v$VERSION-linux-amd64.tar.gz"
      sha256 "$(hash_file "$LINUX_AMD64")"
    end
  end

  def install
    bin.install "codex-meter"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/codex-meter --version")
  end
end
EOF_FORMULA

printf 'Homebrew formula written to %s\n' "$OUT"
