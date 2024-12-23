#!/bin/bash

OUTPUT_DIR="bin"

BINARY_NAME="kubectl-monitor"

if [ ! -d "$OUTPUT_DIR" ]; then
  echo "Creating directory $OUTPUT_DIR..."
  mkdir -p "$OUTPUT_DIR"
fi

PLATFORMS=(
  "darwin/amd64" # macOS 64-bit
  "darwin/arm64" # macOS (M1/M2)
  "linux/amd64"  # Linux 64-bit
  "linux/arm64"  # Linux ARM 64-bit
)

for PLATFORM in "${PLATFORMS[@]}"; do
  IFS="/" read -r GOOS GOARCH <<<"$PLATFORM"
  OUTPUT_FILE="$OUTPUT_DIR/$BINARY_NAME-$GOOS-$GOARCH"

  echo "Building binary for $GOOS/$GOARCH..."
  GOOS=$GOOS GOARCH=$GOARCH go build -o "$OUTPUT_FILE" main.go

  if [ $? -eq 0 ]; then
    echo "Binary created: $OUTPUT_FILE"
  else
    echo "Failed to build binary for $GOOS/$GOARCH."
    exit 1
  fi

done

echo "All required binaries have been created in the $OUTPUT_DIR folder."
