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
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
BOLD='\033[1m'
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
  echo -e "${CYAN}Starting Push Chain node...${NC}"
  
  # Start the node and monitor startup
  NODE_STARTED=false
  STATE_SYNC_DETECTED=false
  
  # Start node in background and capture initial output
  {
    timeout 30 "$MANAGER_LINK" start 2>&1 | while IFS= read -r line; do
      # Skip verbose logs that aren't useful for installer output
      if echo "$line" | grep -qE "Trust Height:|Trust Hash:|RPC Servers:|I\[.*\]|Reset private validator|Removed all blockchain|The address book"; then
        continue  # Skip these verbose log lines
      fi
      
      # Show important startup messages
      if echo "$line" | grep -qE "Starting Push Chain|Initializing validator|Node started successfully|Node already running|Configuring|configured|Starting state sync|Snapshot restored"; then
        echo "$line"
      fi
      
      # Detect if node startup succeeded
      if echo "$line" | grep -q "Node started successfully\|Node already running"; then
        NODE_STARTED=true
        break
      fi
      
      # Detect state sync activity
      if echo "$line" | grep -qE "Starting state sync|Discovered new snapshot"; then
        STATE_SYNC_DETECTED=true
        echo -e "${CYAN}🔄 State sync detected - this may take several minutes...${NC}"
      fi
      
      # Handle state sync failures
      if echo "$line" | grep -q "failed to start state sync"; then
        echo -e "${YELLOW}⚠️ State sync failed, node will sync from genesis${NC}"
        NODE_STARTED=true  # Node might still start without state sync
        break
      fi
    done
  } || true
  
  # Check if node is running with multiple attempts - allow more time for state sync startup
  echo -e "${CYAN}⏳ Verifying node startup...${NC}"
  for i in {1..30}; do  # Increased from 15 to 30 attempts (60 seconds total)
    STATUS_OUTPUT=$("$MANAGER_LINK" status 2>/dev/null || echo "status_failed")
    
    # Check for various indicators that node is running
    if echo "$STATUS_OUTPUT" | grep -q "Node is running"; then
      NODE_STARTED=true
      echo -e "${GREEN}✅ Node startup verified${NC}"
      break
    fi
    
    # Also check if node process is running even if status command fails
    if [ $i -gt 10 ]; then
      # After 20 seconds, also check for running process
      if pgrep -f "pchaind" >/dev/null 2>&1; then
        echo -e "${CYAN}🔍 Node process detected, verifying status...${NC}"
        # Give it a few more seconds and try status again
        sleep 3
        STATUS_OUTPUT=$("$MANAGER_LINK" status 2>/dev/null || echo "status_failed")
        if echo "$STATUS_OUTPUT" | grep -q "Node is running\|Syncing\|height"; then
          NODE_STARTED=true
          echo -e "${GREEN}✅ Node startup verified (process check)${NC}"
          break
        fi
      fi
    fi
    
    # Show progress messages at intervals
    if [ $((i % 10)) -eq 0 ]; then
      echo -e "${CYAN}⏳ Still checking node status (attempt $i/30)...${NC}"
    fi
    
    [ $i -lt 30 ] && sleep 2
  done
  
  # If state sync was detected, use enhanced monitoring with visual progress bars
  # If node was already running, try to detect sync state from status output
  if [ "$STATE_SYNC_DETECTED" != "true" ] && [ "$NODE_STARTED" = "true" ]; then
    STATUS_SNAPSHOT=$("$MANAGER_LINK" status 2>/dev/null || true)
    # Detect catching up via textual hints
    if echo "$STATUS_SNAPSHOT" | grep -qE "Catching Up:\s*true|Status:.*Sync|⏳\s*Syncing"; then
      STATE_SYNC_DETECTED=true
    else
      # Detect via block height numbers if present
      CURRENT_H=$(echo "$STATUS_SNAPSHOT" | sed -n 's/.*Block Height:[[:space:]]*\([0-9][0-9]*\).*/\1/p' | head -1)
      NETWORK_H=$(echo "$STATUS_SNAPSHOT" | sed -n 's#.*/[[:space:]]*\([0-9][0-9]*\).*#\1#p' | head -1)
      if [ -n "${CURRENT_H:-}" ] && [ -n "${NETWORK_H:-}" ]; then
        if [ "$CURRENT_H" -lt "$NETWORK_H" ]; then
          STATE_SYNC_DETECTED=true
        fi
      fi
    fi
  fi

  if [ "$STATE_SYNC_DETECTED" = "true" ] && [ "$NODE_STARTED" = "true" ]; then
    echo -e "${CYAN}📡 State sync detected - monitoring with enhanced progress display...${NC}"
    echo
    
    # Use the enhanced state sync monitoring from push-validator-manager
    # This provides visual progress bars, phase detection, and better user experience
    if "$MANAGER_LINK" monitor-state-sync 2>/dev/null; then
      echo -e "${GREEN}✅ State sync completed successfully!${NC}"
    else
      # Fallback to basic monitoring if enhanced version fails
      echo -e "${YELLOW}⚠️ Enhanced monitoring failed, falling back to basic monitoring...${NC}"
      
      # Basic fallback monitoring (simplified version of original)
      SYNC_TIMEOUT=600  # 10 minutes
      SYNC_START_TIME=$(date +%s)
      
      while true; do
        CURRENT_TIME=$(date +%s)
        ELAPSED=$((CURRENT_TIME - SYNC_START_TIME))
        
        if [ $ELAPSED -gt $SYNC_TIMEOUT ]; then
          echo -e "${YELLOW}⚠️ State sync taking longer than expected, but node is running${NC}"
          break
        fi
        
        # Check sync status using push-validator-manager status
        SYNC_INFO=$("$MANAGER_LINK" status 2>/dev/null | grep -E "Block Height:|Status:|network" || true)
        
        # Check if sync is complete
        if echo "$SYNC_INFO" | grep -q "Catching Up: false" || echo "$SYNC_INFO" | grep -q "✅.*SYNCED" || echo "$SYNC_INFO" | grep -q "Fully Synced"; then
          CURRENT_HEIGHT=$(echo "$SYNC_INFO" | grep "Block Height:" | grep -o "[0-9]\+" | head -1 || echo "unknown")
          echo -e "${GREEN}✅ State sync completed! Node synchronized to block ${CURRENT_HEIGHT}${NC}"
          break
        fi
        
        # Show simple progress every 15 seconds
        if [ $((ELAPSED % 15)) -eq 0 ] && [ $ELAPSED -gt 0 ]; then
          echo -e "${CYAN}🔄 State sync in progress... (${ELAPSED}s elapsed)${NC}"
        fi
        
        sleep 3
      done
    fi
  fi
  
  # Check if node started successfully
  if [ "$NODE_STARTED" = true ]; then
    # Ensure log directory exists and wire file logging
    LOG_DIR="$HOME/.pchain/logs"
    LOG_FILE="$LOG_DIR/pchaind.log"
    mkdir -p "$LOG_DIR"
    if [ ! -f "$LOG_FILE" ]; then
      echo -e "${YELLOW}⚠️ Logs not found at ${BOLD}$LOG_FILE${NC}"
      echo -e "${CYAN}🔧 Restarting node to enable file logging...${NC}"
      # Safe restart to attach stdout/stderr to log file via manager
      "$MANAGER_LINK" stop >/dev/null 2>&1 || true
      # Small delay to ensure clean shutdown
      sleep 1
      "$MANAGER_LINK" start >/dev/null 2>&1 || true
      # Verify log file appears (best-effort)
      for _ in {1..10}; do
        [ -f "$LOG_FILE" ] && break
        sleep 0.5
      done
      if [ -f "$LOG_FILE" ]; then
        echo -e "${GREEN}✅ Logging enabled: ${BOLD}$LOG_FILE${NC}"
      else
        echo -e "${YELLOW}⚠️ Log file still not present; use 'push-validator-manager logs' or 'status'${NC}"
      fi
    fi
    # Check if it's already synced - retry a few times in case node is still starting up
    SYNC_COMPLETE=false
    for i in {1..3}; do
      SYNC_STATUS=$("$MANAGER_LINK" status 2>/dev/null | grep -E "Status:|Catching Up:" || true)
      if echo "$SYNC_STATUS" | grep -q "Catching Up: false" || echo "$SYNC_STATUS" | grep -q "Fully Synced" || echo "$SYNC_STATUS" | grep -q "✅.*SYNCED"; then
        echo -e "${GREEN}✅ Node is fully synchronized!${NC}"
        SYNC_COMPLETE=true
        break
      fi
      [ $i -lt 3 ] && sleep 3
    done
    
    # Only show waiting message if state sync wasn't detected
    if [ "$SYNC_COMPLETE" = false ] && [ "$STATE_SYNC_DETECTED" != "true" ]; then
      echo -e "${CYAN}⏳ Node is synchronizing with the network...${NC}"
    fi
  else
    # Final attempt - check if node is actually running despite detection failure
    echo -e "${YELLOW}⚠️ Node startup detection failed, performing final check...${NC}"
    sleep 5
    
    FINAL_STATUS=$("$MANAGER_LINK" status 2>/dev/null || echo "")
    if echo "$FINAL_STATUS" | grep -q "Node is running"; then
      echo -e "${GREEN}✅ Node is actually running! Continuing...${NC}"
      NODE_STARTED=true
      
      # Quick sync status check
      if echo "$FINAL_STATUS" | grep -q "Fully Synced"; then
        echo -e "${GREEN}✅ Node is fully synchronized!${NC}"
        SYNC_COMPLETE=true
      else
        echo -e "${CYAN}⏳ Node is synchronizing with the network...${NC}"
        SYNC_COMPLETE=false
      fi
    else
      # Enhanced failure diagnosis
      echo -e "${RED}❌ Failed to start node${NC}"
      echo
      echo -e "${YELLOW}🔍 Diagnosis:${NC}"
      
      # Check if process is running even if status fails
      if pgrep -f "pchaind" >/dev/null 2>&1; then
        echo -e "  ${GREEN}✅ Node process is running${NC}"
        echo -e "  ${YELLOW}⚠️ Status command may be experiencing delays${NC}"
        echo -e "  ${CYAN}💡 Try: push-validator-manager status${NC}"
        echo -e "  ${CYAN}💡 Try: push-validator-manager logs${NC}"
        echo -e "  ${CYAN}💡 State sync may still be in progress${NC}"
        echo
        echo -e "${CYAN}Note: The node may actually be working. State sync can take 5-15 minutes.${NC}"
        echo -e "${CYAN}Check status in a few minutes with: push-validator-manager status${NC}"
        exit 0  # Don't fail - node may actually be working
      else
        echo -e "  ${RED}❌ No node process found${NC}"
        echo -e "  ${CYAN}💡 Check logs: push-validator-manager logs${NC}"
        echo -e "  ${CYAN}💡 Try manual start: push-validator-manager start${NC}"
        exit 1
      fi
    fi
  fi
  
  # Only monitor sync if not already complete
  if [ "$SYNC_COMPLETE" = false ]; then
    # Wait longer for node to stabilize
    sleep 2
    
    # Check sync status in a loop
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
      echo -e "${GREEN}✅ Node is fully synced!${NC}"
    fi
  fi
  
  # Show result and prompt for registration if synced
  if [ "$SYNC_COMPLETE" = true ]; then
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
