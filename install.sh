#!/bin/sh
set -eu

# lazy-tool-x installer
# Usage:
#   curl -sSfL https://raw.githubusercontent.com/cjnova/lazy-tool-x/main/install.sh | sh
#   curl -sSfL ... | INSTALL_DIR=/usr/local/bin sh

REPO="cjnova/lazy-tool-x"
BIN_NAME="lazy-tool-x"
INSTALL_DIR="${INSTALL_DIR:-./bin}"

fail() {
  printf 'Error: %s\n' "$1" >&2
  exit 1
}

detect_os() {
  case "$(uname -s)" in
    Linux*)  echo "linux" ;;
    Darwin*) echo "darwin" ;;
    *)       fail "Unsupported OS: $(uname -s). Only linux and darwin are supported." ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)  echo "amd64" ;;
    aarch64|arm64)  echo "arm64" ;;
    *)              fail "Unsupported architecture: $(uname -m). Only amd64 and arm64 are supported." ;;
  esac
}

get_latest_version() {
  if command -v curl >/dev/null 2>&1; then
    curl -sSfL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/'
  elif command -v wget >/dev/null 2>&1; then
    wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/'
  else
    fail "Neither curl nor wget found. Please install one of them."
  fi
}

download() {
  url="$1"
  dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -sSfL -o "$dest" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$dest" "$url"
  fi
}

main() {
  OS="$(detect_os)"
  ARCH="$(detect_arch)"
  VERSION="$(get_latest_version)"

  if [ -z "$VERSION" ]; then
    fail "Could not determine latest version. Check https://github.com/${REPO}/releases"
  fi

  printf 'Installing %s v%s (%s/%s)\n' "$BIN_NAME" "$VERSION" "$OS" "$ARCH"

  ARCHIVE="${BIN_NAME}_${VERSION}_${OS}_${ARCH}.tar.gz"
  BASE_URL="https://github.com/${REPO}/releases/download/v${VERSION}"

  TMPDIR="$(mktemp -d)"
  trap 'rm -rf "$TMPDIR"' EXIT

  printf 'Downloading %s...\n' "$ARCHIVE"
  download "${BASE_URL}/${ARCHIVE}" "${TMPDIR}/${ARCHIVE}"
  download "${BASE_URL}/checksums.txt" "${TMPDIR}/checksums.txt"

  printf 'Verifying checksum...\n'
  EXPECTED="$(grep "${ARCHIVE}" "${TMPDIR}/checksums.txt" | awk '{print $1}')"
  if [ -z "$EXPECTED" ]; then
    fail "Archive not found in checksums.txt"
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL="$(sha256sum "${TMPDIR}/${ARCHIVE}" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    ACTUAL="$(shasum -a 256 "${TMPDIR}/${ARCHIVE}" | awk '{print $1}')"
  else
    fail "Neither sha256sum nor shasum found. Cannot verify checksum."
  fi

  if [ "$EXPECTED" != "$ACTUAL" ]; then
    fail "Checksum mismatch!\n  Expected: ${EXPECTED}\n  Actual:   ${ACTUAL}"
  fi

  printf 'Extracting...\n'
  tar -xzf "${TMPDIR}/${ARCHIVE}" -C "${TMPDIR}"

  mkdir -p "$INSTALL_DIR"
  mv "${TMPDIR}/${BIN_NAME}" "${INSTALL_DIR}/${BIN_NAME}"
  chmod +x "${INSTALL_DIR}/${BIN_NAME}"

  printf 'Installed %s to %s/%s\n' "$BIN_NAME" "$INSTALL_DIR" "$BIN_NAME"
  printf 'Run "%s/%s version" to verify.\n' "$INSTALL_DIR" "$BIN_NAME"
}

main
