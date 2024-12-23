#!/usr/bin/env bash

set -e

OS="$(uname | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

INSTALL_PATH="/usr/local/bin"

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
