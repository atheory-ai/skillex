#!/bin/sh
set -eu

export LC_ALL=C

REPO="atheory-ai/skillex"
INSTALL_DIR="${SKILLEX_INSTALL_DIR:-"$HOME/.local/bin"}"
VERSION="${SKILLEX_VERSION:-latest}"
DRY_RUN=0

usage() {
  cat <<'EOF'
Install skillex.

Usage:
  curl -fsSL https://raw.githubusercontent.com/atheory-ai/skillex/main/install.sh | sh

Environment:
  SKILLEX_VERSION       Version to install, e.g. 0.6.4 or v0.6.4 (default: latest)
  SKILLEX_INSTALL_DIR   Directory to install into (default: ~/.local/bin)
  SKILLEX_BASE_URL      Override release asset base URL (used by tests)

Options:
  --dry-run             Print what would be installed without downloading
  --help                Show this help
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --dry-run)
      DRY_RUN=1
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "skillex installer: unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
  shift
done

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "skillex installer: required command not found: $1" >&2
    exit 1
  fi
}

detect_platform() {
  os="$(uname -s 2>/dev/null || true)"
  arch="$(uname -m 2>/dev/null || true)"

  case "$os" in
    Darwin)
      platform_os="darwin"
      ;;
    Linux)
      platform_os="linux"
      ;;
    MINGW*|MSYS*|CYGWIN*)
      platform_os="win32"
      ;;
    *)
      echo "skillex installer: unsupported operating system: $os" >&2
      exit 1
      ;;
  esac

  case "$arch" in
    x86_64|amd64)
      platform_arch="x64"
      ;;
    arm64|aarch64)
      platform_arch="arm64"
      ;;
    *)
      echo "skillex installer: unsupported architecture: $arch" >&2
      exit 1
      ;;
  esac

  if [ "$platform_os" = "win32" ] && [ "$platform_arch" != "x64" ]; then
    echo "skillex installer: unsupported Windows architecture: $arch" >&2
    exit 1
  fi

  PLATFORM="$platform_os-$platform_arch"
}

download() {
  url="$1"
  out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$out"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$url" -O "$out"
  else
    echo "skillex installer: curl or wget is required to download release assets" >&2
    exit 1
  fi
}

checksum_file() {
  file="$1"
  checksums="$2"
  expected="$(grep "  $(basename "$file")$" "$checksums" | awk '{print $1}')"
  if [ -z "$expected" ]; then
    echo "skillex installer: checksum not found for $(basename "$file")" >&2
    exit 1
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$file" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$file" | awk '{print $1}')"
  else
    echo "skillex installer: sha256sum or shasum is required to verify checksums" >&2
    exit 1
  fi

  if [ "$actual" != "$expected" ]; then
    echo "skillex installer: checksum verification failed for $(basename "$file")" >&2
    exit 1
  fi
}

detect_platform

case "$PLATFORM" in
  darwin-arm64|darwin-x64|linux-arm64|linux-x64)
    ASSET="skillex-$PLATFORM.tar.gz"
    BINARY_NAME="skillex"
    ;;
  win32-x64)
    ASSET="skillex-$PLATFORM.zip"
    BINARY_NAME="skillex.exe"
    ;;
  *)
    echo "skillex installer: unsupported platform: $PLATFORM" >&2
    exit 1
    ;;
esac

if [ "${SKILLEX_BASE_URL:-}" ]; then
  BASE_URL="${SKILLEX_BASE_URL%/}"
elif [ "$VERSION" = "latest" ]; then
  BASE_URL="https://github.com/$REPO/releases/latest/download"
else
  case "$VERSION" in
    v*) TAG="$VERSION" ;;
    *) TAG="v$VERSION" ;;
  esac
  BASE_URL="https://github.com/$REPO/releases/download/$TAG"
fi

echo "Installing skillex for $PLATFORM"
echo "Asset: $ASSET"
echo "Install dir: $INSTALL_DIR"

if [ "$DRY_RUN" = "1" ]; then
  echo "Dry run: would download $BASE_URL/$ASSET"
  exit 0
fi

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmpdir"
}
trap cleanup EXIT INT TERM

archive="$tmpdir/$ASSET"
checksums="$tmpdir/checksums.txt"
download "$BASE_URL/$ASSET" "$archive"
download "$BASE_URL/checksums.txt" "$checksums"
checksum_file "$archive" "$checksums"

case "$ASSET" in
  *.tar.gz)
    need_cmd tar
    tar -xzf "$archive" -C "$tmpdir"
    ;;
  *.zip)
    need_cmd unzip
    unzip -q "$archive" -d "$tmpdir"
    ;;
esac

if [ ! -f "$tmpdir/$BINARY_NAME" ]; then
  echo "skillex installer: expected binary not found in archive: $BINARY_NAME" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"
install_path="$INSTALL_DIR/$BINARY_NAME"
cp "$tmpdir/$BINARY_NAME" "$install_path"
chmod 755 "$install_path"

echo "Installed skillex to $install_path"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    echo ""
    echo "Note: $INSTALL_DIR is not on PATH."
    echo "Add it to your shell profile, for example:"
    echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
    ;;
esac
