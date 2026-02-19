#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOCAL_NATIVE_DIR="$(cd -P "$SCRIPT_DIR/.." && pwd)"
DATA_DIR="$LOCAL_NATIVE_DIR/data"

require_bin() {
    local bin="$1"
    if ! command -v "$bin" >/dev/null 2>&1; then
        echo "❌ Required binary not found: $bin"
        exit 1
    fi
}

require_bin curl
require_bin jq

SEPOLIA_CHAIN_ID="eip155:11155111"
DEFAULT_RPC_URL="https://sepolia.drpc.org"

# Prefer RPC URL from existing config, fallback to default.
detect_rpc_url() {
    local cfg="$1"
    jq -r --arg chain "$SEPOLIA_CHAIN_ID" '.chain_configs[$chain].rpc_url[0] // empty' "$cfg" 2>/dev/null || true
}

fetch_sepolia_height() {
    local rpc_url="$1"
    local response
    response=$(curl -sS -X POST "$rpc_url" \
        -H "Content-Type: application/json" \
        --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}')

    local hex_height
    hex_height=$(echo "$response" | jq -r '.result // empty')

    if [[ -z "$hex_height" || "$hex_height" == "null" || ! "$hex_height" =~ ^0x[0-9a-fA-F]+$ ]]; then
        return 1
    fi

    echo "$((16#${hex_height#0x}))"
}

find_pushuv_configs() {
    find "$DATA_DIR" -type f -path '*/.puniversal/config/pushuv_config.json' | sort
}

print_only_height() {
    local rpc_url="$DEFAULT_RPC_URL"
    local height=""

    if ! height=$(fetch_sepolia_height "$rpc_url"); then
        echo "❌ Failed to fetch Sepolia block height from $rpc_url" >&2
        exit 1
    fi

    echo "$height"
}

main() {
    if [ "${1:-}" = "--get-height" ]; then
        print_only_height
        return 0
    fi

    local configs=()
    while IFS= read -r cfg; do
        configs+=("$cfg")
    done < <(find_pushuv_configs)

    if [ "${#configs[@]}" -eq 0 ]; then
        echo "❌ No pushuv_config.json files found under $DATA_DIR"
        echo "   Start universal validators first with: ./devnet start-uv 4"
        exit 1
    fi

    local rpc_url=""
    rpc_url=$(detect_rpc_url "${configs[0]}")
    if [ -z "$rpc_url" ]; then
        rpc_url="$DEFAULT_RPC_URL"
    fi

    local height=""
    if ! height=$(fetch_sepolia_height "$rpc_url"); then
        echo "⚠️  Failed using configured RPC ($rpc_url), retrying default RPC ($DEFAULT_RPC_URL)..."
        if ! height=$(fetch_sepolia_height "$DEFAULT_RPC_URL"); then
            echo "❌ Failed to fetch Sepolia block height from both RPC endpoints"
            exit 1
        fi
        rpc_url="$DEFAULT_RPC_URL"
    fi

    echo "ℹ️  Sepolia latest block height: $height"
    echo "ℹ️  RPC used: $rpc_url"

    local updated=0
    for cfg in "${configs[@]}"; do
        local tmp
        tmp=$(mktemp)
        jq --arg chain "$SEPOLIA_CHAIN_ID" --argjson height "$height" \
            '.chain_configs[$chain].event_start_from = $height' \
            "$cfg" > "$tmp"
        mv "$tmp" "$cfg"
        updated=$((updated + 1))
        echo "✅ Updated: $cfg"
    done

    echo "🎉 Updated event_start_from for $updated config file(s)."
}

main "$@"
