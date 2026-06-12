#!/usr/bin/env sh
set -eu

export LC_ALL=C

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"

require_file() {
  if [ ! -f "$1" ]; then
    echo "missing required file: $1" >&2
    exit 1
  fi
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

package_tar() {
  src="$1"
  out="$2"
  workdir="$DIST_DIR/release-tmp/$(basename "$out" .tar.gz)"
  rm -rf "$workdir"
  mkdir -p "$workdir"
  cp "$src" "$workdir/skillex"
  chmod 755 "$workdir/skillex"
  (cd "$workdir" && tar -czf "$DIST_DIR/$out" skillex)
}

package_zip() {
  src="$1"
  out="$2"
  workdir="$DIST_DIR/release-tmp/$(basename "$out" .zip)"
  rm -rf "$workdir"
  mkdir -p "$workdir"
  cp "$src" "$workdir/skillex.exe"
  (cd "$workdir" && zip -q "$DIST_DIR/$out" skillex.exe)
}

checksum() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$@"
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$@"
  else
    echo "sha256sum or shasum is required" >&2
    exit 1
  fi
}

mkdir -p "$DIST_DIR"
rm -rf "$DIST_DIR/release-tmp"

require_cmd tar
require_cmd zip

require_file "$DIST_DIR/skillex-darwin-arm64"
require_file "$DIST_DIR/skillex-darwin-x64"
require_file "$DIST_DIR/skillex-linux-arm64"
require_file "$DIST_DIR/skillex-linux-x64"
require_file "$DIST_DIR/skillex-win32-x64.exe"

package_tar "$DIST_DIR/skillex-darwin-arm64" "skillex-darwin-arm64.tar.gz"
package_tar "$DIST_DIR/skillex-darwin-x64" "skillex-darwin-x64.tar.gz"
package_tar "$DIST_DIR/skillex-linux-arm64" "skillex-linux-arm64.tar.gz"
package_tar "$DIST_DIR/skillex-linux-x64" "skillex-linux-x64.tar.gz"
package_zip "$DIST_DIR/skillex-win32-x64.exe" "skillex-win32-x64.zip"

(
  cd "$DIST_DIR"
  checksum \
    skillex-darwin-arm64.tar.gz \
    skillex-darwin-x64.tar.gz \
    skillex-linux-arm64.tar.gz \
    skillex-linux-x64.tar.gz \
    skillex-win32-x64.zip > checksums.txt
)

rm -rf "$DIST_DIR/release-tmp"
echo "Release assets written to $DIST_DIR"
