#!/bin/bash

# Build script for Mac development
# This script builds the push-chain binary locally and prepares it for Docker

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Configuration
REPO_URL="https://github.com/pushchain/push-chain-node.git"
BUILD_DIR="/tmp/push-chain-build"
BINARY_OUTPUT="/tmp/pchaind-local"

print_status "🍎 Building Push Chain binary for Mac development..."

# Check if we're on macOS
if [[ "$OSTYPE" != "darwin"* ]]; then
    print_error "This script is designed for macOS only!"
    exit 1
fi

# Check if Go is installed
if ! command -v go >/dev/null 2>&1; then
    print_error "Go is not installed. Please install Go first:"
    print_error "  brew install go"
    exit 1
fi

print_success "Go is installed: $(go version)"

# Clean up previous build
if [[ -d "$BUILD_DIR" ]]; then
    print_status "Cleaning up previous build..."
    rm -rf "$BUILD_DIR"
fi

# Clone the repository
print_status "Cloning repository..."
git clone "$REPO_URL" "$BUILD_DIR"
cd "$BUILD_DIR"

# Check out main branch (most compatible)
print_status "Using main branch for compatibility..."
git checkout main

# Build the binary
print_status "Building binary..."
if command -v make >/dev/null 2>&1; then
    print_status "Using make install..."
    make install
    
    # Find the built binary
    if [[ -f "$HOME/go/bin/pchaind" ]]; then
        BUILT_BINARY="$HOME/go/bin/pchaind"
    elif [[ -f "/usr/local/go/bin/pchaind" ]]; then
        BUILT_BINARY="/usr/local/go/bin/pchaind"
    else
        print_warning "Binary not found in expected locations, trying direct build..."
        go build -o "$BINARY_OUTPUT" ./cmd/pchaind/
        BUILT_BINARY="$BINARY_OUTPUT"
    fi
else
    print_status "Using direct go build..."
    go build -o "$BINARY_OUTPUT" ./cmd/pchaind/
    BUILT_BINARY="$BINARY_OUTPUT"
fi

# Copy binary to expected location
if [[ "$BUILT_BINARY" != "$BINARY_OUTPUT" ]]; then
    print_status "Copying binary to $BINARY_OUTPUT..."
    cp "$BUILT_BINARY" "$BINARY_OUTPUT"
fi

# Make executable
chmod +x "$BINARY_OUTPUT"

# Test the binary
print_status "Testing binary..."
if "$BINARY_OUTPUT" version >/dev/null 2>&1; then
    VERSION=$("$BINARY_OUTPUT" version 2>/dev/null || echo "unknown")
    print_success "Binary built successfully! Version: $VERSION"
else
    print_warning "Binary built but version check failed (may still work)"
fi

# Clean up build directory
cd /
rm -rf "$BUILD_DIR"

print_success "✅ Mac binary ready at: $BINARY_OUTPUT"
print_status "📋 Next steps:"
print_status "  1. Build Docker image: docker build --build-arg TARGETOS=darwin --build-arg TARGETARCH=arm64 -t push-node-manager:mac ."
print_status "  2. Test with: docker run --rm push-node-manager:mac pchaind version"
print_status "  3. Use with push-node-manager for development"

echo ""
print_status "🚀 Ready for Mac development!" 