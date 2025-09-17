#!/bin/bash
set -eu
shopt -s expand_aliases

# ---------------------------
# === USER MUST SPECIFY GENESIS NODE DOMAIN ===
# ---------------------------

if [ $# -lt 1 ]; then
  echo "❌ Usage: bash setup_fullnode.sh <genesis-node-domain>"
  echo "   Example: bash setup_fullnode.sh node1.push.org"
  exit 1
fi

GENESIS_DOMAIN=$1
GENESIS_RPC="https://$GENESIS_DOMAIN"

# ---------------------------
# === CHAIN SETTINGS ===
# ---------------------------

CHAIN_ID="push_42101-1" 
MONIKER="node"
DENOM="upc"

# Base path
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BINARY="$APP_DIR/binary/pchaind"
HOME_DIR="$APP_DIR/.pchain"
LOG_DIR="$APP_DIR/logs"

# Ports
RPC=${RPC:-26657}
REST=${REST:-1317}
GRPC=${GRPC:-9090}
GRPC_WEB=${GRPC_WEB:-9091}
PROFF=${PROFF:-6060}
P2P=${P2P:-26656}
ROSETTA=${ROSETTA:-8080}
BLOCK_TIME=${BLOCK_TIME:-"1s"}

# ---------------------------
# === CLEAN START ===
# ---------------------------

echo "🚨 Removing old node at $HOME_DIR"
rm -rf "$HOME_DIR"

echo "🚨 Removing old logs at $LOG_DIR"
rm -rf "$LOG_DIR"

echo "🧱 Initializing chain: $MONIKER ($CHAIN_ID)"
"$BINARY" init "$MONIKER" --chain-id "$CHAIN_ID" --default-denom "$DENOM" --home "$HOME_DIR"

# ---------------------------
# === SYNC GENESIS FROM TENDERMINT RPC
# ---------------------------

echo "🌍 Fetching genesis.json from $GENESIS_RPC/genesis"
curl -s "$GENESIS_RPC/genesis" | jq -r '.result.genesis' > "$HOME_DIR/config/genesis.json"

echo "🔍 Fetching validator node ID from /status"
VALIDATOR_NODE_ID=$(curl -s "$GENESIS_RPC/status" | jq -r '.result.node_info.id')

echo "🔍 Resolving $GENESIS_DOMAIN to IP"
VALIDATOR_IP=$(dig +short "$GENESIS_DOMAIN" | tail -n1)

PERSISTENT_PEER="$VALIDATOR_NODE_ID@$VALIDATOR_IP:$P2P"
echo "🔗 persistent_peers = $PERSISTENT_PEER"
sed -i -e "s/^persistent_peers *=.*/persistent_peers = \"$PERSISTENT_PEER\"/" "$HOME_DIR/config/config.toml"

# ---------------------------
# === CONFIG PATCHING ===
# ---------------------------

# RPC
sed -i -e 's/laddr = "tcp:\/\/127.0.0.1:26657"/laddr = "tcp:\/\/0.0.0.0:'$RPC'"/g' $HOME_DIR/config/config.toml
sed -i -e 's/cors_allowed_origins = \[\]/cors_allowed_origins = \["\*"\]/g' $HOME_DIR/config/config.toml

# REST
sed -i -e 's/address = "tcp:\/\/localhost:1317"/address = "tcp:\/\/0.0.0.0:'$REST'"/g' $HOME_DIR/config/app.toml
sed -i -e 's/enable = false/enable = true/g' $HOME_DIR/config/app.toml
sed -i -e 's/enabled-unsafe-cors = false/enabled-unsafe-cors = true/g' $HOME_DIR/config/app.toml

# P2P & profiling
sed -i -e 's/pprof_laddr = "localhost:6060"/pprof_laddr = "localhost:'$PROFF'"/g' $HOME_DIR/config/config.toml
sed -i -e 's/laddr = "tcp:\/\/0.0.0.0:26656"/laddr = "tcp:\/\/0.0.0.0:'$P2P'"/g' $HOME_DIR/config/config.toml

# gRPC
sed -i -e 's/address = "localhost:9090"/address = "0.0.0.0:'$GRPC'"/g' $HOME_DIR/config/app.toml
sed -i -e 's/address = "localhost:9091"/address = "0.0.0.0:'$GRPC_WEB'"/g' $HOME_DIR/config/app.toml

# Rosetta
sed -i -e 's/address = ":8080"/address = "0.0.0.0:'$ROSETTA'"/g' $HOME_DIR/config/app.toml

# Faster blocks
sed -i -e 's/timeout_commit = "5s"/timeout_commit = "'$BLOCK_TIME'"/g' $HOME_DIR/config/config.toml

# ---------------------------
# ✅ DONE
# ---------------------------

echo ""
echo "✅ Full node setup complete!"
echo "🌐 Connected to validator at: $GENESIS_DOMAIN"
echo "➡️ Start the node with:"
echo "   bash $APP_DIR/scripts/start.sh"
