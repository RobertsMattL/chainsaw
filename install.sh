#!/bin/sh
set -e

REPO="RobertsMattL/chainsaw"
BIN_DIR="${CHAINSAW_INSTALL:-/usr/local/bin}"

detect_target() {
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  ARCH=$(uname -m)

  case "$OS" in
    darwin) OS="darwin" ;;
    linux)  OS="linux" ;;
    *)
      echo "Unsupported OS: $OS" >&2
      exit 1
      ;;
  esac

  case "$ARCH" in
    x86_64)          ARCH="amd64" ;;
    aarch64|arm64)   ARCH="arm64" ;;
    armv7l|armv6l)   ARCH="arm" ;;
    *)
      echo "Unsupported architecture: $ARCH" >&2
      exit 1
      ;;
  esac

  echo "${OS}-${ARCH}"
}

latest_version() {
  curl -sSfL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/'
}

main() {
  TARGET=$(detect_target)
  VERSION=$(latest_version)

  if [ -z "$VERSION" ]; then
    echo "Could not determine latest release version." >&2
    exit 1
  fi

  BINARY="chainsaw-${TARGET}"
  URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY}"

  echo "Installing chainsaw ${VERSION} (${TARGET})..."

  TMP=$(mktemp)
  trap 'rm -f "$TMP"' EXIT

  curl -sSfL "$URL" -o "$TMP"
  chmod +x "$TMP"

  if [ -w "$BIN_DIR" ]; then
    mv "$TMP" "${BIN_DIR}/chainsaw"
  else
    echo "Need sudo to install to ${BIN_DIR}..."
    sudo mv "$TMP" "${BIN_DIR}/chainsaw"
  fi

  echo "chainsaw installed to ${BIN_DIR}/chainsaw"
  chainsaw --version 2>/dev/null || true
}

main
