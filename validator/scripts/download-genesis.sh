#!/bin/bash
# Download genesis directly from the network

set -e

# Network node
GENESIS_NODE="http://34.57.209.0:26657"

echo "🔍 Downloading genesis from network node..."

# Download genesis
curl -s "$GENESIS_NODE/genesis" | jq -r '.result.genesis' > /tmp/genesis-network.json

if [ ! -f "/tmp/genesis-network.json" ]; then
    echo "❌ Failed to download genesis"
    exit 1
fi

# Show app_hash
echo "📋 Network genesis info:"
echo -n "App Hash: "
jq -r '.app_hash' /tmp/genesis-network.json
echo -n "Chain ID: "
jq -r '.chain_id' /tmp/genesis-network.json
echo -n "Genesis Time: "
jq -r '.genesis_time' /tmp/genesis-network.json

# Compare with local
if [ -f "/configs/genesis-testnet.json" ]; then
    echo ""
    echo "📋 Local genesis info:"
    echo -n "App Hash: "
    jq -r '.app_hash' /configs/genesis-testnet.json
    echo -n "Chain ID: "
    jq -r '.chain_id' /configs/genesis-testnet.json
    
    # Check if they match
    NETWORK_HASH=$(jq -r '.app_hash' /tmp/genesis-network.json)
    LOCAL_HASH=$(jq -r '.app_hash' /configs/genesis-testnet.json)
    
    if [ "$NETWORK_HASH" = "$LOCAL_HASH" ]; then
        echo "✅ Genesis files match!"
    else
        echo "❌ Genesis files DO NOT match!"
        echo ""
        echo "Would you like to replace local genesis with network genesis?"
        read -p "Replace genesis? (yes/no): " -r
        if [[ $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
            cp /tmp/genesis-network.json /configs/genesis-testnet.json
            echo "✅ Genesis file updated!"
            echo ""
            echo "Now run: ./push-validator start --clean"
        fi
    fi
else
    echo "❌ Local genesis not found at /configs/genesis-testnet.json"
fi