#!/bin/sh
set -eu

VERSION="${VERSION:-0.3.0}"
REPOSITORY="${REPOSITORY:-Xsir0/codex-meter}"
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DIST="$ROOT/dist"
OUT="${OUT:-$ROOT/release}"

write_installer() {
  sed "s|^DEFAULT_REPOSITORY=.*|DEFAULT_REPOSITORY=\"$REPOSITORY\"|" "$ROOT/install.sh" > "$1"
}

rm -rf "$OUT"
mkdir -p "$OUT"

package_unix() {
  os=$1
  arch=$2
  name="codex-meter-v$VERSION-$os-$arch"
  stage=$(mktemp -d)
  mkdir -p "$stage/$name"
  cp "$DIST/codex-meter-$os-$arch" "$stage/$name/codex-meter"
  chmod 0755 "$stage/$name/codex-meter"
  cp "$ROOT/README.md" "$ROOT/README.zh-CN.md" "$ROOT/LICENSE" "$stage/$name/"
  cp -R "$ROOT/assets" "$stage/$name/"
  cp -R "$ROOT/examples" "$stage/$name/"
  tar -C "$stage" -czf "$OUT/$name.tar.gz" "$name"
  rm -rf "$stage"
}

package_windows() {
  name="codex-meter-v$VERSION-windows-amd64"
  stage=$(mktemp -d)
  mkdir -p "$stage/$name"
  cp "$DIST/codex-meter-windows-amd64.exe" "$stage/$name/codex-meter.exe"
  cp "$ROOT/README.md" "$ROOT/README.zh-CN.md" "$ROOT/LICENSE" "$stage/$name/"
  cp -R "$ROOT/assets" "$stage/$name/"
  cp -R "$ROOT/examples" "$stage/$name/"
  (cd "$stage" && zip -qr "$OUT/$name.zip" "$name")
  rm -rf "$stage"
}

package_unix darwin arm64
package_unix darwin amd64
package_unix linux amd64
package_unix linux arm64
package_windows

mac_name="codex-meter-v$VERSION-macos-installer"
mac_stage=$(mktemp -d)
mkdir -p "$mac_stage/$mac_name/bin"
cp "$ROOT/scripts/install-macos.command" "$mac_stage/$mac_name/Install Codex Meter.command"
chmod 0755 "$mac_stage/$mac_name/Install Codex Meter.command"
cp "$ROOT/scripts/macos-installer-readme.txt" "$mac_stage/$mac_name/README.txt"
cp "$DIST/codex-meter-darwin-arm64" "$mac_stage/$mac_name/bin/"
cp "$DIST/codex-meter-darwin-amd64" "$mac_stage/$mac_name/bin/"
chmod 0755 "$mac_stage/$mac_name/bin/"*
(cd "$mac_stage" && zip -qry "$OUT/$mac_name.zip" "$mac_name")
rm -rf "$mac_stage"

VERSION="$VERSION" "$ROOT/scripts/build-macos-self-installer.sh" \
  "$OUT/codex-meter-v$VERSION-macos-installer.command"

source_name="codex-meter-v$VERSION-source"
source_stage=$(mktemp -d)
mkdir -p "$source_stage/$source_name"
(
  cd "$ROOT"
  find . -type f \
    ! -path './dist/*' \
    ! -path './release/*' \
    ! -path './bin/*' \
    ! -path './.git/*' \
    -print
) | while IFS= read -r source; do
  target="$source_stage/$source_name/${source#./}"
  mkdir -p "$(dirname "$target")"
  cp "$ROOT/${source#./}" "$target"
done
(cd "$source_stage" && zip -qr "$OUT/$source_name.zip" "$source_name")
rm -rf "$source_stage"

all_name="codex-meter-v$VERSION-all-platforms"
all_stage=$(mktemp -d)
mkdir -p "$all_stage/$all_name/bin"
cp "$DIST"/codex-meter-* "$all_stage/$all_name/bin/"
cp "$ROOT/README.md" "$ROOT/README.zh-CN.md" "$ROOT/LICENSE" "$all_stage/$all_name/"
cp -R "$ROOT/assets" "$all_stage/$all_name/"
write_installer "$all_stage/$all_name/install.sh"
chmod 0755 "$all_stage/$all_name/install.sh"
cp -R "$ROOT/examples" "$all_stage/$all_name/"
(cd "$all_stage" && zip -qr "$OUT/$all_name.zip" "$all_name")
rm -rf "$all_stage"

write_installer "$OUT/install.sh"
chmod 0755 "$OUT/install.sh"

cd "$OUT"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum ./* > SHA256SUMS
else
  shasum -a 256 ./* > SHA256SUMS
fi
printf 'release archives written to %s\n' "$OUT"
