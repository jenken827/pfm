#!/bin/bash

# Build script for go-server-demo
# Automatically generates version from datetime and commit hash

set -e

# Get current directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

# Get current commit hash (first 8 characters)
COMMIT_HASH=$(git rev-parse --short=8 HEAD 2>/dev/null || echo "unknown")

# Get current datetime in format YYYYMMDDHHMMSS
DATETIME=$(date +"%Y%m%d%H%M%S")

# Build version string
VERSION="${DATETIME}-${COMMIT_HASH}"

echo "Building with version: $VERSION"

# Build with version injected via ldflags
go build -ldflags "-w -s -X jenken827.com/jenken827/pfm/globals.Version=${VERSION}" -o pfm .

echo "Build completed successfully!"
echo "Version: $VERSION"
