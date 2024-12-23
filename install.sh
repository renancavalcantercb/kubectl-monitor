#!/bin/bash

set -e

OS=$(uname | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

if [[ "$ARCH" == "x86_64" ]]; then
    ARCH="amd64"
elif [[ "$ARCH" == "arm64" ]] || [[ "$ARCH" == "aarch64" ]]; then
    ARCH="arm64"
else
    echo "Unsupported architecture: $ARCH"
    exit 1
fi

BINARY="kubectl-monitor-${OS}-${ARCH}"

URL="https://github.com/renancavalcantercb/kubectl-monitor/releases/latest/download/${BINARY}"

DEST_DIR="/usr/local/bin"

echo "Downloading ${BINARY} from ${URL}..."
curl -Lo kubectl-monitor "$URL"

echo "Setting executable permissions..."
chmod +x kubectl-monitor

echo "Moving to ${DEST_DIR}..."
sudo mv kubectl-monitor "$DEST_DIR"

echo "Installation complete! You can now use 'kubectl-monitor' as a kubectl plugin."
