#!/bin/sh
# Derived from atheory-ai/release-template v0.2.0
# See .release-template-config for substitution values.
#
# Install skillex.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/atheory-ai/skillex/main/install.sh | sh
#
# Environment:
#   SKILLEX_VERSION         Version to install, e.g. 0.7.2 or v0.7.2 (default: latest)
#   SKILLEX_INSTALL_DIR     Directory to install into (default: ~/.local/bin)
#   SKILLEX_BASE_URL        Override release asset base URL (used by tests)
#   SKILLEX_REQUIRE_COSIGN  Set to 1 to fail if cosign is not installed (default: 0)
#
# Options:
#   --dry-run        Print what would be installed without downloading
#   --help           Show this help

set -eu
export LC_ALL=C

OWNER="atheory-ai"
REPO="skillex"
BIN_NAME="skillex"
INSTALL_DIR="${SKILLEX_INSTALL_DIR:-${INSTALL_DIR:-$HOME/.local/bin}}"
VERSION="${SKILLEX_VERSION:-latest}"
DRY_RUN=0

usage() {
  cat <<'USAGE_EOF'
Install skillex.

Usage:
  curl -fsSL https://raw.githubusercontent.com/atheory-ai/skillex/main/install.sh | sh

Environment:
  SKILLEX_VERSION         Version to install, e.g. 0.7.2 or v0.7.2 (default: latest)
  SKILLEX_INSTALL_DIR     Directory to install into (default: ~/.local/bin)
  SKILLEX_BASE_URL        Override release asset base URL (used by tests)
  SKILLEX_REQUIRE_COSIGN  Set to 1 to fail if cosign verification can't run

Options:
  --dry-run        Print what would be installed without downloading
  --help           Show this help
USAGE_EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "skillex installer: unknown option: $1" >&2; usage >&2; exit 1 ;;
  esac
  shift
done

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "skillex installer: required command not found: $1" >&2
    exit 1
  fi
}

detect_os() {
  case "$(uname -s 2>/dev/null || true)" in
    Darwin) echo "darwin" ;;
    Linux) echo "linux" ;;
    MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
    *) echo "skillex installer: unsupported OS: $(uname -s)" >&2; exit 1 ;;
  esac
}

detect_arch() {
  case "$(uname -m 2>/dev/null || true)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) echo "skillex installer: unsupported architecture: $(uname -m)" >&2; exit 1 ;;
  esac
}

download() {
  url="$1"; out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$out"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$url" -O "$out"
  else
    echo "skillex installer: curl or wget is required" >&2
    exit 1
  fi
}

latest_version() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSLI -o /dev/null -w "%{url_effective}" "https://github.com/$OWNER/$REPO/releases/latest" |
      sed 's#.*/tag/v##'
  elif command -v wget >/dev/null 2>&1; then
    wget -qO- "https://github.com/$OWNER/$REPO/releases/latest" |
      sed -n 's#.*href="/'"$OWNER"'/'"$REPO"'/releases/tag/v\([^"]*\)".*#\1#p' |
      head -n 1
  else
    echo "skillex installer: curl or wget is required" >&2
    exit 1
  fi
}

verify_checksum() {
  checksum_file="$1"; archive_file="$2"
  archive_name="$(basename "$archive_file")"
  expected="$(grep "  $archive_name\$" "$checksum_file" | awk '{print $1}' || true)"
  if [ -z "$expected" ]; then
    echo "skillex installer: checksum for $archive_name not found" >&2; exit 1
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$archive_file" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$archive_file" | awk '{print $1}')"
  else
    echo "skillex installer: sha256sum or shasum is required" >&2; exit 1
  fi
  if [ "$actual" != "$expected" ]; then
    echo "skillex installer: checksum mismatch for $archive_name" >&2; exit 1
  fi
}

# Cosign verification (skipped if cosign is not on PATH).
# To require: SKILLEX_REQUIRE_COSIGN=1 curl -fsSL .../install.sh | sh
# Install cosign: `brew install cosign` or release downloads from
# https://github.com/sigstore/cosign/releases
verify_cosign() {
  archive_file="$1"
  archive_name="$(basename "$archive_file")"

  if ! command -v cosign >/dev/null 2>&1; then
    if [ "${SKILLEX_REQUIRE_COSIGN:-0}" = "1" ]; then
      echo "skillex installer: cosign required but not installed" >&2
      exit 1
    fi
    return 0
  fi

  bundle_url="$base_url/${archive_name}.bundle"
  bundle="$tmp/${archive_name}.bundle"

  if ! download "$bundle_url" "$bundle" 2>/dev/null; then
    if [ "${SKILLEX_REQUIRE_COSIGN:-0}" = "1" ]; then
      echo "skillex installer: cosign bundle missing at $bundle_url — refusing install (SKILLEX_REQUIRE_COSIGN=1)" >&2
      exit 1
    fi
    echo "skillex installer: cosign bundle not found at $bundle_url" >&2
    echo "  (older releases predate cosign signing; skipping verification)" >&2
    return 0
  fi

  cert_identity_regex="^https://github.com/$OWNER/$REPO/.github/workflows/release(-full)?\\.yml@refs/tags/v.+\$"
  oidc_issuer="https://token.actions.githubusercontent.com"

  if ! cosign verify-blob \
        --bundle "$bundle" \
        --certificate-identity-regexp "$cert_identity_regex" \
        --certificate-oidc-issuer "$oidc_issuer" \
        "$archive_file" >/dev/null 2>&1; then
    echo "skillex installer: cosign verification FAILED for $archive_name" >&2
    echo "  Expected signer identity (regex): $cert_identity_regex" >&2
    echo "  Expected OIDC issuer:             $oidc_issuer" >&2
    exit 1
  fi

  echo "Cosign signature verified: $archive_name"
}

os="$(detect_os)"
arch="$(detect_arch)"

if [ "$VERSION" = "latest" ]; then
  VERSION="$(latest_version)"
fi
case "$VERSION" in v*) VERSION="${VERSION#v}" ;; esac
if [ -z "$VERSION" ]; then
  echo "skillex installer: could not resolve release version" >&2; exit 1
fi

case "$os" in
  darwin|linux)
    archive_name="skillex_${VERSION}_${os}_${arch}.tar.gz"
    binary_name="$BIN_NAME"
    ;;
  windows)
    archive_name="skillex_${VERSION}_${os}_${arch}.zip"
    binary_name="$BIN_NAME.exe"
    ;;
esac

if [ "${SKILLEX_BASE_URL:-}" ]; then
  base_url="${SKILLEX_BASE_URL%/}"
else
  base_url="https://github.com/$OWNER/$REPO/releases/download/v$VERSION"
fi

echo "Installing skillex $VERSION for ${os}/${arch}"
echo "Asset: $archive_name"
echo "Install dir: $INSTALL_DIR"

if [ "$DRY_RUN" = "1" ]; then
  echo "Dry run: would download $base_url/$archive_name"
  exit 0
fi

tmp="$(mktemp -d "${TMPDIR:-/tmp}/skillex-install.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT INT TERM

archive="$tmp/$archive_name"
checksums="$tmp/checksums.txt"

download "$base_url/$archive_name" "$archive"
download "$base_url/checksums.txt" "$checksums"
verify_checksum "$checksums" "$archive"
verify_cosign "$archive"

mkdir -p "$tmp/extract"
case "$archive_name" in
  *.tar.gz) need tar; tar -xzf "$archive" -C "$tmp/extract" ;;
  *.zip)    need unzip; unzip -q "$archive" -d "$tmp/extract" ;;
esac

binary="$(find "$tmp/extract" -type f -name "$binary_name" | head -n 1)"
if [ -z "$binary" ]; then
  echo "skillex installer: $binary_name binary not found in archive" >&2; exit 1
fi

mkdir -p "$INSTALL_DIR"
install_path="$INSTALL_DIR/$binary_name"
cp "$binary" "$install_path"
chmod 755 "$install_path"

echo "Installed skillex $VERSION to $install_path"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    echo ""
    echo "Note: $INSTALL_DIR is not on PATH."
    echo "Add it to your shell profile, for example:"
    echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
    ;;
esac
