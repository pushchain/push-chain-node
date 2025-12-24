#!/bin/bash
# Test released binaries by downloading from GitHub and running local testnet
#
# Usage:
#   ./scripts/test_release_binary.sh                    # Latest release
#   ./scripts/test_release_binary.sh v0.0.15-test       # Specific version
#   VERSION=v0.0.15-test ./scripts/test_release_binary.sh

set -eu

# Configuration
REPO="pushchain/push-chain-node"
BINARY_NAME="pchaind"
VERSION=${VERSION:-${1:-"latest"}}
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
TEMP_DIR=$(mktemp -d)

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case $ARCH in
  x86_64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
esac

echo "=== Release Binary Tester ==="
echo "OS: $OS, Arch: $ARCH"

# Check for gh CLI
if ! command -v gh &> /dev/null; then
  echo "Error: GitHub CLI (gh) is required. Install from: https://cli.github.com/"
  exit 1
fi

# Get version (resolve 'latest')
if [ "$VERSION" = "latest" ]; then
  echo "Fetching latest release..."
  VERSION=$(gh release view --repo $REPO --json tagName -q .tagName)
fi
VERSION_NO_V="${VERSION#v}"
echo "Version: $VERSION"

# Download binary
ASSET_NAME="push-chain_${VERSION_NO_V}_${OS}_${ARCH}.tar.gz"
echo "Downloading: $ASSET_NAME"
gh release download "$VERSION" --repo "$REPO" --pattern "$ASSET_NAME" --dir "$TEMP_DIR"

# Extract
echo "Extracting..."
cd "$TEMP_DIR"
tar -xzf "$ASSET_NAME"

# Find the binary (could be pchaind or pchaind-darwin-arm64 etc)
BINARY_FILE=$(find . -type f \( -perm -u+x -o -name "$BINARY_NAME*" \) 2>/dev/null | grep -E "pchaind" | head -1)
if [ -z "$BINARY_FILE" ]; then
  BINARY_FILE=$(find . -name "$BINARY_NAME*" -type f | head -1)
fi

if [ -z "$BINARY_FILE" ]; then
  echo "Error: Could not find binary in archive"
  ls -la
  exit 1
fi

chmod +x "$BINARY_FILE"
# Remove ./ prefix if present and construct full path
BINARY_FILE="${BINARY_FILE#./}"
BINARY_PATH="$TEMP_DIR/$BINARY_FILE"

# Check for libwasmvm.dylib (macOS) - bundled with binary
if [ -f "$TEMP_DIR/libwasmvm.dylib" ]; then
  echo "Found bundled libwasmvm.dylib"
fi

# Verify binary works
echo ""
echo "=== Testing Binary ==="
"$BINARY_PATH" version

echo ""
echo "=== Starting Testnet ==="

# Run testnet with the downloaded binary
export BINARY="$BINARY_PATH"
export CLEAN=true
export CHAIN_ID="localchain_9000-1"
export BLOCK_TIME="1000ms"

# Return to repo root
cd - > /dev/null

sh scripts/test_node.sh
