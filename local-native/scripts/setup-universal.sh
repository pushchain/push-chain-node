#!/bin/bash
set -eu

# Load environment
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$SCRIPT_DIR/env.sh"

# Universal validator specific config
UNIVERSAL_ID=${UNIVERSAL_ID:-1}
# HOME_DIR will be set after we set HOME env var

# Ports
case "$UNIVERSAL_ID" in
    1) CORE_GRPC_PORT=9090; QUERY_PORT=8080; CORE_RPC_PORT=26657 ;;
    2) CORE_GRPC_PORT=9093; QUERY_PORT=8081; CORE_RPC_PORT=26658 ;;
    3) CORE_GRPC_PORT=9095; QUERY_PORT=8082; CORE_RPC_PORT=26659 ;;
    4) CORE_GRPC_PORT=9097; QUERY_PORT=8083; CORE_RPC_PORT=26660 ;;
    *) echo "Invalid UNIVERSAL_ID: $UNIVERSAL_ID"; exit 1 ;;
esac
TSS_PORT=$((39000 + UNIVERSAL_ID - 1))

CORE_GRPC="127.0.0.1:$CORE_GRPC_PORT"

echo "🚨 Starting universal validator $UNIVERSAL_ID..."
echo "Core gRPC: $CORE_GRPC"

# Wait for core validator gRPC
echo "⏳ Waiting for core validator gRPC..."
max_attempts=60
attempt=0
while [ $attempt -lt $max_attempts ]; do
    if nc -z 127.0.0.1 "$CORE_GRPC_PORT" 2>/dev/null; then
        echo "✅ Core validator gRPC is ready!"
        break
    fi
    sleep 2
    attempt=$((attempt + 1))
done

if [ $attempt -eq $max_attempts ]; then
    echo "❌ Core validator gRPC not ready"
    exit 1
fi

# Wait for blocks
echo "⏳ Waiting for blocks..."
max_block_attempts=120
block_attempt=0
while [ $block_attempt -lt $max_block_attempts ]; do
    height=$(curl -s "http://127.0.0.1:$CORE_RPC_PORT/status" 2>/dev/null | jq -r '.result.sync_info.latest_block_height // "0"' 2>/dev/null || echo "0")
    if [ "$height" != "0" ] && [ "$height" != "null" ]; then
        echo "✅ Core validator producing blocks! Height: $height"
        break
    fi
    sleep 2
    block_attempt=$((block_attempt + 1))
done

# Initialize
# puniversald uses HOME to determine ~/.puniversal location
# So we set a custom HOME for each universal validator
UV_HOME="$DATA_DIR/universal$UNIVERSAL_ID"

# Only remove the .puniversal directory, not the entire UV_HOME (which contains logs)
rm -rf "$UV_HOME/.puniversal"
mkdir -p "$UV_HOME"

# Set HOME so puniversald creates config in our custom directory
export HOME="$UV_HOME"
HOME_DIR="$UV_HOME/.puniversal"

"$PUNIVERSALD_BIN" init

# Update config
jq --arg grpc "$CORE_GRPC" '.push_chain_grpc_urls = [$grpc] | .keyring_backend = "test"' \
    "$HOME_DIR/config/pushuv_config.json" > "$HOME_DIR/config/pushuv_config.json.tmp" && \
    mv "$HOME_DIR/config/pushuv_config.json.tmp" "$HOME_DIR/config/pushuv_config.json"

# Enable debug logging
jq '.log_level = 0' \
    "$HOME_DIR/config/pushuv_config.json" > "$HOME_DIR/config/pushuv_config.json.tmp" && \
    mv "$HOME_DIR/config/pushuv_config.json.tmp" "$HOME_DIR/config/pushuv_config.json"

# Update query server port
jq --argjson port "$QUERY_PORT" '.query_server_port = $port' \
    "$HOME_DIR/config/pushuv_config.json" > "$HOME_DIR/config/pushuv_config.json.tmp" && \
    mv "$HOME_DIR/config/pushuv_config.json.tmp" "$HOME_DIR/config/pushuv_config.json"

# Optionally override Sepolia event start height (set by ./devnet start-uv)
if [ -n "${SEPOLIA_EVENT_START_FROM:-}" ]; then
    jq --argjson height "$SEPOLIA_EVENT_START_FROM" \
       '.chain_configs["eip155:11155111"].event_start_from = $height' \
       "$HOME_DIR/config/pushuv_config.json" > "$HOME_DIR/config/pushuv_config.json.tmp" && \
       mv "$HOME_DIR/config/pushuv_config.json.tmp" "$HOME_DIR/config/pushuv_config.json"
fi

# Enable TSS
TSS_PRIVATE_KEY=$(printf '%02x' $UNIVERSAL_ID | head -c 2)
TSS_PRIVATE_KEY=$(yes $TSS_PRIVATE_KEY | head -32 | tr -d '\n')
TSS_P2P_LISTEN="/ip4/0.0.0.0/tcp/$TSS_PORT"
TSS_HOME_DIR="$HOME_DIR/tss"

jq --arg pk "$TSS_PRIVATE_KEY" \
   --arg pw "testpassword" \
   --arg listen "$TSS_P2P_LISTEN" \
   --arg home "$TSS_HOME_DIR" \
   '.tss_enabled = true | .tss_p2p_private_key_hex = $pk | .tss_password = $pw | .tss_p2p_listen = $listen | .tss_home_dir = $home' \
   "$HOME_DIR/config/pushuv_config.json" > "$HOME_DIR/config/pushuv_config.json.tmp" && \
   mv "$HOME_DIR/config/pushuv_config.json.tmp" "$HOME_DIR/config/pushuv_config.json"

# Set valoper address
VALIDATORS_FILE="$ACCOUNTS_DIR/validators.json"
if [ -f "$VALIDATORS_FILE" ]; then
    VALOPER_ADDR=$(jq -r ".[$((UNIVERSAL_ID-1))].valoper_address" "$VALIDATORS_FILE")
    if [ -n "$VALOPER_ADDR" ] && [ "$VALOPER_ADDR" != "null" ]; then
        jq --arg valoper "$VALOPER_ADDR" '.push_valoper_address = $valoper' \
            "$HOME_DIR/config/pushuv_config.json" > "$HOME_DIR/config/pushuv_config.json.tmp" && \
            mv "$HOME_DIR/config/pushuv_config.json.tmp" "$HOME_DIR/config/pushuv_config.json"
    fi
fi

# Import hotkey
echo "🔑 Importing hotkey..."
HOTKEYS_FILE="$ACCOUNTS_DIR/hotkeys.json"
HOTKEY_NAME="hotkey-$UNIVERSAL_ID"

if [ -f "$HOTKEYS_FILE" ]; then
    hotkey_mnemonic=$(jq -r ".[$((UNIVERSAL_ID-1))].mnemonic" "$HOTKEYS_FILE")
    echo "$hotkey_mnemonic" | "$PUNIVERSALD_BIN" keys add "$HOTKEY_NAME" --recover --keyring-backend test --home "$HOME_DIR" 2>/dev/null || true
fi

echo "📋 Config:"
cat "$HOME_DIR/config/pushuv_config.json"

# Start (HOME env var is already set so puniversald will use $HOME/.puniversal)
echo "🚀 Starting universal validator $UNIVERSAL_ID..."
echo "Home directory: $HOME_DIR"
exec "$PUNIVERSALD_BIN" start
