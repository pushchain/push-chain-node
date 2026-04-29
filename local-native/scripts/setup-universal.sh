#!/bin/bash
set -eu

# Load environment
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$SCRIPT_DIR/env.sh"

# Universal validator specific config
UNIVERSAL_ID=${UNIVERSAL_ID:-1}
# HOME_DIR will be set after we set HOME env var

# Deterministic local SVM relayer used by the local-native devnet. The relayer
# pays the Solana transaction fee for SVM outbound broadcasts.
DEFAULT_SOLANA_RELAYER_PUBKEY="AdWDRaQfvWJqW4TaxTrXP5WogCWJMJBrtBfGjjHUDADM"
DEFAULT_SOLANA_RELAYER_KEYPAIR_JSON='[226,7,176,193,18,2,55,106,191,150,176,87,157,216,118,97,236,128,2,104,181,206,160,147,5,152,0,115,23,8,103,189,143,19,31,194,227,248,222,123,219,13,143,47,154,104,201,235,13,16,11,45,117,154,117,37,130,196,58,154,89,228,136,32]'
DEFAULT_SOLANA_RELAYER_UNIVERSAL_ID=""

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

provision_svm_relayer_keypair() {
    local relayer_dir="$HOME_DIR/relayer"
    local key_path="$relayer_dir/solana.json"
    local keypair_json="${SOLANA_RELAYER_KEYPAIR_JSON:-$DEFAULT_SOLANA_RELAYER_KEYPAIR_JSON}"

    mkdir -p "$relayer_dir"
    printf '%s\n' "$keypair_json" > "$key_path"
    chmod 600 "$key_path"
    echo "✅ Provisioned Solana relayer keypair: $key_path"
}

fund_default_svm_relayer() {
    local rpc_url="${SOLANA_RPC_URL_OVERRIDE:-${LOCAL_SOLANA_UV_RPC_URL:-${SURFPOOL_SOLANA_HOST_RPC_URL:-}}}"
    local lamports="${SOLANA_RELAYER_AIRDROP_LAMPORTS:-10000000000}"
    local relayer_pubkey="${SOLANA_RELAYER_PUBKEY:-$DEFAULT_SOLANA_RELAYER_PUBKEY}"
    local response=""

    [ -n "$rpc_url" ] || return 0

    response=$(curl -sS --max-time 10 -X POST "$rpc_url" \
        -H 'Content-Type: application/json' \
        --data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"requestAirdrop\",\"params\":[\"$relayer_pubkey\",$lamports]}" 2>/dev/null || true)

    if echo "$response" | jq -e '.result // empty' >/dev/null 2>&1; then
        echo "✅ Requested Solana relayer airdrop for $relayer_pubkey via $rpc_url"
    elif [ -n "$response" ]; then
        echo "⚠️  Solana relayer airdrop was not accepted by $rpc_url: $response"
    fi
}

configured_solana_relayer_universal_id="${SOLANA_RELAYER_UNIVERSAL_ID:-$DEFAULT_SOLANA_RELAYER_UNIVERSAL_ID}"
if [ -z "$configured_solana_relayer_universal_id" ] || [ "$configured_solana_relayer_universal_id" = "$UNIVERSAL_ID" ]; then
    provision_svm_relayer_keypair
    fund_default_svm_relayer
else
    echo "ℹ️  Skipping Solana relayer keypair on universal validator $UNIVERSAL_ID; relayer owner is universal validator $configured_solana_relayer_universal_id"
fi

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

# Apply chain RPC URL overrides if set (e.g. for LOCAL anvil forks)
apply_rpc_override() {
    local chain_id="$1" rpc_url="$2"
    [ -n "$rpc_url" ] || return 0
    jq --arg c "$chain_id" --arg u "$rpc_url" \
       '.chain_configs[$c].rpc_urls = [$u]' \
       "$HOME_DIR/config/pushuv_config.json" > "$HOME_DIR/config/pushuv_config.json.tmp" && \
       mv "$HOME_DIR/config/pushuv_config.json.tmp" "$HOME_DIR/config/pushuv_config.json"
}

apply_event_start_override() {
    local chain_id="$1" height="$2"
    [ -n "$height" ] && [[ "$height" =~ ^[0-9]+$ ]] || return 0
    jq --arg c "$chain_id" --argjson h "$height" \
       '.chain_configs[$c].event_start_from = $h' \
       "$HOME_DIR/config/pushuv_config.json" > "$HOME_DIR/config/pushuv_config.json.tmp" && \
       mv "$HOME_DIR/config/pushuv_config.json.tmp" "$HOME_DIR/config/pushuv_config.json"
}

apply_rpc_override "eip155:11155111"                              "${SEPOLIA_RPC_URL_OVERRIDE:-}"
apply_rpc_override "eip155:421614"                               "${ARBITRUM_RPC_URL_OVERRIDE:-}"
apply_rpc_override "eip155:84532"                                "${BASE_RPC_URL_OVERRIDE:-}"
apply_rpc_override "eip155:97"                                   "${BSC_RPC_URL_OVERRIDE:-}"
apply_rpc_override "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"    "${SOLANA_RPC_URL_OVERRIDE:-}"

apply_event_start_override "eip155:421614"                               "${ARBITRUM_EVENT_START_FROM:-}"
apply_event_start_override "eip155:84532"                                "${BASE_EVENT_START_FROM:-}"
apply_event_start_override "eip155:97"                                   "${BSC_EVENT_START_FROM:-}"
apply_event_start_override "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"    "${SOLANA_EVENT_START_FROM:-}"

# Always start from block 1 for the local devnet chain so UVs see TSS key processes immediately
apply_event_start_override "localchain_9000-1" "1"
apply_event_start_override "push_42101-1" "1"

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
