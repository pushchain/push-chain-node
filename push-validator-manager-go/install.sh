#!/usr/bin/env bash
# Push Validator Manager (Go) — Installer with local/clone build + guided start
# Examples:
#   bash install.sh                            # default: reset data, build if needed, init+start, wait for sync
#   bash install.sh --no-reset --no-start      # install only
#   bash install.sh --use-local                # use current repo checkout to build
#   PNM_REF=feature/pnm bash install.sh         # clone specific ref (branch/tag)

set -euo pipefail
IFS=$'\n\t'

# Styling and output functions
CYAN='\033[0;36m'; GREEN='\033[0;32m'; YELLOW='\033[0;33m'; RED='\033[0;31m'; BOLD='\033[1m'; DIM='\033[2m'; NC='\033[0m'
NO_COLOR="${NO_COLOR:-}"
VERBOSE="${VERBOSE:-no}"

# Disable colors if NO_COLOR is set or not a terminal
if [[ -n "$NO_COLOR" ]] || [[ ! -t 1 ]]; then
    CYAN=''; GREEN=''; YELLOW=''; RED=''; BOLD=''; DIM=''; NC=''
fi

status() { echo -e "${CYAN}$*${NC}"; }
ok()     {
    if [[ $PHASE_START_TIME -gt 0 ]]; then
        local delta=$(($(date +%s) - PHASE_START_TIME))
        local unit="s"
        local time_val=$delta
        # Show milliseconds for sub-second times
        if [[ $delta -eq 0 ]]; then
            time_val="<1"
            unit="s"
        fi
        echo -e "${GREEN}✓ $* (${time_val}${unit})${NC}"
    else
        echo -e "${GREEN}✓ $*${NC}"
    fi
}
warn()   { echo -e "${YELLOW}⚠ $*${NC}"; }
err()    { echo -e "${RED}✗ $*${NC}"; }
phase()  { echo -e "\n${BOLD}${CYAN}▸ $*${NC}"; }
step()   { echo -e "  ${DIM}→${NC} $*"; }
verbose() { [[ "$VERBOSE" = "yes" ]] && echo -e "  ${DIM}$*${NC}" || true; }

# Helper: Find timeout command (macOS needs gtimeout)
timeout_cmd() {
    if command -v timeout >/dev/null 2>&1; then
        echo "timeout"
    elif command -v gtimeout >/dev/null 2>&1; then
        echo "gtimeout"
    else
        echo ""
    fi
}

# Helper: Check if node is running
node_running() {
    local TO; TO=$(timeout_cmd)
    local status_json
    if [[ -n "$TO" ]]; then
        status_json=$($TO 2 "$MANAGER_BIN" status --output json 2>/dev/null || echo "{}")
    else
        status_json=$("$MANAGER_BIN" status --output json 2>/dev/null || echo "{}")
    fi

    if command -v jq >/dev/null 2>&1; then
        echo "$status_json" | jq -er '.node.running // .running // false' >/dev/null 2>&1 && return 0 || return 1
    else
        echo "$status_json" | grep -q '"running"[[:space:]]*:[[:space:]]*true' && return 0 || return 1
    fi
}

# Helper: Check if current node consensus key already exists in validator set
node_is_validator() {
    local result
    if ! result=$("$MANAGER_BIN" register-validator --check-only --output json 2>/dev/null); then
        return 1
    fi
    if command -v jq >/dev/null 2>&1; then
        local flag
        flag=$(echo "$result" | jq -r '.registered // false' 2>/dev/null || echo "false")
        [[ "$flag" == "true" ]] && return 0 || return 1
    else
        echo "$result" | grep -q '"registered"[[:space:]]*:[[:space:]]*true' && return 0 || return 1
    fi
}

# Helper: Print useful commands
print_useful_cmds() {
    echo
    echo "Useful commands:"
    echo "  push-validator-manager status          # Check node status"
    echo "  push-validator-manager logs            # View logs"
    echo "  push-validator-manager stop            # Stop the node"
    echo "  push-validator-manager restart         # Restart the node"
    echo "  push-validator-manager register-validator  # Register as validator"
    echo
}

# Phase tracking with timing
INSTALL_START_TIME=$(date +%s)
PHASE_NUM=0
TOTAL_PHASES=6  # Will be adjusted based on what's needed
PHASE_START_TIME=0
next_phase() {
    ((PHASE_NUM++))
    PHASE_START_TIME=$(date +%s)
    phase "[$PHASE_NUM/$TOTAL_PHASES] $1"
}

# Script location (works when piped or invoked directly)
if [ -n "${BASH_SOURCE+x}" ]; then SCRIPT_SOURCE="${BASH_SOURCE[0]}"; else SCRIPT_SOURCE="$0"; fi
SELF_DIR="$(cd -- "$(dirname -- "$SCRIPT_SOURCE")" >/dev/null 2>&1 && pwd -P || pwd)"

# Defaults (overridable via env)
MONIKER="${MONIKER:-push-validator}"
GENESIS_DOMAIN="${GENESIS_DOMAIN:-rpc-testnet-donut-node1.push.org}"
KEYRING_BACKEND="${KEYRING_BACKEND:-test}"
CHAIN_ID="${CHAIN_ID:-push_42101-1}"
SNAPSHOT_RPC="${SNAPSHOT_RPC:-https://rpc-testnet-donut-node2.push.org}"
RESET_DATA="${RESET_DATA:-yes}"
AUTO_START="${AUTO_START:-yes}"
PNM_REF="${PNM_REF:-feature/pnm}"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
PREFIX="${PREFIX:-}"

# Flags
USE_LOCAL="no"
LOCAL_REPO=""
PCHAIND="${PCHAIND:-}"
PCHAIND_REF="${PCHAIND_REF:-}"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-start) AUTO_START="no"; shift ;;
    --start) AUTO_START="yes"; shift ;;
    --no-reset) RESET_DATA="no"; shift ;;
    --reset) RESET_DATA="yes"; shift ;;
    --verbose) VERBOSE="yes"; shift ;;
    --no-color) NO_COLOR="1"; shift ;;
    --bin-dir) BIN_DIR="$2"; shift 2 ;;
    --prefix) PREFIX="$2"; shift 2 ;;
    --moniker) MONIKER="$2"; shift 2 ;;
    --genesis) GENESIS_DOMAIN="$2"; shift 2 ;;
    --keyring) KEYRING_BACKEND="$2"; shift 2 ;;
    --chain-id) CHAIN_ID="$2"; shift 2 ;;
    --snapshot-rpc) SNAPSHOT_RPC="$2"; shift 2 ;;
    --pchaind-ref) PCHAIND_REF="$2"; shift 2 ;;
    --use-local) USE_LOCAL="yes"; shift ;;
    --local-repo) LOCAL_REPO="$2"; shift 2 ;;
    --help)
      echo "Push Validator Manager (Go) - Installer"
      echo
      echo "Usage: bash install.sh [OPTIONS]"
      echo
      echo "Installation Options:"
      echo "  --use-local          Use current repository checkout to build"
      echo "  --local-repo DIR     Use specific local repository directory"
      echo "  --bin-dir DIR        Install binaries to DIR (default: ~/.local/bin)"
      echo "  --prefix DIR         Use DIR as installation prefix (sets data dir)"
      echo
      echo "Node Configuration:"
      echo "  --moniker NAME       Set validator moniker (default: push-validator)"
      echo "  --chain-id ID        Set chain ID (default: push_42101-1)"
      echo "  --genesis DOMAIN     Genesis domain (default: rpc-testnet-donut-node1.push.org)"
      echo "  --snapshot-rpc URL   Snapshot RPC URL (default: https://rpc-testnet-donut-node2.push.org)"
      echo "  --keyring BACKEND    Keyring backend (default: test)"
      echo
      echo "Build Options:"
      echo "  --pchaind-ref REF    Build pchaind from specific git ref/branch/tag"
      echo
      echo "Behavior Options:"
      echo "  --reset              Reset all data (default)"
      echo "  --no-reset           Keep existing data"
      echo "  --start              Start node after installation (default)"
      echo "  --no-start           Install only, don't start"
      echo
      echo "Output Options:"
      echo "  --verbose            Show verbose output"
      echo "  --no-color           Disable colored output"
      echo
      echo "Environment Variables:"
      echo "  NO_COLOR             Set to disable colors"
      echo "  VERBOSE              Set to 'yes' for verbose output"
      echo "  PNM_REF              Git ref for push-validator-manager-go (default: feature/pnm)"
      echo "  PCHAIND_REF          Git ref for pchaind binary"
      echo
      echo "Examples:"
      echo "  bash install.sh --use-local --verbose"
      echo "  bash install.sh --no-reset --no-start"
      echo "  bash install.sh --bin-dir /usr/local/bin --prefix /opt/pchain"
      echo "  PNM_REF=main bash install.sh"
      exit 0
      ;;
    *) err "Unknown flag: $1 (use --help for usage)"; exit 2 ;;
  esac
done

# Paths
if [[ -n "$PREFIX" ]]; then
  ROOT_DIR="$PREFIX/share/push-validator-manager"
  INSTALL_BIN_DIR="$PREFIX/bin"  # --prefix overrides BIN_DIR for relocatable installs
  HOME_DIR="${HOME_DIR:-$PREFIX/data}"
else
  if [[ -n "${XDG_DATA_HOME:-}" ]]; then ROOT_DIR="$XDG_DATA_HOME/push-validator-manager"; else ROOT_DIR="$HOME/.local/share/push-validator-manager"; fi
  INSTALL_BIN_DIR="$BIN_DIR"
  HOME_DIR="${HOME_DIR:-$HOME/.pchain}"
fi
REPO_DIR="$ROOT_DIR/repo"
MANAGER_BIN="$INSTALL_BIN_DIR/push-validator-manager"

# Detect what phases are needed BEFORE creating directories
HAS_RUNNING_NODE="no"
HAS_EXISTING_INSTALL="no"

# Check if node is running or processes exist
if [[ -x "$MANAGER_BIN" ]] && command -v "$MANAGER_BIN" >/dev/null 2>&1; then
  # Manager exists, check if node is actually running via status
  STATUS_JSON=$("$MANAGER_BIN" status --output json 2>/dev/null || echo "{}")
  if echo "$STATUS_JSON" | grep -q '"running"[[:space:]]*:[[:space:]]*true'; then
    HAS_RUNNING_NODE="yes"
  fi
elif pgrep -x pchaind >/dev/null 2>&1 || pgrep -x push-validator-manager >/dev/null 2>&1; then
  HAS_RUNNING_NODE="yes"
fi

# Check if installation exists (check for actual installation artifacts, not just config)
if [[ -d "$ROOT_DIR" ]] || [[ -x "$MANAGER_BIN" ]]; then
  HAS_EXISTING_INSTALL="yes"
elif [[ -d "$HOME_DIR/data" ]] && [[ -n "$(ls -A "$HOME_DIR/data" 2>/dev/null)" ]]; then
  # Only count as existing if data directory has content
  HAS_EXISTING_INSTALL="yes"
fi

mkdir -p "$ROOT_DIR" "$INSTALL_BIN_DIR"

verbose "Installation paths:"
verbose "  Root dir: $ROOT_DIR"
verbose "  Bin dir: $INSTALL_BIN_DIR"
verbose "  Home dir: $HOME_DIR"

need() { command -v "$1" >/dev/null 2>&1 || { err "Missing dependency: $1"; exit 1; }; }
need git; need go

# Validate Go version (requires 1.23+ for pchaind build)
GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
GO_MAJOR=$(echo "$GO_VERSION" | cut -d. -f1)
GO_MINOR=$(echo "$GO_VERSION" | cut -d. -f2)

if [[ "$GO_MAJOR" -lt 1 ]] || [[ "$GO_MAJOR" -eq 1 && "$GO_MINOR" -lt 23 ]]; then
  err "Go 1.23 or higher is required (found: $GO_VERSION)"
  echo
  echo "The pchaind binary requires Go 1.23+ to build."
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
verbose "Go version check passed: $GO_VERSION"

# Optional dependencies (warn if missing, fallbacks exist)
if ! command -v jq >/dev/null 2>&1; then
  warn "jq not found; JSON parsing will be less robust (using grep fallback)"
fi
TO_CMD=$(timeout_cmd)
if [[ -z "$TO_CMD" ]]; then
  warn "timeout/gtimeout not found; RPC checks may block longer than expected"
fi

# Store environment info (will print after manager is built)
OS_NAME=$(uname -s | tr '[:upper:]' '[:lower:]')
OS_ARCH=$(uname -m)
GO_VER=$(go version | awk '{print $3}' | sed 's/go//')
TIMESTAMP=$(date '+%Y-%m-%d %H:%M:%S')

# Calculate total phases needed (detection already done above before mkdir)
TOTAL_PHASES=4  # Base: Install Manager, Build Chain, Init, Start
if [[ "$HAS_RUNNING_NODE" = "yes" ]]; then
  ((TOTAL_PHASES++))  # Add stopping phase
fi
if [[ "$RESET_DATA" = "yes" ]] && [[ "$HAS_EXISTING_INSTALL" = "yes" ]]; then
  ((TOTAL_PHASES++))  # Add cleaning phase
fi

verbose "Phases needed: $TOTAL_PHASES (running=$HAS_RUNNING_NODE, existing=$HAS_EXISTING_INSTALL)"

# Print installation banner
echo
echo "═══════════════════════════════════════════════════════════════"
echo "  Push Chain Validator Node - Installation"
echo "═══════════════════════════════════════════════════════════════"
echo

# Stop any running processes first (only if needed)
if [[ "$HAS_RUNNING_NODE" = "yes" ]]; then
  next_phase "Stopping Validator Processes"
if [[ -x "$MANAGER_BIN" ]]; then
  step "Stopping manager gracefully"
  "$MANAGER_BIN" stop >/dev/null 2>&1 || true
  sleep 2
fi
# Kill any remaining pchaind processes
step "Cleaning up remaining processes"

# Try graceful PID-based approach first
if [[ -x "$MANAGER_BIN" ]]; then
  TO_CMD=$(timeout_cmd)
  if [[ -n "$TO_CMD" ]]; then
    STATUS_JSON=$($TO_CMD 5 "$MANAGER_BIN" status --output json 2>/dev/null || echo "{}")
  else
    STATUS_JSON=$("$MANAGER_BIN" status --output json 2>/dev/null || echo "{}")
  fi
  if command -v jq >/dev/null 2>&1; then
    PID=$(echo "$STATUS_JSON" | jq -r '.node.pid // .pid // empty' 2>/dev/null)
  else
    PID=$(echo "$STATUS_JSON" | grep -o '"pid"[[:space:]]*:[[:space:]]*[0-9]*' | grep -o '[0-9]*$')
  fi
  if [[ -n "$PID" && "$PID" =~ ^[0-9]+$ ]]; then
    kill -TERM "$PID" 2>/dev/null || true
    sleep 1
    kill -KILL "$PID" 2>/dev/null || true
  fi
fi

# Fallback: use pkill with exact name matching (POSIX-portable)
pkill -x pchaind 2>/dev/null || true
pkill -x push-validator-manager 2>/dev/null || true
sleep 1
ok "Stopped all validator processes"
else
  verbose "No running processes to stop (skipped)"
fi

# Clean install: remove all previous installation artifacts (preserve wallets and validator keys)
if [[ "$RESET_DATA" = "yes" ]] && [[ "$HAS_EXISTING_INSTALL" = "yes" ]]; then
  next_phase "Cleaning Installation"

  # Backup all keyring directories
  WALLET_BACKUP="/tmp/pchain-wallet-backup-$$"
  if [[ -d "$HOME_DIR" ]]; then
    step "Backing up wallet keys (your account credentials)"
    mkdir -p "$WALLET_BACKUP"
    for keyring_dir in "$HOME_DIR/keyring-"*; do
      if [[ -d "$keyring_dir" ]]; then
        cp -r "$keyring_dir" "$WALLET_BACKUP/" 2>/dev/null || true
      fi
    done
    if [[ -n "$(ls -A "$WALLET_BACKUP" 2>/dev/null)" ]]; then
      ok "Wallets backed up"
    fi
  fi

  # Backup validator identity keys
  VALIDATOR_BACKUP="/tmp/pchain-validator-backup-$$"
  if [[ -d "$HOME_DIR/config" ]]; then
    step "Backing up validator keys"
    mkdir -p "$VALIDATOR_BACKUP"
    cp "$HOME_DIR/config/priv_validator_key.json" "$VALIDATOR_BACKUP/" 2>/dev/null || true
    cp "$HOME_DIR/config/node_key.json" "$VALIDATOR_BACKUP/" 2>/dev/null || true
    if [[ -n "$(ls -A "$VALIDATOR_BACKUP" 2>/dev/null)" ]]; then
      ok "Validator keys backed up"
    fi
  fi

  # Clean installation (selective - preserves validator identity)
  step "Removing old installation"
  rm -rf "$ROOT_DIR" 2>/dev/null || true
  # Delete only data directories to clear corrupted state
  rm -rf "$HOME_DIR/data/application.db" 2>/dev/null || true
  rm -rf "$HOME_DIR/data/wasm" 2>/dev/null || true
  rm -rf "$HOME_DIR/data/blockstore.db" 2>/dev/null || true
  rm -rf "$HOME_DIR/data/state.db" 2>/dev/null || true
  rm -rf "$HOME_DIR/data/evidence.db" 2>/dev/null || true
  rm -rf "$HOME_DIR/data/tx_index.db" 2>/dev/null || true
  rm -rf "$HOME_DIR/data/08-light-client" 2>/dev/null || true
  rm -f "$HOME_DIR/data/priv_validator_state.json" 2>/dev/null || true
  rm -f "$MANAGER_BIN" 2>/dev/null || true
  rm -f "$INSTALL_BIN_DIR/pchaind" 2>/dev/null || true

  # Delete config files to force fresh generation (validator keys backed up separately)
  rm -f "$HOME_DIR/config/config.toml" 2>/dev/null || true
  rm -f "$HOME_DIR/config/app.toml" 2>/dev/null || true
  rm -f "$HOME_DIR/config/addrbook.json" 2>/dev/null || true
  rm -f "$HOME_DIR/config/genesis.json" 2>/dev/null || true
  rm -f "$HOME_DIR/config/config.toml."*.bak 2>/dev/null || true

  # Delete stale state sync and consensus data
  rm -rf "$HOME_DIR/data/snapshots" 2>/dev/null || true
  rm -rf "$HOME_DIR/data/cs.wal" 2>/dev/null || true

  # Delete state sync marker and stale PID
  rm -f "$HOME_DIR/.initial_state_sync" 2>/dev/null || true
  rm -f "$HOME_DIR/pchaind.pid" 2>/dev/null || true

  # Restore wallets if backed up
  if [[ -d "$WALLET_BACKUP" && -n "$(ls -A "$WALLET_BACKUP" 2>/dev/null)" ]]; then
    step "Restoring wallets"
    mkdir -p "$HOME_DIR"
    cp -r "$WALLET_BACKUP/"* "$HOME_DIR/" 2>/dev/null || true
    rm -rf "$WALLET_BACKUP"
    ok "Wallets restored"
  fi

  # Restore validator keys if backed up
  if [[ -d "$VALIDATOR_BACKUP" && -n "$(ls -A "$VALIDATOR_BACKUP" 2>/dev/null)" ]]; then
    step "Restoring validator keys"
    mkdir -p "$HOME_DIR/config"
    cp -r "$VALIDATOR_BACKUP/"* "$HOME_DIR/config/" 2>/dev/null || true
    rm -rf "$VALIDATOR_BACKUP"
    ok "Validator keys restored"
  fi

  ok "Clean installation ready"
elif [[ "$RESET_DATA" = "yes" ]]; then
  verbose "Fresh installation detected (skipped cleanup)"
else
  verbose "Skipping data reset (--no-reset)"
fi

next_phase "Installing Validator Manager"
verbose "Target directory: $ROOT_DIR"

# Determine repo source
if [[ "$USE_LOCAL" = "yes" || -n "$LOCAL_REPO" ]]; then
  if [[ -n "$LOCAL_REPO" ]]; then REPO_DIR="$(cd "$LOCAL_REPO" && pwd -P)"; else REPO_DIR="$(cd "$SELF_DIR/.." && pwd -P)"; fi
  step "Using local repository: $REPO_DIR"
  if [[ ! -f "$REPO_DIR/push-validator-manager-go/go.mod" ]]; then
    err "Expected Go module not found at: $REPO_DIR/push-validator-manager-go"; exit 1
  fi
else
  rm -rf "$REPO_DIR"
  step "Cloning push-chain-node (ref: $PNM_REF)"
  git clone --quiet --depth 1 --branch "$PNM_REF" https://github.com/pushchain/push-chain-node "$REPO_DIR"
fi

# Build manager from source (ensures latest + no external runtime deps)
if [[ ! -d "$REPO_DIR/push-validator-manager-go" ]]; then
  err "Expected directory missing: $REPO_DIR/push-validator-manager-go"
  warn "The cloned ref ('$PNM_REF') may not include the Go module yet."
  # Suggest local usage if available
  LOCAL_CANDIDATE="$(cd "$SELF_DIR/.." 2>/dev/null && pwd -P || true)"
  if [[ -n "$LOCAL_CANDIDATE" && -d "$LOCAL_CANDIDATE/push-validator-manager-go" ]]; then
    warn "Try: bash push-validator-manager-go/install.sh --use-local"
  fi
  warn "Or specify a branch/tag that contains it: PNM_REF=feature/pnm bash push-validator-manager-go/install.sh"
  exit 1
fi

# Check if already up-to-date (idempotent install)
SKIP_BUILD=no
if [[ -x "$MANAGER_BIN" ]]; then
  CURRENT_COMMIT=$(cd "$REPO_DIR/push-validator-manager-go" && git rev-parse --short HEAD 2>/dev/null || echo "unknown")
  # Extract commit from version output (format: "push-validator-manager vX.Y.Z (1f599bd) built ...")
  INSTALLED_COMMIT=$("$MANAGER_BIN" version 2>/dev/null | sed -n 's/.*(\([0-9a-f]\{7,\}\)).*/\1/p')
  # Only skip build if both are valid hex commits and match
  if [[ "$CURRENT_COMMIT" =~ ^[0-9a-f]+$ ]] && [[ "$INSTALLED_COMMIT" =~ ^[0-9a-f]+$ ]] && [[ "$CURRENT_COMMIT" == "$INSTALLED_COMMIT" ]]; then
    step "Manager already up-to-date ($CURRENT_COMMIT) - skipped"
    SKIP_BUILD=yes
  fi
fi

if [[ "$SKIP_BUILD" = "no" ]]; then
  step "Building Push Validator Manager binary"
  pushd "$REPO_DIR/push-validator-manager-go" >/dev/null

  # Build version information
  VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "v1.0.0")}
  COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
  BUILD_DATE=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
  LDFLAGS="-X main.Version=$VERSION -X main.Commit=$COMMIT -X main.BuildDate=$BUILD_DATE"

  GOFLAGS="-trimpath" CGO_ENABLED=0 go build -mod=mod -ldflags="$LDFLAGS" -o "$MANAGER_BIN" ./cmd/push-validator-manager
  popd >/dev/null
  chmod +x "$MANAGER_BIN"

  # Compute and display SHA256
  if command -v sha256sum >/dev/null 2>&1; then
    MANAGER_SHA=$(sha256sum "$MANAGER_BIN" 2>/dev/null | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    MANAGER_SHA=$(shasum -a 256 "$MANAGER_BIN" 2>/dev/null | awk '{print $1}')
  fi
  if [[ -n "$MANAGER_SHA" ]]; then
    SHA_SHORT="${MANAGER_SHA:0:8}...${MANAGER_SHA: -8}"
    ok "Built push-validator-manager (SHA256: $SHA_SHORT)"
  else
    ok "Built push-validator-manager"
  fi
fi

ok "Manager installed: $MANAGER_BIN"

# Print environment banner now that manager is built
MANAGER_VER_BANNER="dev unknown"
if [[ -x "$MANAGER_BIN" ]]; then
  # Parse full version output: "push-validator-manager v1.0.0 (abc1234) built 2025-01-08"
  MANAGER_FULL=$("$MANAGER_BIN" version 2>/dev/null || echo "unknown")
  if [[ "$MANAGER_FULL" != "unknown" ]]; then
    MANAGER_VER_BANNER=$(echo "$MANAGER_FULL" | awk '{print $2, $3}' | sed 's/[()]//g')
  fi
fi
echo
echo -e "${BOLD}Environment:${NC} ${OS_NAME}/${OS_ARCH} | Go ${GO_VER} | Manager ${MANAGER_VER_BANNER} | ${TIMESTAMP}"
echo

# Ensure PATH for current session
case ":$PATH:" in *":$INSTALL_BIN_DIR:"*) : ;; *) export PATH="$INSTALL_BIN_DIR:$PATH" ;; esac

# Persist PATH in common shell config files
SHELL_CONFIG=""
if [[ -f "$HOME/.zshrc" ]]; then SHELL_CONFIG="$HOME/.zshrc"; elif [[ -f "$HOME/.bashrc" ]]; then SHELL_CONFIG="$HOME/.bashrc"; elif [[ -f "$HOME/.bash_profile" ]]; then SHELL_CONFIG="$HOME/.bash_profile"; fi
if [[ -n "$SHELL_CONFIG" ]] && ! grep -q "$INSTALL_BIN_DIR" "$SHELL_CONFIG" 2>/dev/null; then
  echo "" >> "$SHELL_CONFIG"
  echo "# Push Validator Manager (Go)" >> "$SHELL_CONFIG"
  echo "export PATH=\"$INSTALL_BIN_DIR:\$PATH\"" >> "$SHELL_CONFIG"
fi

next_phase "Building Chain Binary"

# Build or select pchaind (prefer locally built binary to match network upgrades)
BUILD_SCRIPT="$REPO_DIR/push-validator-manager-go/scripts/build-pchaind.sh"
if [[ -f "$BUILD_SCRIPT" ]]; then
  step "Building Push Chain binary (Push Node Daemon) from source"
  # Build from repo (whether local or cloned)
  BUILD_OUTPUT="$REPO_DIR/push-validator-manager-go/scripts/build"
  if bash "$BUILD_SCRIPT" "$REPO_DIR" "$BUILD_OUTPUT"; then
    if [[ -f "$BUILD_OUTPUT/pchaind" ]]; then
      mkdir -p "$INSTALL_BIN_DIR"
      ln -sf "$BUILD_OUTPUT/pchaind" "$INSTALL_BIN_DIR/pchaind"
      export PCHAIND="$INSTALL_BIN_DIR/pchaind"

      # Compute and display SHA256
      if command -v sha256sum >/dev/null 2>&1; then
        PCHAIND_SHA=$(sha256sum "$BUILD_OUTPUT/pchaind" 2>/dev/null | awk '{print $1}')
      elif command -v shasum >/dev/null 2>&1; then
        PCHAIND_SHA=$(shasum -a 256 "$BUILD_OUTPUT/pchaind" 2>/dev/null | awk '{print $1}')
      fi
      if [[ -n "$PCHAIND_SHA" ]]; then
        SHA_SHORT="${PCHAIND_SHA:0:8}...${PCHAIND_SHA: -8}"
        ok "Push Chain binary ready (SHA256: $SHA_SHORT)"
      else
        ok "Push Chain binary ready: $PCHAIND"
      fi
    else
      warn "Build completed but binary not found at expected location"
    fi
  else
    warn "Build failed; trying fallback options"
  fi
fi

# Final fallback to system pchaind
if [[ -z "$PCHAIND" || ! -f "$PCHAIND" ]]; then
  if command -v pchaind >/dev/null 2>&1; then
    step "Using system Push Node Daemon binary"
    export PCHAIND="$(command -v pchaind)"
    ok "Found existing Push Node Daemon: $PCHAIND"
  else
    err "Push Node Daemon (pchaind) not found"
    err "Build failed and no system binary available"
    err "Please ensure the build script works or install manually"
    exit 1
  fi
fi

verbose "Using built-in WebSocket monitor (no external dependency)"

if [[ "$AUTO_START" = "yes" ]]; then
  next_phase "Initializing Node"
  # Initialize if config or genesis missing
  if [[ ! -f "$HOME_DIR/config/config.toml" ]] || [[ ! -f "$HOME_DIR/config/genesis.json" ]]; then
    step "Configuring node"
    "$MANAGER_BIN" init \
      --moniker "$MONIKER" \
      --home "$HOME_DIR" \
      --chain-id "$CHAIN_ID" \
      --genesis-domain "$GENESIS_DOMAIN" \
      --snapshot-rpc "$SNAPSHOT_RPC" \
      --bin "${PCHAIND:-pchaind}" || { err "init failed"; exit 1; }
    ok "Node initialized"
  else
    step "Configuration exists, skipping init"
  fi

  next_phase "Starting and Syncing Node"
  step "Starting Push Chain validator node"
  "$MANAGER_BIN" start --home "$HOME_DIR" --bin "${PCHAIND:-pchaind}" || { err "start failed"; exit 1; }
  ok "Validator node started successfully"

  step "Waiting for state sync"
  # Stream compact sync until fully synced (monitor prints snapshot/block progress)
  set +e
  "$MANAGER_BIN" sync --compact --window 30 --rpc "http://127.0.0.1:26657" --remote "https://$GENESIS_DOMAIN:443" --skip-final-message
  SYNC_RC=$?
  set -e
  if [[ $SYNC_RC -ne 0 ]]; then warn "Sync monitoring ended with code $SYNC_RC"; fi

  echo  # Ensure newline after sync progress
  # Wait for peer connections to establish
  sleep 5
  if [[ $SYNC_RC -eq 0 ]]; then
    echo -e "${GREEN}✅ Sync complete! Node is fully synced.${NC}"
  fi
  "$MANAGER_BIN" status || true
  ok "Ready to become a validator"

  # Detect whether a controlling TTY is available for prompts/log view
  INTERACTIVE="no"
  if [[ -t 0 ]] && [[ -t 1 ]]; then
    INTERACTIVE="yes"
  elif [[ -e /dev/tty ]]; then
    INTERACTIVE="yes"
  fi

  REGISTRATION_STATUS="skipped"

  ALREADY_VALIDATOR="no"
  if node_is_validator; then
    ALREADY_VALIDATOR="yes"
    REGISTRATION_STATUS="already"
  fi

  if [[ "$ALREADY_VALIDATOR" == "no" ]]; then
    echo
    echo "Next steps:"
    echo "1. Get test tokens from: https://faucet.push.org"
    echo "2. Register as validator: push-validator-manager register-validator"
    echo
  else
    echo
    ok "Validator already registered"
    echo
    echo "  Check your validator:"
    echo "     push-validator-manager validators"
    echo
    echo "  Monitor node status:"
    echo "     push-validator-manager status"
    echo
  fi

  # Guard registration prompt in non-interactive mode
  if [[ "$ALREADY_VALIDATOR" == "yes" ]]; then
    RESP="N"
  else
    if [[ "$INTERACTIVE" == "yes" ]]; then
      if [[ -e /dev/tty ]]; then
        read -r -p "Register as a validator now? (y/N) " RESP < /dev/tty 2> /dev/tty || true
      else
        read -r -p "Register as a validator now? (y/N) " RESP || true
      fi
    else
      RESP="N"
    fi
  fi
  case "${RESP:-}" in
    [Yy])
      echo
      echo "Push Validator Manager - Registration"
      echo "══════════════════════════════════════"
      # Run registration flow directly (CLI handles prompts and status checks)
      if "$MANAGER_BIN" register-validator; then
        REGISTRATION_STATUS="success"
      else
        REGISTRATION_STATUS="failed"
      fi
      ;;
    *)
      # Ensure clean separation before summary
      echo
      REGISTRATION_STATUS="skipped"
      ;;
  esac
fi

# Shared post-install summary & follow-up
echo

# Allow environment variable override
ACTION="${PVM_POST_INSTALL_ACTION:-logs}"  # Default to logs

# Calculate total time for summary
INSTALL_END_TIME=$(date +%s)
TOTAL_TIME=$((INSTALL_END_TIME - ${INSTALL_START_TIME:-$INSTALL_END_TIME}))

# Enhanced final summary
MANAGER_VER=$("$MANAGER_BIN" version 2>/dev/null | awk '{print $2}' || echo "unknown")
PCHAIND_PATH="${PCHAIND:-pchaind}"
# Extract pchaind version if binary exists
if command -v "$PCHAIND_PATH" >/dev/null 2>&1; then
  CHAIN_VER=$("$PCHAIND_PATH" version 2>/dev/null | head -1 || echo "")
  if [[ -n "$CHAIN_VER" ]]; then
    PCHAIND_VER="$PCHAIND_PATH ($CHAIN_VER)"
  else
    PCHAIND_VER="$PCHAIND_PATH"
  fi
else
  PCHAIND_VER="$PCHAIND_PATH"
fi
RPC_URL="http://127.0.0.1:26657"

# Try to get Network and Moniker from status
TO_CMD=$(timeout_cmd)
if [[ -n "$TO_CMD" ]]; then
  STATUS_JSON=$($TO_CMD 5 "$MANAGER_BIN" status --output json 2>/dev/null || echo "{}")
else
  STATUS_JSON=$("$MANAGER_BIN" status --output json 2>/dev/null || echo "{}")
fi
if command -v jq >/dev/null 2>&1; then
  NETWORK=$(echo "$STATUS_JSON" | jq -r '.network // .node.network // empty' 2>/dev/null)
  MONIKER=$(echo "$STATUS_JSON" | jq -r '.moniker // .node.moniker // empty' 2>/dev/null)
else
  NETWORK=$(echo "$STATUS_JSON" | grep -o '"network"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"network"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
  MONIKER=$(echo "$STATUS_JSON" | grep -o '"moniker"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"moniker"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
fi

# Print summary (will be visible before TUI or for non-logs actions)
echo ""
echo "═══════════════════════════════════════════════════════════════"
echo "  Installation Complete (${TOTAL_TIME}s)"
echo "═══════════════════════════════════════════════════════════════"
echo "  Manager:       $MANAGER_BIN ($MANAGER_VER)"
echo "  Chain binary:  $PCHAIND_VER"
echo "  Home:          $HOME_DIR"
echo "  RPC:           $RPC_URL"
if [[ -n "$NETWORK" ]]; then
  echo "  Network:       $NETWORK"
fi
if [[ -n "$MONIKER" ]]; then
  echo "  Moniker:       $MONIKER"
fi
echo "───────────────────────────────────────────────────────────────"
echo "  Usage:        push-validator-manager status | logs | stop | restart"
echo "                push-validator-manager register-validator"
echo "                push-validator-manager --help (for more commands)"
echo "═══════════════════════════════════════════════════════════════"

if [[ "$INTERACTIVE" == "yes" ]] && [[ -z "$ACTION" || "$ACTION" == "logs" ]]; then
  # Interactive mode: directly show logs
  echo
  case "$REGISTRATION_STATUS" in
    success)
      ok "Validator registration completed"
      ;;
    failed)
      warn "Validator registration encountered issues; showing logs for troubleshooting"
      ;;
    already)
      ok "Validator already registered"
      ;;
  esac
  echo "Starting log viewer..."
  echo
  "$MANAGER_BIN" logs || true

  # After logs exit, show status
  echo
  if node_running; then
    ok "Node is still running"
  else
    warn "Node is not running"
    echo "Start it with: push-validator-manager start"
  fi
  print_useful_cmds
else
  # Non-interactive mode or custom action
  case "${ACTION}" in
    logs)
      echo
      echo "Viewing logs (non-interactive mode)..."
      echo
      "$MANAGER_BIN" logs || true
      ;;
    stop)
      echo
      echo "Stopping node..."
      "$MANAGER_BIN" stop || true
      ok "Node stopped"
      echo
      echo "Start it again with: push-validator-manager start"
      echo
      ;;
    keep|"")
      # Default: keep running
      echo
      if node_running; then
        ok "Node is running in background"
      else
        warn "Node is not running"
        echo "Start it with: push-validator-manager start"
      fi
      print_useful_cmds
      ;;
    *)
      warn "Invalid PVM_POST_INSTALL_ACTION='$ACTION', defaulting to keep running"
      if node_running; then
        ok "Node is running in background"
      else
        warn "Node is not running"
      fi
      print_useful_cmds
      ;;
  esac
fi
