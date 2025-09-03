#!/bin/bash
# Native Push Chain Dependencies & Build Setup
# Automatically installs dependencies and builds binaries for new machines

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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Installer layout:
#   ROOT_TOP (~/.local/share/push-validator-manager)
#     ├─ app/ (this script lives in app/scripts)
#     └─ repo/ (cloned source by install.sh)
ROOT_TOP="$(cd "$SCRIPT_DIR/../.." && pwd)"
LOCAL_REPO_DIR="$ROOT_TOP/repo"
BUILD_DIR="$SCRIPT_DIR/build"

print_status "🚀 Setting up native Push Chain environment..."
echo

# Fast-path: if binary already exists and works, skip all
if [ -f "$BUILD_DIR/pchaind" ]; then
    if "$BUILD_DIR/pchaind" --help >/dev/null 2>&1; then
        print_success "✅ Existing binary detected: $BUILD_DIR/pchaind"
        print_success "✅ Skipping source clone and rebuild"
        echo
        echo "Binary location: $BUILD_DIR/pchaind"
        echo "Version: $("$BUILD_DIR/pchaind" version 2>/dev/null || echo "unknown")"
        exit 0
    fi
fi

# Detect OS
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    OS="linux"
elif [[ "$OSTYPE" == "darwin"* ]]; then
    OS="macos"
else
    print_error "❌ Unsupported OS: $OSTYPE"
    exit 1
fi

print_status "🔍 Detected OS: $OS"

# Function to install dependencies on Linux
install_linux_deps() {
    print_status "📦 Installing Linux dependencies..."
    
    # Update package list
    sudo apt-get update
    
    # Install required packages
    sudo apt-get install -y \
        build-essential \
        git \
        golang-go \
        jq \
        python3 \
        python3-venv \
        curl \
        wget \
        netcat-traditional
    
    # Setup Python virtual environment for dependencies
    VENV_DIR="$ROOT_TOP/venv"
    if [ ! -d "$VENV_DIR" ]; then
        print_status "Creating Python virtual environment..."
        python3 -m venv "$VENV_DIR"
    fi
    
    # Activate virtual environment and install dependencies
    source "$VENV_DIR/bin/activate"
    
    # Check and install tomlkit in the virtual environment
    if ! python3 -m pip show tomlkit &> /dev/null; then
        print_status "Installing tomlkit in virtual environment..."
        python3 -m pip install tomlkit
    fi
    
    # Verify installations
    print_status "✅ Verifying installations..."
    go version || print_error "Go installation failed"
    jq --version || print_error "jq installation failed"
    python3 --version || print_error "Python3 installation failed"
    python3 -m pip show tomlkit || print_error "tomlkit installation failed"
}

# Function to install dependencies on macOS
install_macos_deps() {
    print_status "📦 Checking macOS dependencies..."
    
    # Check if Homebrew is installed
    if ! command -v brew &> /dev/null; then
        print_warning "Homebrew not found. Installing..."
        /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
    fi
    
    # Install Go if not present
    if ! command -v go &> /dev/null; then
        print_status "Installing Go..."
        brew install go
    fi
    
    # Install jq if not present
    if ! command -v jq &> /dev/null; then
        print_status "Installing jq..."
        brew install jq
    fi
    
    # Setup Python virtual environment for dependencies
    VENV_DIR="$ROOT_TOP/venv"
    if [ ! -d "$VENV_DIR" ]; then
        print_status "Creating Python virtual environment..."
        python3 -m venv "$VENV_DIR"
    fi
    
    # Activate virtual environment and install dependencies
    source "$VENV_DIR/bin/activate"
    
    # Check and install tomlkit in the virtual environment
    if ! python3 -m pip show tomlkit &> /dev/null; then
        print_status "Installing tomlkit in virtual environment..."
        python3 -m pip install tomlkit
    fi
    
    # Verify installations
    print_status "✅ Verifying installations..."
    go version >/dev/null 2>&1 || print_error "Go installation failed"
    jq --version >/dev/null 2>&1 || print_error "jq installation failed"
    python3 --version >/dev/null 2>&1 || print_error "Python3 installation failed"
    python3 -m pip show tomlkit >/dev/null 2>&1 || print_error "tomlkit installation failed"
}

# Install dependencies based on OS
if [ "$OS" = "linux" ]; then
    install_linux_deps
else
    install_macos_deps
fi

print_success "✅ Dependencies installed successfully!"
echo

# Use the repo cloned by install.sh
if [ ! -d "$LOCAL_REPO_DIR" ] || [ ! -f "$LOCAL_REPO_DIR/go.mod" ]; then
    print_error "❌ Push Chain source not found at: $LOCAL_REPO_DIR"
    print_error "This script should be called by install.sh which clones the repository first."
    exit 1
fi

SRC_DIR="$LOCAL_REPO_DIR"
cd "$SRC_DIR"

# Patch chain ID inside app/app.go (silent)
APP_FILE="app/app.go"
OLD_CHAIN_ID="localchain_9000-1"
NEW_CHAIN_ID="push_42101-1"

if grep -q "$OLD_CHAIN_ID" "$APP_FILE"; then
    if [[ "$OS" == "macos" ]]; then
        sed -i '' "s/\"$OLD_CHAIN_ID\"/\"$NEW_CHAIN_ID\"/" "$APP_FILE"
    else
        sed -i "s/\"$OLD_CHAIN_ID\"/\"$NEW_CHAIN_ID\"/" "$APP_FILE"
    fi
fi

print_status "🔨 Building Push Chain binary..."

# Build directly to our target directory to avoid quarantine issues
mkdir -p "$BUILD_DIR"

# Use go build directly with proper flags for macOS compatibility
go build -mod=readonly -tags "netgo,ledger" \
    -ldflags "-X github.com/cosmos/cosmos-sdk/version.Name=pchain \
             -X github.com/cosmos/cosmos-sdk/version.AppName=pchaind \
             -X github.com/cosmos/cosmos-sdk/version.Version=1.0.1-native \
             -s -w" \
    -trimpath -o "$BUILD_DIR/pchaind" ./cmd/pchaind

# Verify binary was created
if [ -f "$BUILD_DIR/pchaind" ]; then
    
    # Make executable and set proper permissions
    chmod +x "$BUILD_DIR/pchaind"
    
    # Test basic functionality (silent)
    if ! "$BUILD_DIR/pchaind" --help >/dev/null 2>&1; then
        print_warning "⚠️ Binary created but may have issues"
    fi
else
    print_error "❌ Binary creation failed"
    exit 1
fi

echo
print_success "✅ Binary created successfully"
echo "Binary location: $BUILD_DIR/pchaind"
echo "Version: $("$BUILD_DIR/pchaind" version 2>/dev/null)"