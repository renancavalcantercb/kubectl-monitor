#!/usr/bin/env bash

set -e

OS="$(uname | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

INSTALL_PATH="/usr/local/bin"

# Normalize architecture name
case "$ARCH" in
  "x86_64")
    ARCH="amd64"
    ;;
  "aarch64"|"arm64")
    ARCH="arm64"
    ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

case "$OS" in
  "darwin")
    BINARY="kubectl-monitor-darwin-$ARCH"
    ;;
  "linux")
    BINARY="kubectl-monitor-linux-$ARCH"
    ;;
  *)
    echo "Unsupported OS: $OS"
    exit 1
    ;;
esac

if [ ! -f "bin/$BINARY" ]; then
  echo "Binary bin/$BINARY not found!"
  exit 1
fi

echo "Copying bin/$BINARY to $INSTALL_PATH/kubectl-monitor..."
sudo cp "bin/$BINARY" "$INSTALL_PATH/kubectl-monitor"
sudo chmod +x "$INSTALL_PATH/kubectl-monitor"

echo ""
echo "Installation completed!"
echo "Now you can run: kubectl-monitor"
