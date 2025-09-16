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
# Absolute directory of this script before any cd
SELF_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd -P)"

# Colors for output
CYAN='\033[0;36m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
MAGENTA='\033[0;35m'
BOLD='\033[1m'
NC='\033[0m'

# Spinner function for long-running operations
show_spinner() {
    local msg="${1:-Processing}"
    local spin='⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏'
    local i=0
    
    # Run in background subshell
    (
        while true; do
            printf "\r${CYAN}%s %s ${NC}" "$msg" "${spin:i++%${#spin}:1}"
            sleep 0.1
        done
    ) &
    
    # Return the PID so caller can kill it
    echo $!
}

# Run a command with a soft timeout (POSIX-friendly, no GNU timeout required)
# Usage: run_with_timeout <seconds> <cmd> [args...]
run_with_timeout() {
  local seconds="$1"; shift || true
  local tmp
  tmp="$(mktemp 2>/dev/null || echo "/tmp/pnm_timeout_$$")"
  ( "$@" >"$tmp" 2>&1 ) &
  local pid=$!
  local waited=0
  while kill -0 "$pid" 2>/dev/null && [ "$waited" -lt "$seconds" ]; do
    sleep 1
    waited=$((waited + 1))
  done
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    # Give it a moment to die
    sleep 1
    kill -9 "$pid" 2>/dev/null || true
  fi
  wait "$pid" 2>/dev/null || true
  cat "$tmp" 2>/dev/null || true
  rm -f "$tmp" 2>/dev/null || true
}

# Safe wrapper to get manager status without hanging
safe_status() {
  run_with_timeout 5 "$MANAGER_LINK" status || true
}

# Read env or defaults
MONIKER="${MONIKER:-push-validator}"
GENESIS_DOMAIN="${GENESIS_DOMAIN:-rpc-testnet-donut-node1.push.org}"
KEYRING_BACKEND="${KEYRING_BACKEND:-test}"
AUTO_START="yes"
RESET_DATA="${RESET_DATA:-yes}"  # Default to reset for clean installation
# Use local repository instead of cloning (for development/testing)
USE_LOCAL="no"
LOCAL_REPO=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-start) AUTO_START="no"; shift ;;
    --start) AUTO_START="yes"; shift ;;
    --no-reset) RESET_DATA="no"; shift ;;
    --reset) RESET_DATA="yes"; shift ;;
    --moniker) MONIKER="$2"; shift 2 ;;
    --genesis) GENESIS_DOMAIN="$2"; shift 2 ;;
    --keyring) KEYRING_BACKEND="$2"; shift 2 ;;
    --use-local) USE_LOCAL="yes"; shift ;;
    --local-repo)
      LOCAL_REPO="$2"; shift 2 ;;
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
    
    # Check if node is running and stop it first
    if pgrep -f "pchaind.*start.*--home.*$HOME/.pchain" >/dev/null 2>&1; then
        echo -e "${CYAN}  • Stopping running node for clean reset...${NC}"
        # Try to stop gracefully if manager exists
        if [[ -x "$HOME/.local/bin/push-validator-manager" ]]; then
            "$HOME/.local/bin/push-validator-manager" stop >/dev/null 2>&1 || true
        else
            # Force kill if manager not available
            pkill -f "pchaind.*start.*--home.*$HOME/.pchain" 2>/dev/null || true
        fi
        sleep 2  # Give it time to shut down
    fi
    
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

if [[ "$USE_LOCAL" = "yes" || -n "$LOCAL_REPO" ]]; then
  # Determine local repository path
  if [[ -n "$LOCAL_REPO" ]]; then
    REPO_DIR="$(cd "$LOCAL_REPO" && pwd -P)"
  else
    REPO_DIR="$(cd "$SELF_DIR/.." && pwd -P)"
  fi
  echo -e "${CYAN}🧪 Using local repository: $REPO_DIR${NC}"
  # Sanity check
  if [[ ! -d "$REPO_DIR/push-validator-manager" ]]; then
    echo -e "${RED}Error:${NC} expected directory not found: $REPO_DIR/push-validator-manager"
    echo "Run with --local-repo <path-to-repo-root> or invoke from a local checkout."
    exit 1
  fi
else
  # Always clone fresh repository to ensure latest version
  rm -rf "$REPO_DIR"
  echo -e "${CYAN}📥 Fetching Push Validator Manager (ref: $PNM_REF)...${NC}"
  git clone --quiet --depth 1 --branch "$PNM_REF" https://github.com/pushchain/push-chain-node "$REPO_DIR"
fi

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
  SYNC_COMPLETE=false
  
  # Start the node via the manager in a fully detached way to avoid piping signals
  # The manager itself backgrounds the daemon; we detach the manager invocation as well
  export SKIP_SYNC_MONITOR=true
  nohup "$MANAGER_LINK" start >/dev/null 2>&1 < /dev/null &
  
  # Check if node is running with multiple attempts - allow more time for state sync startup
  for i in {1..30}; do  # 30 attempts (60 seconds total)
    # First check if process exists (more reliable than status during init)
    if pgrep -f "pchaind.*start.*--home.*$HOME/.pchain" >/dev/null 2>&1; then
      # Process is running, now check if status reports correctly
      STATUS_OUTPUT=$(safe_status 2>&1)
      
      # Accept various states as "started"
      if echo "$STATUS_OUTPUT" | grep -qE "Node is running|initializing|Syncing|height"; then
        NODE_STARTED=true
        echo -e "${GREEN}✅ Node started successfully${NC}"
        break
      elif [ $i -gt 5 ]; then
        # After 10 seconds, if process exists, consider it started
        NODE_STARTED=true
        echo -e "${GREEN}✅ Node started successfully${NC}"
        break
      fi
      
      # After 10 attempts, try more aggressive status checking
      if [ $i -gt 10 ]; then
        # Check if RPC port is listening
        if lsof -i:26657 >/dev/null 2>&1; then
          # Give it a moment and try status again
          sleep 1
          STATUS_OUTPUT=$(safe_status || echo "status_failed")
          if echo "$STATUS_OUTPUT" | grep -q "Node is running\|Syncing\|height"; then
            NODE_STARTED=true
            echo -e "${GREEN}✅ Node started successfully${NC}"
            break
          fi
        fi
      fi
    fi
    
    [ $i -lt 30 ] && sleep 2
  done
  
  # Simple sync detection and monitoring
  if [ "$NODE_STARTED" = "true" ]; then
    # Ensure HOME_DIR is set
    HOME_DIR="${HOME_DIR:-$HOME/.pchain}"
    
    # Step 1: Check if state sync already happened
    if grep -q "Snapshot restored" "$HOME_DIR/logs/pchaind.log" 2>/dev/null; then
      echo -e "${GREEN}✅ State sync completed!${NC}"
      STATE_SYNC_DONE=true
    else
      # Wait for state sync to complete (with timeout)
      echo -e "${CYAN}⏳ Waiting for state sync...${NC}"
      wait_count=0
      while [ $wait_count -lt 60 ]; do
        if grep -q "Snapshot restored" "$HOME_DIR/logs/pchaind.log" 2>/dev/null; then
          echo -e "${GREEN}✅ State sync completed!${NC}"
          STATE_SYNC_DONE=true
          break
        fi
        sleep 2
        wait_count=$((wait_count + 2))
        # Show progress dots
        printf "."
      done
      echo  # New line after dots
    fi
    
    # Step 2: Monitor block sync using API
    echo -e "${CYAN}📡 Checking block sync status...${NC}"
    
    # Give node a moment to stabilize after state sync
    sleep 3
    
    # Check catching_up from API
    CATCHING_UP=$(curl -s http://localhost:26657/status 2>/dev/null | jq -r '.result.sync_info.catching_up' 2>/dev/null || echo "true")
    
    if [ "$CATCHING_UP" = "false" ]; then
      echo -e "${GREEN}✅ Node is fully synced!${NC}"
      SYNC_COMPLETE=true
    else
      # Show sync progress using WebSocket monitoring if available
      #echo -e "${CYAN}📊 Syncing blocks...${NC}"
      
      # Check if we have a WebSocket client
      if command -v websocat >/dev/null 2>&1; then
        # Use WebSocket monitoring for real-time updates
        MAX_WAIT=300  # 5 minutes max
        START_TIME=$(date +%s)
        
        # Start WebSocket subscription and monitor
        # Create a named pipe for clean shutdown
        PIPE_FILE="/tmp/installer_ws_$$"
        mkfifo "$PIPE_FILE" 2>/dev/null || true
        
        # Start WebSocket monitoring in a subshell to prevent pipeline failures from exiting script
        (
          # Start background process to keep stdin open
          (
            echo '{"jsonrpc":"2.0","method":"subscribe","params":{"query":"tm.event='\''NewBlockHeader'\''"},"id":1}'
            # Keep stdin open until pipe signals stop
            while [ ! -f "${PIPE_FILE}.stop" ]; do sleep 1; done
          ) | websocat -t --ping-interval 5 ws://localhost:26657/websocket 2>/dev/null | \
          while IFS= read -r line; do
          # Extract height from event
          height=$(echo "$line" | jq -r '.result.data.value.header.height // empty' 2>/dev/null)
          
          if [ -n "$height" ] && [ "$height" != "null" ]; then
            # Get current sync status
            NODE_STATUS=$(curl -s localhost:26657/status 2>/dev/null)
            CATCHING_UP=$(echo "$NODE_STATUS" | jq -r '.result.sync_info.catching_up' 2>/dev/null || echo "true")
            LOCAL_HEIGHT=$(echo "$NODE_STATUS" | jq -r '.result.sync_info.latest_block_height // "0"' 2>/dev/null || echo "0")
            
            # Get remote height for progress (use configured RPC)
            GENESIS_RPC="https://${GENESIS_DOMAIN:-rpc-testnet-donut-node1.push.org}"
            REMOTE_STATUS=$(curl -s "$GENESIS_RPC/status" 2>/dev/null)
            REMOTE_HEIGHT=$(echo "$REMOTE_STATUS" | jq -r '.result.sync_info.latest_block_height // "0"' 2>/dev/null || echo "0")
            
            # Calculate progress and bar
            if [ "$REMOTE_HEIGHT" -gt 0 ] && [ "$LOCAL_HEIGHT" -gt 0 ]; then
              PROGRESS_PERCENT=$(awk "BEGIN { printf \"%.2f\", ($LOCAL_HEIGHT / $REMOTE_HEIGHT) * 100 }")
              
              # Create progress bar (30 chars wide)
              BAR_WIDTH=30
              FILLED=$(awk "BEGIN { printf \"%.0f\", ($LOCAL_HEIGHT / $REMOTE_HEIGHT) * $BAR_WIDTH }")
              EMPTY=$((BAR_WIDTH - FILLED))
              
              # Build the bar
              BAR="${GREEN}"
              for ((i=0; i<FILLED; i++)); do BAR="${BAR}█"; done
              BAR="${BAR}${NC}\033[90m"
              for ((i=0; i<EMPTY; i++)); do BAR="${BAR}░"; done
              BAR="${BAR}${NC}"
            else
              PROGRESS_PERCENT="?"
              BAR="\033[90m░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░${NC}"
            fi
            
            # Determine status and display
            if [ "$CATCHING_UP" = "false" ]; then
              printf "\r${GREEN}✅ SYNCED${NC} [${BAR}] ${GREEN}100%%${NC} | ${MAGENTA}%s${NC}/${MAGENTA}%s${NC} blocks\033[K\n" "$LOCAL_HEIGHT" "$REMOTE_HEIGHT"
              SYNC_COMPLETE=true
              # Signal background process to stop before breaking
              touch "${PIPE_FILE}.stop" 2>/dev/null || true
              sleep 0.5  # Give background process time to exit cleanly
              break
            else
              printf "\r${YELLOW}⏳ SYNCING${NC} [${BAR}] ${GREEN}%s%%${NC} | ${MAGENTA}%s${NC}/${MAGENTA}%s${NC} blocks\033[K" "$PROGRESS_PERCENT" "$LOCAL_HEIGHT" "$REMOTE_HEIGHT"
            fi
            
            # Check timeout
            CURRENT_TIME=$(date +%s)
            ELAPSED=$((CURRENT_TIME - START_TIME))
            if [ $ELAPSED -gt $MAX_WAIT ]; then
              echo  # New line
              echo -e "${YELLOW}⚠️ Sync monitoring timeout${NC}"
              # Signal background process to stop before breaking
              touch "${PIPE_FILE}.stop" 2>/dev/null || true
              sleep 0.5  # Give background process time to exit cleanly
              break
            fi
          fi
          done
        ) || true  # Ignore pipeline failures when breaking from loop
        
        # Clean shutdown: signal the background process to stop and cleanup
        (
          touch "${PIPE_FILE}.stop" 2>/dev/null || true
          sleep 0.5  # Give background process time to exit cleanly
          
          # Kill the websocat background process if still running
          pkill -f "websocat.*ws://localhost:26657/websocket" 2>/dev/null || true
          
          # Clean up pipe files
          rm -f "$PIPE_FILE" "${PIPE_FILE}.stop" 2>/dev/null || true
        ) || true
        
      else
        # Fallback to using the sync command if no WebSocket client
        "$MANAGER_LINK" sync &
        SYNC_PID=$!
        
        # Monitor for up to 5 minutes
        MAX_WAIT=300
        WAIT_TIME=0
        
        while [ $WAIT_TIME -lt $MAX_WAIT ]; do
          # Check if sync is complete
          CATCHING_UP=$(curl -s localhost:26657/status 2>/dev/null | jq -r '.result.sync_info.catching_up' 2>/dev/null || echo "true")
          
          if [ "$CATCHING_UP" = "false" ]; then
            # Sync complete, kill the sync command
            kill $SYNC_PID 2>/dev/null || true
            wait $SYNC_PID 2>/dev/null || true
            SYNC_COMPLETE=true
            break
          fi
          
          sleep 3
          WAIT_TIME=$((WAIT_TIME + 3))
        done
        
        # Ensure sync command is killed if timeout
        kill $SYNC_PID 2>/dev/null || true
        wait $SYNC_PID 2>/dev/null || true
      fi
      
      echo  # Add newline after progress
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
    # Only reset SYNC_COMPLETE if not already set by state sync monitoring
    if [ "$SYNC_COMPLETE" != true ]; then
      SYNC_COMPLETE=false
    fi
    for i in {1..3}; do
      SYNC_STATUS=$("$MANAGER_LINK" status 2>/dev/null | grep -E "Status:|Catching Up:" || true)
      if echo "$SYNC_STATUS" | grep -q "Catching Up: false" || echo "$SYNC_STATUS" | grep -q "Fully Synced" || echo "$SYNC_STATUS" | grep -q "✅.*SYNCED"; then
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
    sleep 5
    FINAL_STATUS=$("$MANAGER_LINK" status 2>/dev/null || echo "")
    if echo "$FINAL_STATUS" | grep -q "Node is running"; then
      echo -e "${GREEN}✅ Node is running${NC}"
      NODE_STARTED=true
      
      # Quick sync status check
      if echo "$FINAL_STATUS" | grep -q "Fully Synced"; then
        SYNC_COMPLETE=true
      else
        echo -e "${CYAN}⏳ Node is synchronizing with the network...${NC}"
        SYNC_COMPLETE=false
      fi
    else
      # Final check for process
      if pgrep -f "pchaind.*start.*--home.*$HOME/.pchain" >/dev/null 2>&1; then
        echo -e "${GREEN}✅ Node process is running${NC}"
        echo -e "${CYAN}⏳ Node is initializing. This may take 5-15 minutes for state sync.${NC}"
        NODE_STARTED=true
      else
        echo -e "${RED}❌ Failed to start node${NC}"
        echo -e "${CYAN}Check logs: push-validator-manager logs${NC}"
        echo -e "${CYAN}Try manual start: push-validator-manager start${NC}"
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
      SYNC_STATUS=$(safe_status | grep -E "Catching Up|Block Height" || true)
      
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
    echo -e "${GREEN}✅ Ready to become a validator!${NC}"
    echo
    echo -e "${YELLOW}Register as a validator now? (y/N)${NC}"
    read -r -p "> " REGISTER_NOW
      
      if [[ "$REGISTER_NOW" =~ ^[Yy]$ ]]; then
        echo
        "$MANAGER_LINK" register-validator
      else
        echo
        echo -e "${BOLD}Next steps:${NC}"
        echo -e "1. Get test tokens from: ${GREEN}https://faucet.push.org${NC}"
        echo -e "2. Register as validator: ${GREEN}push-validator-manager register-validator${NC}"
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
if [[ "$USE_LOCAL" != "yes" && -z "$LOCAL_REPO" ]]; then
  cd "$ROOT_DIR"
  if [[ -d "$REPO_DIR" ]]; then
      # Remove the temporary clone
      rm -rf "$REPO_DIR"
  fi
fi
