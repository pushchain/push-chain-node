#!/usr/bin/env bash
# Push Validator Manager (Go) — Installer with local/clone build + guided start
# Examples:
#   bash install.sh                            # default: reset data, build if needed, init+start, wait for sync
#   bash install.sh --no-reset --no-start      # install only
#   bash install.sh --use-local                # use current repo checkout to build
#   PNM_REF=feature/pnm bash install.sh         # clone specific ref (branch/tag)

set -euo pipefail
IFS=$'\n\t'

# Styling
CYAN='\033[0;36m'; GREEN='\033[0;32m'; YELLOW='\033[0;33m'; RED='\033[0;31m'; BOLD='\033[1m'; NC='\033[0m'
status() { echo -e "${CYAN}$*${NC}"; }
ok()     { echo -e "${GREEN}$*${NC}"; }
warn()   { echo -e "${YELLOW}$*${NC}"; }
err()    { echo -e "${RED}$*${NC}"; }

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

# Flags
USE_LOCAL="no"
LOCAL_REPO=""
PCHAIND_REF="${PCHAIND_REF:-}"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-start) AUTO_START="no"; shift ;;
    --start) AUTO_START="yes"; shift ;;
    --no-reset) RESET_DATA="no"; shift ;;
    --reset) RESET_DATA="yes"; shift ;;
    --moniker) MONIKER="$2"; shift 2 ;;
    --genesis) GENESIS_DOMAIN="$2"; shift 2 ;;
    --keyring) KEYRING_BACKEND="$2"; shift 2 ;;
    --chain-id) CHAIN_ID="$2"; shift 2 ;;
    --snapshot-rpc) SNAPSHOT_RPC="$2"; shift 2 ;;
    --pchaind-ref) PCHAIND_REF="$2"; shift 2 ;;
    --use-local) USE_LOCAL="yes"; shift ;;
    --local-repo) LOCAL_REPO="$2"; shift 2 ;;
    *) err "Unknown flag: $1"; exit 1 ;;
  esac
done

# Paths
if [[ -n "${XDG_DATA_HOME:-}" ]]; then ROOT_DIR="$XDG_DATA_HOME/push-validator-manager"; else ROOT_DIR="$HOME/.local/share/push-validator-manager"; fi
REPO_DIR="$ROOT_DIR/repo"
INSTALL_BIN_DIR="$HOME/.local/bin"
MANAGER_BIN="$INSTALL_BIN_DIR/push-validator-manager"

mkdir -p "$ROOT_DIR" "$INSTALL_BIN_DIR"

need() { command -v "$1" >/dev/null 2>&1 || { err "Missing dependency: $1"; exit 1; }; }
need curl; need git; need go

# Optionally reset existing chain data (wallets preserved)
if [[ "$RESET_DATA" = "yes" && -d "$HOME/.pchain" ]]; then
  status "🧹 Resetting blockchain data (wallets preserved)..."
  # If manager exists, stop via CLI; otherwise best-effort kill
  if [[ -x "$MANAGER_BIN" ]]; then "$MANAGER_BIN" stop >/dev/null 2>&1 || true; else pkill -f "pchaind.*start.*--home.*$HOME/.pchain" 2>/dev/null || true; fi
  rm -rf "$HOME/.pchain/data" "$HOME/.pchain/config" "$HOME/.pchain/wasm" "$HOME/.pchain/logs" 2>/dev/null || true
  rm -f "$HOME/.pchain/pchaind.pid" 2>/dev/null || true
  ok "✅ Blockchain data reset (wallets preserved)"
fi

status "📦 Installing Push Validator Manager into $ROOT_DIR"

# Determine repo source
if [[ "$USE_LOCAL" = "yes" || -n "$LOCAL_REPO" ]]; then
  if [[ -n "$LOCAL_REPO" ]]; then REPO_DIR="$(cd "$LOCAL_REPO" && pwd -P)"; else REPO_DIR="$(cd "$SELF_DIR/.." && pwd -P)"; fi
  status "🧪 Using local repository: $REPO_DIR"
  if [[ ! -f "$REPO_DIR/push-validator-manager-go/go.mod" ]]; then
    err "Expected Go module not found at: $REPO_DIR/push-validator-manager-go"; exit 1
  fi
else
  rm -rf "$REPO_DIR"
  status "📥 Cloning push-chain-node (ref: $PNM_REF)..."
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

status "🔨 Building Push Validator Manager binary..."
pushd "$REPO_DIR/push-validator-manager-go" >/dev/null
GOFLAGS="-trimpath" CGO_ENABLED=0 go build -mod=mod -o "$MANAGER_BIN" ./cmd/push-validator-manager
popd >/dev/null
chmod +x "$MANAGER_BIN"
ok "✅ Manager installed: $MANAGER_BIN"

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

# Build or select pchaind (prefer locally built binary to match network upgrades)
BUILD_SCRIPT="$REPO_DIR/push-validator-manager-go/scripts/build-pchaind.sh"
if [[ "$USE_LOCAL" = "yes" && -f "$BUILD_SCRIPT" ]]; then
  status "🔨 Building Push Chain binary..."
  # For local use, build from the current repo
  BUILD_OUTPUT="$REPO_DIR/push-validator-manager-go/scripts/build"
  if bash "$BUILD_SCRIPT" "$REPO_DIR" "$BUILD_OUTPUT"; then
    if [[ -f "$BUILD_OUTPUT/pchaind" ]]; then
      mkdir -p "$INSTALL_BIN_DIR"
      ln -sf "$BUILD_OUTPUT/pchaind" "$INSTALL_BIN_DIR/pchaind"
      export PCHAIND="$INSTALL_BIN_DIR/pchaind"
      ok "✅ Built and installed pchaind: $PCHAIND"
    else
      warn "⚠️ Build completed but binary not found at expected location"
    fi
  else
    warn "⚠️ Build failed; trying fallback options"
  fi
fi

# Fallback to existing approaches if build didn't work
if [[ -z "$PCHAIND" || ! -f "$PCHAIND" ]]; then
  if [[ -f "$REPO_DIR/push-validator-manager/scripts/setup-dependencies.sh" ]]; then
    status "🔨 Trying bash installer's build script..."
    PCHAIND_REF="$PCHAIND_REF" bash "$REPO_DIR/push-validator-manager/scripts/setup-dependencies.sh" || warn "⚠️ Build helper failed"
    # Link built binary into PATH for consistent usage
    if [[ -f "$REPO_DIR/push-validator-manager/scripts/build/pchaind" ]]; then
      mkdir -p "$INSTALL_BIN_DIR"
      ln -sf "$REPO_DIR/push-validator-manager/scripts/build/pchaind" "$INSTALL_BIN_DIR/pchaind"
      export PCHAIND="$INSTALL_BIN_DIR/pchaind"
      ok "✅ Using built pchaind: $PCHAIND"
    fi
  fi
fi

# Final fallback to system pchaind
if [[ -z "$PCHAIND" || ! -f "$PCHAIND" ]]; then
  if command -v pchaind >/dev/null 2>&1; then
    export PCHAIND="$(command -v pchaind)"
    ok "✅ Found existing pchaind: $PCHAIND"
  else
    warn "⚠️ pchaind not found. Please install pchaind or ensure the build succeeds"
  fi
fi

# WebSocket client step (not needed with Go WS monitor)
status "🔌 Checking WebSocket client for real-time sync..."
ok "✅ Using built-in WebSocket monitor (no external dependency)"

if [[ "$AUTO_START" = "yes" ]]; then
  status "Starting Push Chain node..."
  # Initialize if config or genesis missing
  if [[ ! -f "$HOME/.pchain/config/config.toml" ]] || [[ ! -f "$HOME/.pchain/config/genesis.json" ]]; then
    status "🔧 Initializing node configuration..."
    "$MANAGER_BIN" init \
      --moniker "$MONIKER" \
      --home "$HOME/.pchain" \
      --chain-id "$CHAIN_ID" \
      --genesis-domain "$GENESIS_DOMAIN" \
      --snapshot-rpc "$SNAPSHOT_RPC" \
      --bin "${PCHAIND:-pchaind}" || { err "init failed"; exit 1; }
    ok "✅ Node initialized"
  fi
  "$MANAGER_BIN" start --home "$HOME/.pchain" --bin "${PCHAIND:-pchaind}" || { err "start failed"; exit 1; }
  ok "✅ Node started successfully"

  status "⏳ Waiting for state sync..."
  # Stream compact sync until fully synced (monitor prints snapshot/block progress)
  set +e
  "$MANAGER_BIN" sync --compact --window 30 --rpc "http://127.0.0.1:26657" --remote "https://$GENESIS_DOMAIN:443"
  SYNC_RC=$?
  set -e
  if [[ $SYNC_RC -ne 0 ]]; then warn "⚠️ Sync monitoring ended with code $SYNC_RC"; fi

  # Quick status sample
  status "📡 Checking block sync status..."
  "$MANAGER_BIN" status || true
  ok "✅ Ready to become a validator!"

  echo
  echo "Next steps:"
  echo "1. Get test tokens from: https://faucet.push.org"
  echo "2. Register as validator: push-validator-manager register-validator"
  echo

  read -r -p "Register as a validator now? (y/N) " RESP || true
  case "${RESP:-}" in
    [Yy])
      echo
      echo "Push Validator Manager - Registration"
      echo "══════════════════════════════════════"
      # Prompt for overrides
      read -r -p "Enter validator name (moniker) [${MONIKER}]: " MONIN || true
      MONIN=${MONIN:-$MONIKER}
      read -r -p "Enter key name for validator (default: validator-key): " KEYIN || true
      KEYIN=${KEYIN:-validator-key}
      export MONIKER="$MONIN" KEY_NAME="$KEYIN"
      # Run registration flow (handles balance checks itself)
      "$MANAGER_BIN" register-validator || true
      ;;
    *) : ;;
  esac
fi

ok "Done. Use 'push-validator-manager --help' for commands."
