#!/bin/bash
# Build pchaind binary with correct version for Push Chain compatibility

set -e

GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m'

print_status() { echo -e "${BLUE}$1${NC}"; }
print_success() { echo -e "${GREEN}$1${NC}"; }
print_error() { echo -e "${RED}$1${NC}"; }
print_warning() { echo -e "${YELLOW}$1${NC}"; }

# Get script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Parse arguments
SOURCE_DIR="${1:-}"
OUTPUT_DIR="${2:-$SCRIPT_DIR/build}"

if [ -z "$SOURCE_DIR" ]; then
    # Try to find the source directory
    # First check if we're in a local dev environment
    if [ -d "$SCRIPT_DIR/../../go.mod" ]; then
        SOURCE_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
    elif [ -d "$HOME/.local/share/push-validator/repo" ]; then
        SOURCE_DIR="$HOME/.local/share/push-validator/repo"
    else
        print_error "❌ Usage: $0 <source-dir> [output-dir]"
        print_error "   source-dir: Path to push-chain source code"
        print_error "   output-dir: Where to place built binary (default: ./build)"
        exit 1
    fi
fi

if [ ! -f "$SOURCE_DIR/go.mod" ]; then
    print_error "❌ Invalid source directory: $SOURCE_DIR"
    print_error "   Expected to find go.mod in the directory"
    exit 1
fi

# Validate Go version (requires 1.23+ for pchaind build)
GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
GO_MAJOR=$(echo "$GO_VERSION" | cut -d. -f1)
GO_MINOR=$(echo "$GO_VERSION" | cut -d. -f2)

if [[ "$GO_MAJOR" -lt 1 ]] || [[ "$GO_MAJOR" -eq 1 && "$GO_MINOR" -lt 23 ]]; then
    print_error "❌ Go 1.23 or higher is required (found: $GO_VERSION)"
    echo
    echo "The Push Node Daemon (pchaind) requires Go 1.23+ to build."
    echo
    echo "Please upgrade Go:"
    if [[ "$OSTYPE" == "darwin"* ]]; then
        echo "  • Using Homebrew: brew upgrade go"
        echo "  • Or download from: https://go.dev/dl/"
    else
        echo "  • Download from: https://go.dev/dl/"
        echo "  • Or use your package manager to upgrade"
    fi
    exit 1
fi
print_status "✓ Go version check passed: $GO_VERSION"

print_status "🔨 Building Push Chain binary from: $SOURCE_DIR"

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Change to source directory
cd "$SOURCE_DIR"

# Detect OS for sed command
OS="linux"
if [[ "$OSTYPE" == "darwin"* ]]; then
    OS="macos"
fi

# Patch chain ID in app/app.go if needed
APP_FILE="app/app.go"
OLD_CHAIN_ID="localchain_9000-1"
NEW_CHAIN_ID="push_42101-1"

if [ -f "$APP_FILE" ]; then
    if grep -q "$OLD_CHAIN_ID" "$APP_FILE"; then
        print_status "📝 Patching chain ID from $OLD_CHAIN_ID to $NEW_CHAIN_ID"
        if [[ "$OS" == "macos" ]]; then
            sed -i '' "s/\"$OLD_CHAIN_ID\"/\"$NEW_CHAIN_ID\"/" "$APP_FILE"
        else
            sed -i "s/\"$OLD_CHAIN_ID\"/\"$NEW_CHAIN_ID\"/" "$APP_FILE"
        fi
    fi
fi

# Detect version from git tags (can be overridden via VERSION env var)
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "v1.0.1")}
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(date -u '+%Y-%m-%dT%H:%M:%SZ')

# Build with the exact same flags as the bash version
print_status "Building Push Node Daemon (version $VERSION)..."

go build -mod=readonly -tags "netgo,ledger" \
    -ldflags "-X github.com/cosmos/cosmos-sdk/version.Name=pchain \
             -X github.com/cosmos/cosmos-sdk/version.AppName=pchaind \
             -X github.com/cosmos/cosmos-sdk/version.Version=$VERSION-native \
             -s -w" \
    -trimpath -o "$OUTPUT_DIR/pchaind" ./cmd/pchaind

# Verify binary was created
if [ ! -f "$OUTPUT_DIR/pchaind" ]; then
    print_error "❌ Binary creation failed"
    exit 1
fi

# Make executable
chmod +x "$OUTPUT_DIR/pchaind"

# Test basic functionality
if ! "$OUTPUT_DIR/pchaind" version >/dev/null 2>&1; then
    print_warning "⚠️ Binary created but may have issues"
else
    VERSION=$("$OUTPUT_DIR/pchaind" version 2>&1 | head -1)
    print_success "✅ Binary created successfully: $OUTPUT_DIR/pchaind"
    print_success "   Version: $VERSION"
fi

echo
echo "Binary location: $OUTPUT_DIR/pchaind"