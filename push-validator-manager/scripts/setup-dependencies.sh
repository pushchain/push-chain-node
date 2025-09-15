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
    
    # Detect Linux distribution
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        DISTRO=$ID
        DISTRO_FAMILY=$ID_LIKE
    else
        print_error "❌ Cannot detect Linux distribution"
        exit 1
    fi
    
    print_status "📦 Detected distribution: $DISTRO"
    
    # Install based on distribution
    if command -v apt-get >/dev/null 2>&1; then
        # Debian/Ubuntu based
        print_status "Installing via apt-get..."
        sudo apt-get update
        sudo apt-get install -y \
            build-essential \
            git \
            golang-go \
            jq \
            python3 \
            python3-venv \
            python3-pip \
            curl \
            wget
        # Prefer netcat-openbsd; fall back to netcat-traditional if needed
        sudo apt-get install -y netcat-openbsd || sudo apt-get install -y netcat-traditional
            
    elif command -v dnf >/dev/null 2>&1; then
        # Fedora/RHEL 8+ based
        print_status "Installing via dnf..."
        sudo dnf install -y \
            @development-tools \
            git \
            golang \
            jq \
            python3 \
            python3-pip \
            curl \
            wget \
            nmap-ncat
            
    elif command -v yum >/dev/null 2>&1; then
        # RHEL/CentOS 7 based
        print_status "Installing via yum..."
        sudo yum groupinstall -y "Development Tools"
        sudo yum install -y \
            git \
            golang \
            jq \
            python3 \
            python3-pip \
            curl \
            wget \
            nmap-ncat
            
    elif command -v pacman >/dev/null 2>&1; then
        # Arch Linux based
        print_status "Installing via pacman..."
        sudo pacman -Syu --noconfirm
        sudo pacman -S --noconfirm \
            base-devel \
            git \
            go \
            jq \
            python \
            python-pip \
            python-virtualenv \
            curl \
            wget \
            gnu-netcat
            
    elif command -v apk >/dev/null 2>&1; then
        # Alpine Linux based
        print_status "Installing via apk..."
        sudo apk update
        sudo apk add \
            build-base \
            git \
            go \
            jq \
            python3 \
            py3-pip \
            py3-virtualenv \
            curl \
            wget \
            netcat-openbsd
            
    elif command -v zypper >/dev/null 2>&1; then
        # openSUSE based
        print_status "Installing via zypper..."
        sudo zypper install -y \
            -t pattern devel_basis \
            git \
            go \
            jq \
            python3 \
            python3-pip \
            python3-virtualenv \
            curl \
            wget \
            netcat-openbsd
            
    else
        print_error "❌ Unsupported Linux distribution: $DISTRO"
        print_status "Please manually install: build tools, git, go, jq, python3, python3-venv, curl, wget, netcat"
        exit 1
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

# Optional: WebSocket client for real-time sync monitoring
install_ws_client() {
    if command -v websocat >/dev/null 2>&1 || command -v wscat >/dev/null 2>&1; then
        return 0
    fi

    # Quietly attempt installation of a WebSocket client for real-time sync

    if [ "$OS" = "macos" ]; then
        if command -v brew >/dev/null 2>&1; then
            HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_CLEANUP=1 HOMEBREW_NO_ENV_HINTS=1 \
                brew install websocat >/dev/null 2>&1 || true
        fi
        if ! command -v websocat >/dev/null 2>&1; then
            # fallback to wscat via npm
            if ! command -v npm >/dev/null 2>&1; then
                HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_CLEANUP=1 HOMEBREW_NO_ENV_HINTS=1 \
                    brew install node >/dev/null 2>&1 || true
            fi
            if command -v npm >/dev/null 2>&1; then
                npm install -g --silent --no-progress wscat >/dev/null 2>&1 || true
            fi
        fi
    else
        # linux
        if command -v apt-get >/dev/null 2>&1; then
            sudo apt-get update -y -qq >/dev/null 2>&1 || true
            sudo apt-get install -y -qq websocat >/dev/null 2>&1 || true
        fi
        if ! command -v websocat >/dev/null 2>&1; then
            if command -v apt-get >/dev/null 2>&1; then
                sudo apt-get install -y -qq npm >/dev/null 2>&1 || true
            fi
            if command -v npm >/dev/null 2>&1; then
                npm install -g --silent --no-progress wscat >/dev/null 2>&1 || true
            fi
        fi
    fi

    # Attempt direct release download as last resort
    if ! command -v websocat >/dev/null 2>&1 && ! command -v wscat >/dev/null 2>&1; then
        print_status "🌐 Attempting direct download of websocat release..."
        OS_NAME="$(uname -s)"; ARCH_NAME="$(uname -m)"
        ASSET_FILTER=""
        if [[ "$OS_NAME" = "Darwin" ]]; then
            if [[ "$ARCH_NAME" = "arm64" || "$ARCH_NAME" = "aarch64" ]]; then
                ASSET_FILTER="aarch64-apple-darwin"
            else
                ASSET_FILTER="x86_64-apple-darwin"
            fi
        elif [[ "$OS_NAME" = "Linux" ]]; then
            if [[ "$ARCH_NAME" = "arm64" || "$ARCH_NAME" = "aarch64" ]]; then
                ASSET_FILTER="aarch64-unknown-linux-musl"
            else
                ASSET_FILTER="x86_64-unknown-linux-musl"
            fi
        fi
        if [[ -n "$ASSET_FILTER" ]]; then
            RELEASE_API="https://api.github.com/repos/vi/websocat/releases/latest"
            # Get both download URL and SHA256 checksum URL
            RELEASE_INFO=$(curl -fsSL "$RELEASE_API" 2>/dev/null || true)
            ASSET_URL=$(echo "$RELEASE_INFO" | jq -r \
                '.assets[] | select(.name | contains("'"$ASSET_FILTER"'")) | .browser_download_url' | head -n1 2>/dev/null || true)
            CHECKSUM_URL=$(echo "$RELEASE_INFO" | jq -r \
                '.assets[] | select(.name | contains("sha256sum")) | .browser_download_url' | head -n1 2>/dev/null || true)
            
            if [[ -n "$ASSET_URL" ]]; then
                mkdir -p "$HOME/.local/bin"
                TEMP_DIR=$(mktemp -d)
                trap "rm -rf $TEMP_DIR" EXIT
                
                # Download with retry logic
                local retry_count=0
                local max_retries=3
                while [ $retry_count -lt $max_retries ]; do
                    if curl -fsSL "$ASSET_URL" -o "$TEMP_DIR/websocat" 2>/dev/null; then
                        break
                    fi
                    retry_count=$((retry_count + 1))
                    print_warning "⚠️ Download attempt $retry_count failed, retrying..."
                    sleep $((retry_count * 2))  # Exponential backoff
                done
                
                if [ $retry_count -eq $max_retries ]; then
                    print_error "❌ Failed to download websocat after $max_retries attempts"
                    return 1
                fi
                
                # Verify checksum if available
                if [[ -n "$CHECKSUM_URL" ]]; then
                    curl -fsSL "$CHECKSUM_URL" -o "$TEMP_DIR/checksums.txt" 2>/dev/null || true
                    if [ -f "$TEMP_DIR/checksums.txt" ]; then
                        # Extract checksum for our specific file
                        EXPECTED_CHECKSUM=$(grep "$ASSET_FILTER" "$TEMP_DIR/checksums.txt" | awk '{print $1}' 2>/dev/null || true)
                        if [[ -n "$EXPECTED_CHECKSUM" ]]; then
                            # Calculate actual checksum
                            if command -v sha256sum >/dev/null 2>&1; then
                                ACTUAL_CHECKSUM=$(sha256sum "$TEMP_DIR/websocat" | awk '{print $1}')
                            elif command -v shasum >/dev/null 2>&1; then
                                ACTUAL_CHECKSUM=$(shasum -a 256 "$TEMP_DIR/websocat" | awk '{print $1}')
                            fi
                            
                            if [[ -n "$ACTUAL_CHECKSUM" ]]; then
                                if [[ "$EXPECTED_CHECKSUM" != "$ACTUAL_CHECKSUM" ]]; then
                                    print_error "❌ Checksum verification failed for websocat"
                                    print_status "Expected: $EXPECTED_CHECKSUM"
                                    print_status "Actual: $ACTUAL_CHECKSUM"
                                    return 1
                                else
                                    print_success "✅ Checksum verified for websocat"
                                fi
                            fi
                        fi
                    fi
                fi
                
                # Move to final location
                mv "$TEMP_DIR/websocat" "$HOME/.local/bin/websocat"
                chmod +x "$HOME/.local/bin/websocat" 2>/dev/null || true
                # Add to PATH for current shell
                case ":$PATH:" in *":$HOME/.local/bin:"*) : ;; *) export PATH="$HOME/.local/bin:$PATH" ;; esac
            fi
        fi
    fi

    # Ensure npm global bin is on PATH if wscat was installed
    if command -v npm >/dev/null 2>&1; then
        NPM_BIN="$(npm bin -g 2>/dev/null || true)"
        if [[ -n "$NPM_BIN" ]]; then
            case ":$PATH:" in *":$NPM_BIN:"*) : ;; *) export PATH="$NPM_BIN:$PATH" ;; esac
        fi
    fi

    if command -v websocat >/dev/null 2>&1; then
        : # silent if installed successfully
    elif command -v wscat >/dev/null 2>&1; then
        : # silent if installed successfully
    else
        print_warning "⚠️ Could not install websocat/wscat automatically. Monitoring will use polling."
        print_status "   Manual install options:"
        print_status "   - websocat: brew install websocat  |  sudo apt-get install -y websocat"
        print_status "   - wscat: npm install -g wscat"
    fi
}

install_ws_client || true

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
