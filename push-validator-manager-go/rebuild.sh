#!/usr/bin/env bash
# Simple rebuild script for push-validator-manager

set -e

echo "Building push-validator-manager..."
CGO_ENABLED=0 go build -o build/push-validator-manager ./cmd/push-validator-manager

echo "✓ Built: build/push-validator-manager"

# Automatically copy to system location
mkdir -p ~/.local/bin
cp build/push-validator-manager ~/.local/bin/push-validator-manager
chmod +x ~/.local/bin/push-validator-manager
echo "✓ Copied to ~/.local/bin/push-validator-manager"
echo ""
echo "You can now run: push-validator-manager dashboard"
