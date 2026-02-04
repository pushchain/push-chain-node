#!/bin/bash
set -eu

# Load environment
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$SCRIPT_DIR/env.sh"

# Validator specific config
VALIDATOR_ID=${VALIDATOR_ID:-2}
HOME_DIR="$DATA_DIR/validator$VALIDATOR_ID/.pchain"
MONIKER="validator-$VALIDATOR_ID"
LOG_FILE="$DATA_DIR/validator$VALIDATOR_ID/validator.log"

# Ports for this validator
case "$VALIDATOR_ID" in
    2) RPC_PORT=26658; REST_PORT=1318; GRPC_PORT=9093; P2P_PORT=26666; EVM_PORT=8547; EVM_WS_PORT=8548 ;;
    3) RPC_PORT=26659; REST_PORT=1319; GRPC_PORT=9095; P2P_PORT=26676; EVM_PORT=8549; EVM_WS_PORT=8550 ;;
    4) RPC_PORT=26660; REST_PORT=1320; GRPC_PORT=9097; P2P_PORT=26686; EVM_PORT=8551; EVM_WS_PORT=8552 ;;
    *) echo "Invalid VALIDATOR_ID: $VALIDATOR_ID"; exit 1 ;;
esac

# Genesis RPC (use 127.0.0.1 to avoid IPv6 issues on macOS)
GENESIS_RPC="http://127.0.0.1:26657"
GENESIS_P2P_PORT=26656

echo "🚨 Starting validator $VALIDATOR_ID setup..."
echo "Chain ID: $CHAIN_ID"
echo "Home: $HOME_DIR"

# Wait for genesis validator
echo "⏳ Waiting for genesis validator..."
max_attempts=60
attempt=0
while [ $attempt -lt $max_attempts ]; do
    if curl -s "$GENESIS_RPC/status" > /dev/null 2>&1; then
        echo "✅ Genesis validator is ready!"
        break
    fi
    sleep 2
    attempt=$((attempt + 1))
done

if [ $attempt -eq $max_attempts ]; then
    echo "❌ Genesis validator not ready"
    exit 1
fi

# Clean and initialize
rm -rf "$HOME_DIR"
mkdir -p "$HOME_DIR"
mkdir -p "$(dirname "$LOG_FILE")"

"$PCHAIND_BIN" init "$MONIKER" --chain-id "$CHAIN_ID" --default-denom "$DENOM" --home "$HOME_DIR"

# Copy genesis from accounts dir
echo "🌍 Copying genesis..."
cp "$ACCOUNTS_DIR/genesis.json" "$HOME_DIR/config/genesis.json"

# Import validator key
echo "🔐 Importing validator key..."
VALIDATORS_FILE="$ACCOUNTS_DIR/validators.json"
mnemonic=$(jq -r ".[$((VALIDATOR_ID-1))].mnemonic" "$VALIDATORS_FILE")
echo "$mnemonic" | "$PCHAIND_BIN" keys add "validator-$VALIDATOR_ID" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOME_DIR" --recover 2>/dev/null || true

VALIDATOR_ADDR=$("$PCHAIND_BIN" keys show "validator-$VALIDATOR_ID" -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
echo "Validator address: $VALIDATOR_ADDR"

# Get genesis node ID and set persistent peers
GENESIS_NODE_ID=$(curl -s "$GENESIS_RPC/status" | jq -r '.result.node_info.id')
PERSISTENT_PEER="$GENESIS_NODE_ID@127.0.0.1:$GENESIS_P2P_PORT"
echo "🔗 Setting persistent peer: $PERSISTENT_PEER"
sed -i.bak "s/^persistent_peers *=.*/persistent_peers = \"$PERSISTENT_PEER\"/" "$HOME_DIR/config/config.toml"

# Config patches
echo "⚙️ Configuring network..."
# Allow multiple connections from same IP (needed for local multi-validator setup)
sed -i.bak 's/allow_duplicate_ip = false/allow_duplicate_ip = true/g' "$HOME_DIR/config/config.toml"
sed -i.bak "s/laddr = \"tcp:\/\/127.0.0.1:26657\"/laddr = \"tcp:\/\/0.0.0.0:${RPC_PORT}\"/g" "$HOME_DIR/config/config.toml"
sed -i.bak 's/cors_allowed_origins = \[\]/cors_allowed_origins = \["\*"\]/g' "$HOME_DIR/config/config.toml"
sed -i.bak "s/address = \"tcp:\/\/localhost:1317\"/address = \"tcp:\/\/0.0.0.0:${REST_PORT}\"/g" "$HOME_DIR/config/app.toml"
sed -i.bak 's/enable = false/enable = true/g' "$HOME_DIR/config/app.toml"
sed -i.bak "s/address = \"localhost:9090\"/address = \"0.0.0.0:${GRPC_PORT}\"/g" "$HOME_DIR/config/app.toml"
sed -i.bak "s/laddr = \"tcp:\/\/0.0.0.0:26656\"/laddr = \"tcp:\/\/0.0.0.0:${P2P_PORT}\"/g" "$HOME_DIR/config/config.toml"
sed -i.bak 's/timeout_commit = "5s"/timeout_commit = "1s"/g' "$HOME_DIR/config/config.toml"

# Start node
echo "🚀 Starting validator $VALIDATOR_ID..."
"$PCHAIND_BIN" start \
    --home "$HOME_DIR" \
    --pruning=nothing \
    --minimum-gas-prices="1000000000${DENOM}" \
    --rpc.laddr="tcp://0.0.0.0:${RPC_PORT}" \
    --json-rpc.address="0.0.0.0:${EVM_PORT}" \
    --json-rpc.ws-address="0.0.0.0:${EVM_WS_PORT}" \
    --json-rpc.api=eth,txpool,personal,net,debug,web3 \
    --chain-id="$CHAIN_ID" &

NODE_PID=$!

# Wait for sync
echo "⏳ Waiting for sync..."
max_sync=120
sync_attempt=0
while [ $sync_attempt -lt $max_sync ]; do
    if curl -s "http://127.0.0.1:${RPC_PORT}/status" > /dev/null 2>&1; then
        catching_up=$(curl -s "http://127.0.0.1:${RPC_PORT}/status" | jq -r '.result.sync_info.catching_up')
        if [ "$catching_up" = "false" ]; then
            echo "✅ Node synced!"
            break
        fi
    fi
    sleep 5
    sync_attempt=$((sync_attempt + 1))
done

# Create validator if not already bonded
VALOPER_ADDR=$("$PCHAIND_BIN" keys show "validator-$VALIDATOR_ID" --bech val -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
VALIDATOR_STATUS=$("$PCHAIND_BIN" query staking validator "$VALOPER_ADDR" --node="$GENESIS_RPC" --output json 2>/dev/null | jq -r '.status' || echo "NOT_FOUND")

if [ "$VALIDATOR_STATUS" != "BOND_STATUS_BONDED" ]; then
    echo "📝 Creating validator..."
    
    PUBKEY=$("$PCHAIND_BIN" tendermint show-validator --home "$HOME_DIR")
    VALIDATOR_JSON="$HOME_DIR/validator.json"
    cat > "$VALIDATOR_JSON" <<EOF
{
  "pubkey": $PUBKEY,
  "amount": "${VALIDATOR_STAKE}${DENOM}",
  "moniker": "validator-$VALIDATOR_ID",
  "identity": "",
  "website": "",
  "security": "",
  "details": "Validator $VALIDATOR_ID",
  "commission-rate": "0.10",
  "commission-max-rate": "0.20",
  "commission-max-change-rate": "0.01",
  "min-self-delegation": "1"
}
EOF

    sleep $((VALIDATOR_ID * 2))  # Stagger
    
    "$PCHAIND_BIN" tx staking create-validator "$VALIDATOR_JSON" \
        --from="validator-$VALIDATOR_ID" \
        --chain-id="$CHAIN_ID" \
        --keyring-backend="$KEYRING" \
        --home="$HOME_DIR" \
        --node="$GENESIS_RPC" \
        --gas=auto \
        --gas-adjustment=1.5 \
        --gas-prices="1000000000${DENOM}" \
        --yes || echo "Create validator may have failed"
    
    echo "✅ Validator $VALIDATOR_ID created"
fi

wait $NODE_PID
