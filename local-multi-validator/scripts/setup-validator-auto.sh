#!/bin/bash
set -eu
shopt -s expand_aliases

# ---------------------------
# === CONFIGURATION ===
# ---------------------------

CHAIN_ID=${CHAIN_ID:-"localchain_9000-1"}
MONIKER=${MONIKER:-"validator"}
VALIDATOR_ID=${VALIDATOR_ID:-"2"}
KEYRING="test"
KEYALGO="eth_secp256k1"
DENOM="upc"

# Paths
BINARY="/usr/bin/pchaind"
HOME_DIR="/root/.pchain"

# Ports (from environment)
RPC_PORT=${RPC_PORT:-26657}
REST_PORT=${REST_PORT:-1317}
GRPC_PORT=${GRPC_PORT:-9090}
GRPC_WEB_PORT=$((GRPC_PORT + 1))
P2P_PORT=${P2P_PORT:-26656}
PROFF_PORT=$((RPC_PORT + 3))
GENESIS_PEER=${GENESIS_PEER:-"core-validator-1:26656"}

# Validator stake amount
VALIDATOR_STAKE="100000000000000000000000"  # 100k * 10^18

# ---------------------------
# === LOAD PRE-GENERATED ACCOUNTS ===
# ---------------------------

TMP_DIR="/tmp/push-accounts"
GENESIS_ACCOUNTS_FILE="$TMP_DIR/genesis_accounts.json"
VALIDATORS_FILE="$TMP_DIR/validators.json"

# Check if account files exist
if [ ! -f "$GENESIS_ACCOUNTS_FILE" ] || [ ! -f "$VALIDATORS_FILE" ]; then
  echo "❌ Account files not found. Please run generate-accounts.sh first:"
  echo "   /opt/scripts/generate-accounts.sh"
  exit 1
fi

echo "📋 Loading pre-generated accounts for validator $VALIDATOR_ID..."

# Load validator mnemonic for this specific validator
VALIDATOR_MNEMONIC=$(jq -r ".[] | select(.id == $VALIDATOR_ID) | .mnemonic" "$VALIDATORS_FILE")
VALIDATOR_KEY="validator-$VALIDATOR_ID"

if [ "$VALIDATOR_MNEMONIC" = "null" ] || [ -z "$VALIDATOR_MNEMONIC" ]; then
  echo "❌ No validator found with ID $VALIDATOR_ID"
  exit 1
fi

echo "🔑 Using validator: $VALIDATOR_KEY"

# Genesis funding account (will fund this validator) - use validator-specific genesis account
FUNDING_INDEX=$((VALIDATOR_ID - 1))  # Convert to 0-based index
FUNDING_MNEMONIC=$(jq -r ".[$FUNDING_INDEX].mnemonic" "$GENESIS_ACCOUNTS_FILE")
FUNDING_KEY="genesis-acc-$VALIDATOR_ID"
FUNDING_AMOUNT="200000000000000000000000"  # 200k * 10^18 (enough for staking + fees)

# ---------------------------
# === WAIT FOR GENESIS VALIDATOR ===
# ---------------------------

echo "⏳ Waiting for genesis validator to be ready..."
GENESIS_RPC="http://core-validator-1:26657"

# Wait for genesis validator to be accessible
max_attempts=60
attempt=0
while [ $attempt -lt $max_attempts ]; do
  if curl -s "$GENESIS_RPC/status" > /dev/null 2>&1; then
    echo "✅ Genesis validator is ready!"
    break
  fi
  echo "Waiting for genesis validator... (attempt $((attempt + 1))/$max_attempts)"
  sleep 5
  attempt=$((attempt + 1))
done

if [ $attempt -eq $max_attempts ]; then
  echo "❌ Genesis validator not ready after ${max_attempts} attempts"
  exit 1
fi

# ---------------------------
# === INITIALIZATION ===
# ---------------------------

echo "🚨 Starting validator $VALIDATOR_ID setup..."
echo "Chain ID: $CHAIN_ID"
echo "Moniker: $MONIKER"

# Clean start
rm -rf "$HOME_DIR"/* "$HOME_DIR"/.[!.]* "$HOME_DIR"/..?* 2>/dev/null || true

echo "🧱 Initializing chain..."
$BINARY init "$MONIKER" --chain-id "$CHAIN_ID" --default-denom "$DENOM" --home "$HOME_DIR"

# ---------------------------
# === FETCH GENESIS ===
# ---------------------------

echo "🌍 Fetching genesis.json from genesis validator..."
curl -s "$GENESIS_RPC/genesis" | jq -r '.result.genesis' > "$HOME_DIR/config/genesis.json"

echo "🔍 Getting genesis validator node ID..."
GENESIS_NODE_ID=$(curl -s "$GENESIS_RPC/status" | jq -r '.result.node_info.id')
PERSISTENT_PEER="$GENESIS_NODE_ID@$GENESIS_PEER"

echo "🔗 Setting persistent peer: $PERSISTENT_PEER"
sed -i -e "s/^persistent_peers *=.*/persistent_peers = \"$PERSISTENT_PEER\"/" "$HOME_DIR/config/config.toml"

# ---------------------------
# === CREATE VALIDATOR KEY ===
# ---------------------------

echo "🔐 Creating validator key with pre-defined mnemonic..."
echo "$VALIDATOR_MNEMONIC" | $BINARY keys add "$VALIDATOR_KEY" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOME_DIR" --recover

VALIDATOR_ADDR=$($BINARY keys show "$VALIDATOR_KEY" -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
echo "Validator address: $VALIDATOR_ADDR"

# ---------------------------
# === FUND VALIDATOR ===
# ---------------------------

echo "💰 Setting up funding account..."
echo "$FUNDING_MNEMONIC" | $BINARY keys add "$FUNDING_KEY" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOME_DIR" --recover

echo "💸 Funding validator from genesis account..."
$BINARY tx bank send "$FUNDING_KEY" "$VALIDATOR_ADDR" "${FUNDING_AMOUNT}${DENOM}" \
  --chain-id "$CHAIN_ID" \
  --keyring-backend "$KEYRING" \
  --home "$HOME_DIR" \
  --node="$GENESIS_RPC" \
  --gas=auto \
  --gas-adjustment=1.3 \
  --gas-prices="1000000000${DENOM}" \
  --yes

echo "⏳ Waiting for funding transaction to be processed..."
sleep 10

# Verify funding
BALANCE=$($BINARY query bank balances "$VALIDATOR_ADDR" --chain-id "$CHAIN_ID" --home "$HOME_DIR" --node="$GENESIS_RPC" --output json | jq -r ".balances[0].amount // \"0\"")
echo "💰 Validator balance: $BALANCE $DENOM"

if [ "$BALANCE" -lt "$VALIDATOR_STAKE" ]; then
  echo "❌ Insufficient balance for staking. Required: $VALIDATOR_STAKE, Available: $BALANCE"
  exit 1
fi

echo "✅ Validator funded successfully!"

# ---------------------------
# === CONFIG PATCHING ===
# ---------------------------

echo "⚙️ Configuring network settings..."

# RPC configuration
sed -i -e "s/laddr = \"tcp:\/\/127.0.0.1:26657\"/laddr = \"tcp:\/\/0.0.0.0:${RPC_PORT}\"/g" $HOME_DIR/config/config.toml
sed -i -e 's/cors_allowed_origins = \[\]/cors_allowed_origins = \["\*"\]/g' $HOME_DIR/config/config.toml

# REST configuration
sed -i -e "s/address = \"tcp:\/\/localhost:1317\"/address = \"tcp:\/\/0.0.0.0:${REST_PORT}\"/g" $HOME_DIR/config/app.toml
sed -i -e 's/enable = false/enable = true/g' $HOME_DIR/config/app.toml
sed -i -e 's/enabled-unsafe-cors = false/enabled-unsafe-cors = true/g' $HOME_DIR/config/app.toml

# P2P configuration
sed -i -e "s/laddr = \"tcp:\/\/0.0.0.0:26656\"/laddr = \"tcp:\/\/0.0.0.0:${P2P_PORT}\"/g" $HOME_DIR/config/config.toml

# GRPC configuration
sed -i -e "s/address = \"localhost:9090\"/address = \"0.0.0.0:${GRPC_PORT}\"/g" $HOME_DIR/config/app.toml
sed -i -e "s/address = \"localhost:9091\"/address = \"0.0.0.0:${GRPC_WEB_PORT}\"/g" $HOME_DIR/config/app.toml

# Profiling
sed -i -e "s/pprof_laddr = \"localhost:6060\"/pprof_laddr = \"localhost:${PROFF_PORT}\"/g" $HOME_DIR/config/config.toml

# Block time
sed -i -e 's/timeout_commit = "5s"/timeout_commit = "1s"/g' $HOME_DIR/config/config.toml

# ---------------------------
# === START NODE AND AUTO-PROMOTE ===
# ---------------------------

echo "🚀 Starting validator node..."

# Start node in background
$BINARY start \
  --home "$HOME_DIR" \
  --pruning=nothing \
  --minimum-gas-prices="1000000000${DENOM}" \
  --rpc.laddr="tcp://0.0.0.0:${RPC_PORT}" \
  --json-rpc.api=eth,txpool,personal,net,debug,web3 \
  --chain-id="$CHAIN_ID" &

NODE_PID=$!

# Wait for node to sync
echo "⏳ Waiting for node to sync..."
max_sync_attempts=120
sync_attempt=0

while [ $sync_attempt -lt $max_sync_attempts ]; do
  if curl -s "http://localhost:${RPC_PORT}/status" > /dev/null 2>&1; then
    CATCHING_UP=$(curl -s "http://localhost:${RPC_PORT}/status" | jq -r '.result.sync_info.catching_up')
    if [ "$CATCHING_UP" = "false" ]; then
      echo "✅ Node is synced!"
      break
    fi
  fi
  echo "Syncing... (attempt $((sync_attempt + 1))/$max_sync_attempts)"
  sleep 10
  sync_attempt=$((sync_attempt + 1))
done

if [ $sync_attempt -eq $max_sync_attempts ]; then
  echo "❌ Node failed to sync after ${max_sync_attempts} attempts"
  kill $NODE_PID
  exit 1
fi

# ---------------------------
# === AUTO-PROMOTE TO VALIDATOR ===
# ---------------------------

echo "🎖️ Auto-promoting to validator..."

# Get tendermint pubkey
PUBKEY_JSON=$($BINARY tendermint show-validator --home "$HOME_DIR")

# Create validator JSON
VALIDATOR_JSON="/tmp/validator.json"
cat > "$VALIDATOR_JSON" <<EOF
{
  "pubkey": $PUBKEY_JSON,
  "amount": "${VALIDATOR_STAKE}${DENOM}",
  "moniker": "$MONIKER",
  "identity": "",
  "website": "",
  "security": "",
  "details": "Push Chain Auto Validator $VALIDATOR_ID",
  "commission-rate": "0.10",
  "commission-max-rate": "0.20",
  "commission-max-change-rate": "0.01",
  "min-self-delegation": "1"
}
EOF

# Submit create-validator transaction
echo "🚀 Submitting create-validator transaction..."
$BINARY tx staking create-validator "$VALIDATOR_JSON" \
  --from "$VALIDATOR_KEY" \
  --chain-id "$CHAIN_ID" \
  --keyring-backend "$KEYRING" \
  --home "$HOME_DIR" \
  --node="$GENESIS_RPC" \
  --gas=auto \
  --gas-adjustment=1.3 \
  --gas-prices="1000000000${DENOM}" \
  --yes

# Clean up
rm -f "$VALIDATOR_JSON"

echo "✅ Validator $VALIDATOR_ID promoted successfully!"
echo "🔄 Node will continue running as validator..."

# Wait for the background process
wait $NODE_PID