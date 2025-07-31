#!/bin/bash

###############################################
# Push Chain Enhanced Sync Status Checker
#
# Shows:
# - Chain ID
# - Node moniker
# - Node ID
# - Latest block height & time
# - Catching up status
# - Peers connected
# - App version
#
# Requires:
# - jq
# - Local node running on port 26657
###############################################

RPC_URL="http://localhost:26657"
STATUS_URL="$RPC_URL/status"
NET_INFO_URL="$RPC_URL/net_info"
ABCI_INFO_URL="$RPC_URL/abci_info"

echo "🔍 Checking Push Chain node status..."

# Ensure jq is installed
if ! command -v jq &> /dev/null; then
  echo "📦 Installing jq..."
  sudo apt-get update && sudo apt-get install -y jq
fi

# Get status response
STATUS=$(curl -s "$STATUS_URL")
NET_INFO=$(curl -s "$NET_INFO_URL")
ABCI_INFO=$(curl -s "$ABCI_INFO_URL")

# Validate status
if [ -z "$STATUS" ]; then
  echo "❌ Unable to connect to local RPC at $RPC_URL"
  exit 1
fi

# Parse info
CHAIN_ID=$(echo "$STATUS" | jq -r '.result.node_info.network')
MONIKER=$(echo "$STATUS" | jq -r '.result.node_info.moniker')
NODE_ID=$(echo "$STATUS" | jq -r '.result.node_info.id')
HEIGHT=$(echo "$STATUS" | jq -r '.result.sync_info.latest_block_height')
CATCHING_UP=$(echo "$STATUS" | jq -r '.result.sync_info.catching_up')
PEERS=$(echo "$NET_INFO" | jq -r '.result.n_peers')
APP_VERSION=$(echo "$ABCI_INFO" | jq -r '.result.response.version')

# Output
echo ""
echo "🔗 Chain ID       : $CHAIN_ID"
echo "🧑 Node Moniker   : $MONIKER"
echo "🆔 Node ID        : $NODE_ID"
echo "📦 App Version    : $APP_VERSION"
echo "🔢 Block Height   : $HEIGHT"
echo "📡 Peers Connected: $PEERS"
echo "🧭 Catching Up    : $CATCHING_UP"

# Health summary
if [[ "$CATCHING_UP" == "false" ]]; then
  echo -e "\n✅ Node is fully synced and healthy."
else
  echo -e "\n⚠️  Node is still catching up. Monitor progress..."
fi
