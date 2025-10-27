#!/usr/bin/env bash
# Simple rebuild script for push-validator

set -e

echo "Building push-validator..."
CGO_ENABLED=0 go build -a -o build/push-validator ./cmd/push-validator

echo "✓ Built: build/push-validator"

# Automatically copy to system location
mkdir -p ~/.local/bin
cp build/push-validator ~/.local/bin/push-validator
chmod +x ~/.local/bin/push-validator
echo "✓ Copied to ~/.local/bin/push-validator"
echo ""
echo "You can now run: push-validator dashboard"
