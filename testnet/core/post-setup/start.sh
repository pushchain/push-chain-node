#!/bin/bash
set -e

# Resolve base directory: $HOME/app/
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

 # Load environment variables from .env if it exists
if [ -f "$APP_DIR/.env" ]; then
  export $(grep -v '^#' "$APP_DIR/.env" | xargs)
  echo "✅ Loaded environment variables from .env"
fi

BINARY="$APP_DIR/binary/pchaind"
NODE_HOME="$APP_DIR/.pchain"
LOG_DIR="$APP_DIR/logs"
LOG_FILE="$LOG_DIR/pchaind.log"

# Optional: make these configurable with defaults
CHAIN_ID="push_42101-1"
DENOM="upc"
RPC="26657"

mkdir -p "$LOG_DIR"

if [ ! -f "$BINARY" ]; then
  echo "❌ Binary not found at: $BINARY"
  exit 1
fi

echo "🚀 Starting node from: $NODE_HOME"
"$BINARY" start --pruning=nothing  --minimum-gas-prices=1000000000$DENOM --rpc.laddr="tcp://0.0.0.0:$RPC" --json-rpc.api=eth,txpool,personal,net,debug,web3 --chain-id="$CHAIN_ID" --home="$NODE_HOME" > "$LOG_FILE" 2>&1 &
echo "✅ Node started. Logging to: $LOG_FILE"
