#!/usr/bin/env bash

# Push Validator Manager one-liner installer (source-build only, no CI required)
# By default, resets blockchain data for clean installation (wallets/keys preserved)
# Usage examples:
#   curl -fsSL https://get.push.network/pnm/install.sh | bash                    # Clean install (default)
#   curl -fsSL https://get.push.network/pnm/install.sh | bash -s -- --no-reset   # Keep existing data
#   curl -fsSL https://get.push.network/pnm/install.sh | bash -s -- --no-start   # Don't auto-start
#   PNM_REF=v1.0.0 curl -fsSL https://get.push.network/pnm/install.sh | bash     # Install specific version
#   MONIKER=my-node GENESIS_DOMAIN=rpc-testnet-donut-node1.push.org KEYRING_BACKEND=os \
#     curl -fsSL https://get.push.network/pnm/install.sh | bash

set -euo pipefail
IFS=$'\n\t'
ORIGINAL_PATH="$PATH"

# Colors for output
CYAN='\033[0;36m'
NC='\033[0m'

# Read env or defaults
MONIKER="${MONIKER:-push-validator}"
GENESIS_DOMAIN="${GENESIS_DOMAIN:-rpc-testnet-donut-node1.push.org}"
KEYRING_BACKEND="${KEYRING_BACKEND:-test}"
AUTO_START="yes"
RESET_DATA="${RESET_DATA:-yes}"  # Default to reset for clean installation

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-start) AUTO_START="no"; shift ;;
    --start) AUTO_START="yes"; shift ;;
    --no-reset) RESET_DATA="no"; shift ;;
    --reset) RESET_DATA="yes"; shift ;;
    --moniker) MONIKER="$2"; shift 2 ;;
    --genesis) GENESIS_DOMAIN="$2"; shift 2 ;;
    --keyring) KEYRING_BACKEND="$2"; shift 2 ;;
    *) echo "Unknown flag: $1" >&2; exit 1 ;;
  esac
done

require_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "Missing dependency: $1" >&2; exit 1; }; }
for c in curl jq git tar; do require_cmd "$c"; done

# Use XDG Base Directory or fallback to hidden directory
if [[ -n "${XDG_DATA_HOME:-}" ]]; then
    ROOT_DIR="$XDG_DATA_HOME/push-validator-manager"
else
    ROOT_DIR="$HOME/.local/share/push-validator-manager"
fi
REPO_DIR="$ROOT_DIR/repo"
INSTALL_DIR="$ROOT_DIR/app"
MANAGER_LINK="$HOME/.local/bin/push-validator-manager"

mkdir -p "$ROOT_DIR"
mkdir -p "$HOME/.local/bin"
cd "$ROOT_DIR"

# Reset blockchain data by default (preserve keyring)
if [[ "$RESET_DATA" = "yes" ]] && [[ -d "$HOME/.pchain" ]]; then
    echo -e "${CYAN}🧹 Resetting blockchain data (wallets preserved)...${NC}"
    # Remove entire directories to ensure clean state
    rm -rf "$HOME/.pchain/data" 2>/dev/null || true
    rm -rf "$HOME/.pchain/config" 2>/dev/null || true
    rm -rf "$HOME/.pchain/wasm" 2>/dev/null || true
    rm -rf "$HOME/.pchain/logs" 2>/dev/null || true
    # Remove pid file if exists
    rm -f "$HOME/.pchain/pchaind.pid" 2>/dev/null || true
    # Note: ~/.pchain/keyring-test/ is preserved
    echo -e "\033[0;32m✅ Blockchain data reset (wallets preserved)${NC}"
fi

echo -e "${CYAN}📦 Installing Push Validator Manager into $ROOT_DIR${NC}"

# Repository version configuration
# Allow override via environment variable for specific version/branch/tag
PNM_REF="${PNM_REF:-feature/pnm}"  # Default to feature branch, can be overridden to stable tag

# Always clone fresh repository to ensure latest version
rm -rf "$REPO_DIR"
echo -e "${CYAN}📥 Fetching Push Validator Manager (ref: $PNM_REF)...${NC}"
git clone --quiet --depth 1 --branch "$PNM_REF" https://github.com/pushchain/push-chain-node "$REPO_DIR"

# Build native binary and ensure manager script

# Copy manager to a stable install directory so we can delete the repo later
rm -rf "$INSTALL_DIR"
mkdir -p "$INSTALL_DIR"
if [[ ! -d "$REPO_DIR/push-validator-manager" ]]; then
  echo "Error: missing source at $REPO_DIR/push-validator-manager"
  exit 1
fi
cp -a "$REPO_DIR/push-validator-manager/." "$INSTALL_DIR/"

cd "$INSTALL_DIR"
bash scripts/setup-dependencies.sh

# Ensure the push-validator-manager script is executable
chmod +x "$INSTALL_DIR/push-validator-manager"

# Create symlink for binary in expected location
# The register-validator script expects ../build/pchaind relative to scripts/ directory
mkdir -p "$INSTALL_DIR/build"
cd "$INSTALL_DIR/build"
ln -sf ../scripts/build/pchaind pchaind
cd "$INSTALL_DIR"

# Remove any existing symlink/script and install a small launcher script
rm -f "$MANAGER_LINK"
cat > "$MANAGER_LINK" <<EOF
#!/usr/bin/env bash
exec "$INSTALL_DIR/push-validator-manager" "\$@"
EOF
chmod +x "$MANAGER_LINK"

# Verify the script exists and is executable
if [[ ! -f "$INSTALL_DIR/push-validator-manager" ]]; then
  echo "Error: push-validator-manager script not found in $INSTALL_DIR"
  exit 1
fi

if [[ ! -x "$INSTALL_DIR/push-validator-manager" ]]; then
  echo "Error: push-validator-manager script is not executable"
  exit 1
fi

# Add to PATH if not already there
SHELL_CONFIG=""
if [[ -f "$HOME/.zshrc" ]]; then
    SHELL_CONFIG="$HOME/.zshrc"
elif [[ -f "$HOME/.bashrc" ]]; then
    SHELL_CONFIG="$HOME/.bashrc"
elif [[ -f "$HOME/.bash_profile" ]]; then
    SHELL_CONFIG="$HOME/.bash_profile"
fi

if [[ -n "$SHELL_CONFIG" ]]; then
    if ! grep -q "push-validator-manager" "$SHELL_CONFIG" 2>/dev/null; then
        echo "" >> "$SHELL_CONFIG"
        echo "# Push Validator Manager" >> "$SHELL_CONFIG"
        echo "export PATH=\"$HOME/.local/bin:\$PATH\"" >> "$SHELL_CONFIG"
    fi
fi

# ALWAYS export PATH for current session, regardless of shell config
export PATH="$HOME/.local/bin:$PATH"

# Best-effort install for a WebSocket client (websocat preferred, else wscat)
install_ws_client() {
  echo -e "${CYAN}🔌 Checking WebSocket client for real-time sync...${NC}"
  if command -v websocat >/dev/null 2>&1; then
    echo -e "\033[0;32m✅ websocat already installed${NC}"
    return 0
  fi
  if command -v wscat >/dev/null 2>&1; then
    echo -e "\033[0;32m✅ wscat already installed${NC}"
    return 0
  fi

  OS_NAME="$(uname -s)"
  ARCH_NAME="$(uname -m)"

  # Try package managers first
  if [[ "$OS_NAME" = "Darwin" ]]; then
    if command -v brew >/dev/null 2>&1; then
      HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_CLEANUP=1 HOMEBREW_NO_ENV_HINTS=1 \
        brew install websocat >/dev/null 2>&1 || true
    fi
  elif [[ "$OS_NAME" = "Linux" ]]; then
    if command -v apt-get >/dev/null 2>&1; then
      sudo apt-get update -y -qq >/dev/null 2>&1 || true
      sudo apt-get install -y -qq websocat >/dev/null 2>&1 || true
    fi
  fi

  if command -v websocat >/dev/null 2>&1; then
    echo -e "\033[0;32m✅ websocat installed${NC}"
    return 0
  fi

  # Attempt direct GitHub release download of websocat (best effort)
  echo -e "${CYAN}🌐 Attempting direct download of websocat release...${NC}"
  RELEASE_API="https://api.github.com/repos/vi/websocat/releases/latest"
  ASSET_URL=""

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
    ASSET_URL=$(curl -fsSL "$RELEASE_API" | jq -r \
      '.assets[] | select(.name | contains("'"$ASSET_FILTER"'")) | .browser_download_url' | head -n1 2>/dev/null || true)
  fi

  if [[ -n "$ASSET_URL" ]]; then
    TMP_BIN="$HOME/.local/bin/websocat"
    mkdir -p "$HOME/.local/bin"
    curl -fsSL "$ASSET_URL" -o "$TMP_BIN" 2>/dev/null || true
    chmod +x "$TMP_BIN" 2>/dev/null || true
  fi

  if command -v websocat >/dev/null 2>&1; then
    echo -e "\033[0;32m✅ websocat installed (download)${NC}"
    return 0
  fi

  # Fallback to wscat via npm (if available)
  if command -v npm >/dev/null 2>&1; then
    npm install -g --silent --no-progress wscat >/dev/null 2>&1 || true
  else
    # Try to get npm if practical
    if [[ "$OS_NAME" = "Darwin" ]] && command -v brew >/dev/null 2>&1; then
      HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_CLEANUP=1 HOMEBREW_NO_ENV_HINTS=1 \
        brew install node >/dev/null 2>&1 || true
    elif [[ "$OS_NAME" = "Linux" ]] && command -v apt-get >/dev/null 2>&1; then
      sudo apt-get install -y -qq npm >/dev/null 2>&1 || true
    fi
    if command -v npm >/dev/null 2>&1; then
      npm install -g --silent --no-progress wscat >/dev/null 2>&1 || true
    fi
  fi

  if command -v websocat >/dev/null 2>&1 || command -v wscat >/dev/null 2>&1; then
    echo -e "\033[0;32m✅ WebSocket client available (real-time sync enabled)${NC}"
    # Ensure npm global bin (where wscat usually lives) is on PATH for this session and future shells
    if command -v npm >/dev/null 2>&1; then
      NPM_BIN="$(npm bin -g 2>/dev/null || true)"
      if [[ -n "$NPM_BIN" ]]; then
        case ":$PATH:" in
          *":$NPM_BIN:"*) : ;;
          *) export PATH="$NPM_BIN:$PATH" ;;
        esac
        # Persist to shell config if we created/updated it earlier
        if [[ -n "$SHELL_CONFIG" ]] && ! grep -q "$NPM_BIN" "$SHELL_CONFIG" 2>/dev/null; then
          echo "export PATH=\"$NPM_BIN:\$PATH\"" >> "$SHELL_CONFIG"
        fi
      fi
    fi
    return 0
  fi

  echo -e "\033[1;33m⚠️  Could not install websocat/wscat automatically. Falling back to polling.\033[0m"
  echo "   You can install manually:"
  echo "   - websocat: brew install websocat  |  sudo apt-get install -y websocat"
  echo "   - wscat: npm install -g wscat"
}

# Try to install a WS client now (best effort, non-fatal)
install_ws_client || true

# Persist configuration
ENV_FILE="$ROOT_DIR/.env"
tmp="$ENV_FILE.tmp"; : > "$tmp"
{ grep -v -e '^GENESIS_DOMAIN=' -e '^MONIKER=' -e '^KEYRING_BACKEND=' "$ENV_FILE" 2>/dev/null || true; } >> "$tmp"
mv "$tmp" "$ENV_FILE"
{
  echo "GENESIS_DOMAIN=$GENESIS_DOMAIN"
  echo "MONIKER=$MONIKER"
  echo "KEYRING_BACKEND=$KEYRING_BACKEND"
} >> "$ENV_FILE"

# Run auto-start before cleanup to ensure wrapper script is available
if [[ "$AUTO_START" = "yes" ]]; then
  if "$MANAGER_LINK" start; then
    # Wait longer for node to stabilize
    sleep 2
    
    # Monitor sync until complete
    echo -e "${CYAN}⏳ Waiting for node to sync...${NC}"
    
    # Check sync status in a loop
    SYNC_COMPLETE=false
    MAX_WAIT=600  # 10 minutes max wait
    WAIT_TIME=0
    
    while [ $WAIT_TIME -lt $MAX_WAIT ]; do
      # Get sync status
      SYNC_STATUS=$("$MANAGER_LINK" status 2>/dev/null | grep -E "Catching Up|Block Height" || true)
      
      # Check if fully synced
      if echo "$SYNC_STATUS" | grep -q "Catching Up: false"; then
        SYNC_COMPLETE=true
        break
      fi
      
      # Check if we can extract heights for progress display
      if echo "$SYNC_STATUS" | grep -q "Block Height"; then
        CURRENT_HEIGHT=$(echo "$SYNC_STATUS" | grep "Block Height" | sed -E 's/.*Block Height:[[:space:]]*([0-9]+).*/\1/')
        NETWORK_HEIGHT=$(echo "$SYNC_STATUS" | grep "Block Height" | sed -E 's/.*\/[[:space:]]*([0-9]+).*/\1/')
        
        if [ -n "$CURRENT_HEIGHT" ] && [ -n "$NETWORK_HEIGHT" ]; then
          if [ "$CURRENT_HEIGHT" = "$NETWORK_HEIGHT" ] || [ $((NETWORK_HEIGHT - CURRENT_HEIGHT)) -le 2 ]; then
            SYNC_COMPLETE=true
            break
          fi
          
          # Show progress
          PERCENT=$((CURRENT_HEIGHT * 100 / NETWORK_HEIGHT))
          echo -ne "\r\033[K🔄 Syncing: ${CURRENT_HEIGHT}/${NETWORK_HEIGHT} (${PERCENT}%)  "
        fi
      fi
      
      sleep 3
      WAIT_TIME=$((WAIT_TIME + 3))
    done
    
    echo  # New line after progress display
    
    if [ "$SYNC_COMPLETE" = true ]; then
      echo -e "\033[0;32m✅ Node is fully synced!${NC}"
      echo
      
      # Prompt for validator registration
      echo -e "${CYAN}════════════════════════════════════════════════════${NC}"
      echo -e "${BOLD}${YELLOW}🎯 Ready to become a validator!${NC}"
      echo -e "${CYAN}════════════════════════════════════════════════════${NC}"
      echo
      echo -e "${BOLD}Next steps:${NC}"
      echo -e "1. Get test tokens from: ${GREEN}https://faucet.push.org${NC}"
      echo -e "2. Register as validator: ${GREEN}push-validator-manager register-validator${NC}"
      echo
      echo -e "${YELLOW}Would you like to register as a validator now? (y/N)${NC}"
      read -r -p "> " REGISTER_NOW
      
      if [[ "$REGISTER_NOW" =~ ^[Yy]$ ]]; then
        echo
        "$MANAGER_LINK" register-validator
      else
        echo
        echo -e "${CYAN}You can register later with:${NC}"
        echo -e "${GREEN}  push-validator-manager register-validator${NC}"
      fi
    else
      echo -e "${YELLOW}⚠️ Sync is taking longer than expected${NC}"
      echo "Monitor sync status with: push-validator-manager sync"
      echo "Register when ready with: push-validator-manager register-validator"
    fi
  else
    echo -e "\033[0;31m❌ Failed to start node. Check logs for details.\033[0m"
    echo "You can try starting manually with: push-validator-manager start"
  fi
fi

# ALWAYS show PATH instruction when running from pipe (curl | bash)
if [ ! -t 0 ]; then
  case ":$ORIGINAL_PATH:" in
    *":$HOME/.local/bin:"*) : ;; # already present before running
    *)
      echo
      echo -e "\033[1;33m⚠️  To use push-validator-manager in this terminal, run:\033[0m"
      echo -e "\033[1;32m    export PATH=\"\$HOME/.local/bin:\$PATH\"\033[0m"
      echo
      echo "Or open a new terminal window."
      ;;
  esac
fi

# Optional: Clean up the cloned repository to save space (keep only push-validator-manager)
cd "$ROOT_DIR"
if [[ -d "$REPO_DIR" ]]; then
    # Remove the temporary clone
    rm -rf "$REPO_DIR"
fi
