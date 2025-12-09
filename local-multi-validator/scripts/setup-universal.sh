#!/bin/bash
set -eu

# ---------------------------
# === CONFIGURATION ===
# ---------------------------

UNIVERSAL_ID=${UNIVERSAL_ID:-"1"}
CORE_VALIDATOR_GRPC=${CORE_VALIDATOR_GRPC:-"core-validator-1:9090"}
QUERY_PORT=${QUERY_PORT:-8080}

# Paths
BINARY="/usr/bin/puniversald"
HOME_DIR="/root/.puniversal"

echo "🚨 Starting universal validator $UNIVERSAL_ID setup..."
echo "Core validator GRPC: $CORE_VALIDATOR_GRPC"
echo "Query port: $QUERY_PORT"

# ---------------------------
# === WAIT FOR CORE VALIDATOR ===
# ---------------------------

echo "⏳ Waiting for core validator to be ready..."

# Extract host and port from GRPC endpoint
CORE_HOST=$(echo $CORE_VALIDATOR_GRPC | cut -d: -f1)
CORE_GRPC_PORT=$(echo $CORE_VALIDATOR_GRPC | cut -d: -f2)

# Wait for core validator GRPC to be accessible
max_attempts=60
attempt=0
while [ $attempt -lt $max_attempts ]; do
  # Try to connect to GRPC port
  if nc -z "$CORE_HOST" "$CORE_GRPC_PORT" 2>/dev/null; then
    echo "✅ Core validator GRPC is ready!"
    break
  fi
  echo "Waiting for core validator GRPC... (attempt $((attempt + 1))/$max_attempts)"
  sleep 5
  attempt=$((attempt + 1))
done

if [ $attempt -eq $max_attempts ]; then
  echo "❌ Core validator GRPC not ready after ${max_attempts} attempts"
  exit 1
fi

# ---------------------------
# === WAIT FOR FIRST BLOCK ===
# ---------------------------

echo "⏳ Waiting for core validator to produce first block..."

# Core validator RPC is on port 26657 (same host as GRPC)
CORE_RPC_PORT=26657

max_block_attempts=120
block_attempt=0
while [ $block_attempt -lt $max_block_attempts ]; do
  # Query the status endpoint and check block height
  BLOCK_HEIGHT=$(curl -s "http://$CORE_HOST:$CORE_RPC_PORT/status" 2>/dev/null | jq -r '.result.sync_info.latest_block_height // "0"' 2>/dev/null || echo "0")

  if [ "$BLOCK_HEIGHT" != "0" ] && [ "$BLOCK_HEIGHT" != "null" ] && [ "$BLOCK_HEIGHT" != "" ]; then
    echo "✅ Core validator has produced blocks! Current height: $BLOCK_HEIGHT"
    break
  fi

  echo "Waiting for first block... (attempt $((block_attempt + 1))/$max_block_attempts)"
  sleep 2
  block_attempt=$((block_attempt + 1))
done

if [ $block_attempt -eq $max_block_attempts ]; then
  echo "❌ Core validator did not produce blocks after ${max_block_attempts} attempts"
  exit 1
fi

# ---------------------------
# === INITIALIZATION ===
# ---------------------------

# Clean start
rm -rf "$HOME_DIR"/* "$HOME_DIR"/.[!.]* "$HOME_DIR"/..?* 2>/dev/null || true

echo "🔧 Initializing universal validator..."

# Initialize puniversald (creates config directory and default config)
$BINARY init

# Update the gRPC URL and keyring backend in the config
# The CORE_VALIDATOR_GRPC env var is already set correctly in docker-compose.yml:
# - universal-validator-1 uses core-validator-1:9090
# - universal-validator-2 uses core-validator-2:9090
# - universal-validator-3 uses core-validator-3:9090
jq '.push_chain_grpc_urls = ["'$CORE_VALIDATOR_GRPC'"] | .keyring_backend = "test"' \
  "$HOME_DIR/config/pushuv_config.json" > "$HOME_DIR/config/pushuv_config.json.tmp" && \
  mv "$HOME_DIR/config/pushuv_config.json.tmp" "$HOME_DIR/config/pushuv_config.json"

# Enable debug logging (log_level 0 = debug)
jq '.log_level = 0' \
  "$HOME_DIR/config/pushuv_config.json" > "$HOME_DIR/config/pushuv_config.json.tmp" && \
  mv "$HOME_DIR/config/pushuv_config.json.tmp" "$HOME_DIR/config/pushuv_config.json"

# Enable TSS if environment variables are set
TSS_ENABLED=${TSS_ENABLED:-"false"}
if [ "$TSS_ENABLED" = "true" ]; then
  echo "🔑 Enabling TSS..."

  # Generate a deterministic private key based on UNIVERSAL_ID
  # Each validator gets a unique key: 01...01, 02...02, 03...03
  TSS_PRIVATE_KEY=$(printf '%02x' $UNIVERSAL_ID | head -c 2)
  TSS_PRIVATE_KEY=$(yes $TSS_PRIVATE_KEY | head -32 | tr -d '\n')

  TSS_PASSWORD=${TSS_PASSWORD:-"testpassword"}
  TSS_P2P_PORT=$((39000 + UNIVERSAL_ID - 1))
  TSS_P2P_LISTEN="/ip4/0.0.0.0/tcp/$TSS_P2P_PORT"
  TSS_HOME_DIR="$HOME_DIR/tss"

  jq --arg pk "$TSS_PRIVATE_KEY" \
     --arg pw "$TSS_PASSWORD" \
     --arg listen "$TSS_P2P_LISTEN" \
     --arg home "$TSS_HOME_DIR" \
     '.tss_enabled = true | .tss_p2p_private_key_hex = $pk | .tss_password = $pw | .tss_p2p_listen = $listen | .tss_home_dir = $home' \
     "$HOME_DIR/config/pushuv_config.json" > "$HOME_DIR/config/pushuv_config.json.tmp" && \
     mv "$HOME_DIR/config/pushuv_config.json.tmp" "$HOME_DIR/config/pushuv_config.json"

  echo "✅ TSS enabled with P2P listen: $TSS_P2P_LISTEN"
fi

# Also update the query server port if different from default
if [ "$QUERY_PORT" != "8080" ]; then
  jq '.query_server_port = '$QUERY_PORT \
    "$HOME_DIR/config/pushuv_config.json" > "$HOME_DIR/config/pushuv_config.json.tmp" && \
    mv "$HOME_DIR/config/pushuv_config.json.tmp" "$HOME_DIR/config/pushuv_config.json"
fi

echo "📋 Universal validator config created:"
cat "$HOME_DIR/config/pushuv_config.json"

# ---------------------------
# === IMPORT HOTKEY ===
# ---------------------------

echo "🔑 Importing pre-generated hotkey for universal validator..."

# Create keyring directory
mkdir -p "$HOME_DIR/keyring-test"

# The hotkey mnemonic is stored in the shared volume
HOTKEYS_FILE="/tmp/push-accounts/hotkeys.json"
HOTKEY_NAME="hotkey-$UNIVERSAL_ID"

if [ -f "$HOTKEYS_FILE" ]; then
  # Get the hotkey for this validator
  HOTKEY_INDEX=$((UNIVERSAL_ID - 1))
  HOTKEY_MNEMONIC=$(jq -r ".[$HOTKEY_INDEX].mnemonic" "$HOTKEYS_FILE")
  HOTKEY_ADDRESS=$(jq -r ".[$HOTKEY_INDEX].address" "$HOTKEYS_FILE")

  echo "Importing hotkey: $HOTKEY_NAME"
  echo "Expected address: $HOTKEY_ADDRESS"

  # Import the pre-generated hotkey
  echo "$HOTKEY_MNEMONIC" | $BINARY keys add "$HOTKEY_NAME" --recover --keyring-backend test --home "$HOME_DIR" 2>&1 || {
    echo "Key may already exist, checking..."
    $BINARY keys show "$HOTKEY_NAME" --keyring-backend test --home "$HOME_DIR" 2>&1
  }

  # Display key info
  echo "✅ Hotkey imported:"
  $BINARY keys show "$HOTKEY_NAME" --keyring-backend test --home "$HOME_DIR" 2>&1 || true
else
  echo "⚠️ Hotkeys file not found at $HOTKEYS_FILE"
  echo "Creating a new random hotkey instead..."
  $BINARY keys add "$HOTKEY_NAME" --keyring-backend test --home "$HOME_DIR" 2>&1 || true
fi

# ---------------------------
# === AUTHZ NOTE ===
# ---------------------------

echo "🔐 AuthZ permissions are granted from the core validator during genesis setup"
echo "📝 Universal validator registration is done from core validators after bonding"

# ---------------------------
# === START UNIVERSAL VALIDATOR ===
# ---------------------------

echo "🚀 Starting universal validator $UNIVERSAL_ID..."
echo "🔗 Connecting to core validator: $CORE_VALIDATOR_GRPC"

exec $BINARY start