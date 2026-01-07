#!/bin/bash
set -e

###############################################
# Push Chain Node Start Script (Cosmovisor)
#
# Starts the node using Cosmovisor for
# automatic binary upgrade management.
#
# Prerequisites:
# - Cosmovisor installed: go install cosmossdk.io/tools/cosmovisor/cmd/cosmovisor@latest
# - Binary at: $APP_DIR/binary/pchaind
###############################################

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Load environment variables from .env if it exists
if [ -f "$APP_DIR/.env" ]; then
  export $(grep -v '^#' "$APP_DIR/.env" | xargs)
  echo "✅ Loaded environment variables from .env"
fi

# Paths
BINARY="$APP_DIR/binary/pchaind"
NODE_HOME="$APP_DIR/.pchain"
LOG_DIR="$APP_DIR/logs"
LOG_FILE="$LOG_DIR/pchaind.log"

# Cosmovisor environment variables
export DAEMON_NAME=pchaind
export DAEMON_HOME="$NODE_HOME"
export DAEMON_ALLOW_DOWNLOAD_BINARIES=true
export DAEMON_RESTART_AFTER_UPGRADE=true
export UNSAFE_SKIP_BACKUP=false

# Chain config (can be overridden by .env)
CHAIN_ID="${CHAIN_ID:-push_42101-1}"
DENOM="${DENOM:-upc}"
RPC="${RPC:-26657}"

mkdir -p "$LOG_DIR"

# Check binary exists
if [ ! -f "$BINARY" ]; then
  echo "❌ Binary not found at: $BINARY"
  exit 1
fi

# Setup Cosmovisor directory structure
COSMOVISOR_DIR="$NODE_HOME/cosmovisor"
GENESIS_BIN_DIR="$COSMOVISOR_DIR/genesis/bin"

mkdir -p "$GENESIS_BIN_DIR"
mkdir -p "$COSMOVISOR_DIR/upgrades"

# Copy genesis binary if not present or if source is newer
if [ ! -f "$GENESIS_BIN_DIR/pchaind" ] || [ "$BINARY" -nt "$GENESIS_BIN_DIR/pchaind" ]; then
  echo "📦 Copying binary to Cosmovisor genesis directory..."
  cp "$BINARY" "$GENESIS_BIN_DIR/pchaind"
  chmod +x "$GENESIS_BIN_DIR/pchaind"
  echo "✅ Binary copied to: $GENESIS_BIN_DIR/pchaind"
fi

# Check cosmovisor is installed
if ! command -v cosmovisor &> /dev/null; then
  echo "❌ Cosmovisor not found. Install with:"
  echo "   go install cosmossdk.io/tools/cosmovisor/cmd/cosmovisor@latest"
  exit 1
fi

echo "🚀 Starting node with Cosmovisor from: $NODE_HOME"
cosmovisor run start \
  --pruning=nothing \
  --minimum-gas-prices=1000000000$DENOM \
  --rpc.laddr="tcp://0.0.0.0:$RPC" \
  --json-rpc.address="0.0.0.0:8545" \
  --json-rpc.ws-address="0.0.0.0:8546" \
  --json-rpc.api=eth,txpool,personal,net,debug,web3 \
  --chain-id="$CHAIN_ID" \
  --home="$NODE_HOME" > "$LOG_FILE" 2>&1 &

echo "✅ Node started with Cosmovisor. Logging to: $LOG_FILE"
