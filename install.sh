#!/usr/bin/env bash

set -e

VERSION="v1.0.3"

REPO="renancavalcantercb/kubectl-monitor"

OS="$(uname | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$OS" in
  "darwin")
    BINARY="kubectl-monitor-darwin-amd64"
    ;;
  "linux")
    BINARY="kubectl-monitor-linux-amd64"
    ;;
  *)
    echo "Unsupported OS or script not prepared for: $OS"
    exit 1
    ;;
esac

INSTALL_PATH="/usr/local/bin"

DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY}"

echo "Downloading ${DOWNLOAD_URL} ..."
curl -sL "$DOWNLOAD_URL" -o "/tmp/${BINARY}"

echo "Copying the binary to ${INSTALL_PATH}/kubectl-monitor ..."
sudo cp "/tmp/${BINARY}" "${INSTALL_PATH}/kubectl-monitor"
sudo chmod +x "${INSTALL_PATH}/kubectl-monitor"
rm -f "/tmp/${BINARY}"

echo ""
echo "Installation completed!"
echo "You can now run: kubectl-monitor"
