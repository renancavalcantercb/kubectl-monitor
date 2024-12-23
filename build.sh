#!/bin/bash

# Output directory
OUTPUT_DIR="bin"

# Binary name
BINARY_NAME="kubectl-monitor"

# Create the output directory if it doesn't exist
if [ ! -d "$OUTPUT_DIR" ]; then
    echo "Creating directory $OUTPUT_DIR..."
    mkdir -p "$OUTPUT_DIR"
fi

# Build for the local platform
echo "Building binary for the local platform..."
GOOS=$(go env GOOS)
GOARCH=$(go env GOARCH)
OUTPUT_FILE="$OUTPUT_DIR/$BINARY_NAME-$GOOS-$GOARCH"

go build -o "$OUTPUT_FILE" main.go

if [ $? -eq 0 ]; then
    echo "Binary created: $OUTPUT_FILE"
else
    echo "Failed to build binary for the local platform."
    exit 1
fi

# Optional: Build for other platforms
PLATFORMS=("linux/amd64" "linux/arm64" "windows/amd64")

for PLATFORM in "${PLATFORMS[@]}"; do
    IFS="/" read -r GOOS GOARCH <<< "$PLATFORM"
    OUTPUT_FILE="$OUTPUT_DIR/$BINARY_NAME-$GOOS-$GOARCH"

    echo "Building binary for $GOOS/$GOARCH..."
    GOOS=$GOOS GOARCH=$GOARCH go build -o "$OUTPUT_FILE" main.go

    if [ $? -eq 0 ]; then
        echo "Binary created: $OUTPUT_FILE"
    else
        echo "Failed to build binary for $GOOS/$GOARCH."
    fi
done

echo "All binaries have been created in the $OUTPUT_DIR folder."
