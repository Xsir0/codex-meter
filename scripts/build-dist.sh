#!/bin/sh
set -eu

VERSION="${VERSION:-0.3.0}"
COMMIT="${COMMIT:-none}"
DATE="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DIST="$ROOT/dist"
LDFLAGS="-s -w -X main.version=$VERSION -X main.commit=$COMMIT -X main.date=$DATE"

rm -rf "$DIST"
mkdir -p "$DIST"

build() {
  os=$1
  arch=$2
  extension=$3
  output="$DIST/codex-meter-$os-$arch$extension"
  printf 'building %s/%s\n' "$os" "$arch"
  (
    cd "$ROOT"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
      go build -trimpath -ldflags "$LDFLAGS" -o "$output" ./cmd/codex-meter
  )
}

build darwin arm64 ""
build darwin amd64 ""
build linux amd64 ""
build linux arm64 ""
build windows amd64 ".exe"

cd "$DIST"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum codex-meter-* > SHA256SUMS
else
  shasum -a 256 codex-meter-* > SHA256SUMS
fi
printf 'artifacts written to %s\n' "$DIST"
