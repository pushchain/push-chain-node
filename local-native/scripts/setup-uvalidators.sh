#!/bin/bash
set -eu

# Load environment
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$SCRIPT_DIR/env.sh"

# Use validator 1's home for transactions
HOME_DIR="$DATA_DIR/validator1/.pchain"
RPC_NODE="tcp://127.0.0.1:26657"

# Pre-computed peer IDs for each UV (computed from deterministic private keys)
# Key format: 0101...01, 0202...02, 0303...03, 0404...04
PEER_ID_1="12D3KooWK99VoVxNE7XzyBwXEzW7xhK7Gpv85r9F3V3fyKSUKPH5"
PEER_ID_2="12D3KooWJWoaqZhDaoEFshF7Rh1bpY9ohihFhzcW6d69Lr2NASuq"
PEER_ID_3="12D3KooWRndVhVZPCiQwHBBBdg769GyrPUW13zxwqQyf9r3ANaba"
PEER_ID_4="12D3KooWPT98FXMfDQYavZm66EeVjTqP9Nnehn1gyaydqV8L8BQw"

get_peer_id() {
    case "$1" in
        1) echo "$PEER_ID_1" ;;
        2) echo "$PEER_ID_2" ;;
        3) echo "$PEER_ID_3" ;;
        4) echo "$PEER_ID_4" ;;
    esac
}

get_tss_port() {
    echo $((39000 + $1 - 1))
}

echo "🔧 Setting up Universal Validators..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Wait for chain to be ready
echo "⏳ Waiting for chain to be ready..."
max_attempts=60
attempt=0
while [ $attempt -lt $max_attempts ]; do
    height=$(curl -s "http://127.0.0.1:26657/status" 2>/dev/null | jq -r '.result.sync_info.latest_block_height // "0"' 2>/dev/null || echo "0")
    if [ "$height" != "0" ] && [ "$height" != "null" ]; then
        echo "✅ Chain is ready! Block height: $height"
        break
    fi
    sleep 2
    attempt=$((attempt + 1))
done

if [ $attempt -eq $max_attempts ]; then
    echo "❌ Chain not ready after $max_attempts attempts"
    exit 1
fi

# Load account files
VALIDATORS_FILE="$ACCOUNTS_DIR/validators.json"
HOTKEYS_FILE="$ACCOUNTS_DIR/hotkeys.json"

# ═══════════════════════════════════════════════════════════════════════════════
# REGISTER UNIVERSAL VALIDATORS
# ═══════════════════════════════════════════════════════════════════════════════

echo ""
echo "📝 Registering Universal Validators..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

for i in 1 2 3 4; do
    echo ""
    echo "📋 Registering universal-validator-$i"
    
    # Get valoper address for this validator
    VALOPER_ADDR=$("$PCHAIND_BIN" keys show "validator-$i" --bech val -a --keyring-backend "$KEYRING" --home "$HOME_DIR" 2>/dev/null)
    PEER_ID=$(get_peer_id $i)
    TSS_PORT=$(get_tss_port $i)
    
    # For local native, use localhost multiaddr
    MULTI_ADDR="/ip4/127.0.0.1/tcp/$TSS_PORT"
    NETWORK_JSON="{\"peer_id\": \"$PEER_ID\", \"multi_addrs\": [\"$MULTI_ADDR\"]}"
    
    echo "   Valoper: $VALOPER_ADDR"
    echo "   Peer ID: $PEER_ID"
    echo "   Multi-addr: $MULTI_ADDR"
    
    # Check if already registered
    EXISTING=$("$PCHAIND_BIN" query uvalidator all-universal-validators --node="$RPC_NODE" --output json 2>/dev/null | jq -r --arg pid "$PEER_ID" '.universal_validator[]? | select(.network_info.peer_id == $pid) | .network_info.peer_id' 2>/dev/null || echo "")
    
    if [ -n "$EXISTING" ]; then
        echo "   ✅ Already registered"
        continue
    fi
    
    # Register universal validator
    RESULT=$("$PCHAIND_BIN" tx uvalidator add-universal-validator \
        --core-validator-address "$VALOPER_ADDR" \
        --network "$NETWORK_JSON" \
        --from genesis-acc-1 \
        --chain-id "$CHAIN_ID" \
        --keyring-backend "$KEYRING" \
        --home "$HOME_DIR" \
        --node="$RPC_NODE" \
        --fees 1000000000000000upc \
        --yes \
        --output json 2>&1 || echo "{}")
    
    if echo "$RESULT" | grep -q '"txhash"'; then
        TX_HASH=$(echo "$RESULT" | jq -r '.txhash' 2>/dev/null)
        echo "   ✅ Registered! TX: $TX_HASH"
    else
        echo "   ⚠️ Registration may have failed"
    fi
    
    sleep 2  # Wait between registrations
done

# ═══════════════════════════════════════════════════════════════════════════════
# CREATE AUTHZ GRANTS (batched - 4 grants per transaction)
# ═══════════════════════════════════════════════════════════════════════════════

echo ""
echo "🔐 Setting up AuthZ grants (batched)..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📋 Creating grants: validator-N → hotkey-N (4 msg types per tx)"

# Disable exit on error for authz commands (some may already exist)
set +e

TOTAL_GRANTS=0
TEMP_DIR=$(mktemp -d)

MSG_TYPES=(
    "/uexecutor.v1.MsgVoteInbound"
    "/uexecutor.v1.MsgVoteGasPrice"
    "/uexecutor.v1.MsgVoteOutbound"
    "/utss.v1.MsgVoteTssKeyProcess"
)

for i in 1 2 3 4; do
    HOTKEY_ADDR=$(jq -r ".[$((i-1))].address" "$HOTKEYS_FILE")
    VALIDATOR_ADDR=$("$PCHAIND_BIN" keys show "validator-$i" -a --keyring-backend "$KEYRING" --home "$HOME_DIR" 2>/dev/null)
    
    if [ -z "$HOTKEY_ADDR" ] || [ -z "$VALIDATOR_ADDR" ]; then
        echo "⚠️ Missing addresses for validator-$i"
        continue
    fi
    
    echo ""
    echo "📋 validator-$i → hotkey-$i (4 grants in 1 tx)"
    echo "   Granter: $VALIDATOR_ADDR"
    echo "   Grantee: $HOTKEY_ADDR"
    
    # Generate unsigned txs for all 4 message types
    MESSAGES="[]"
    for j in "${!MSG_TYPES[@]}"; do
        MSG_TYPE="${MSG_TYPES[$j]}"
        
        # Generate unsigned tx
        UNSIGNED_TX=$("$PCHAIND_BIN" tx authz grant "$HOTKEY_ADDR" generic \
            --msg-type="$MSG_TYPE" \
            --from "validator-$i" \
            --chain-id "$CHAIN_ID" \
            --keyring-backend "$KEYRING" \
            --home "$HOME_DIR" \
            --node="$RPC_NODE" \
            --gas=50000 \
            --gas-prices="1000000000upc" \
            --generate-only 2>/dev/null)
        
        # Extract the message and add to array
        MSG=$(echo "$UNSIGNED_TX" | jq -c '.body.messages[0]' 2>/dev/null)
        if [ -n "$MSG" ] && [ "$MSG" != "null" ]; then
            MESSAGES=$(echo "$MESSAGES" | jq --argjson msg "$MSG" '. + [$msg]')
        fi
    done
    
    # Create combined transaction with all 4 messages
    COMBINED_TX=$(cat <<EOF
{
  "body": {
    "messages": $MESSAGES,
    "memo": "",
    "timeout_height": "0",
    "extension_options": [],
    "non_critical_extension_options": []
  },
  "auth_info": {
    "signer_infos": [],
    "fee": {
      "amount": [{"denom": "upc", "amount": "400000000000000"}],
      "gas_limit": "400000",
      "payer": "",
      "granter": ""
    },
    "tip": null
  },
  "signatures": []
}
EOF
)
    
    # Save combined tx
    echo "$COMBINED_TX" > "$TEMP_DIR/combined_tx_$i.json"
    
    # Sign the combined transaction
    SIGNED_TX=$("$PCHAIND_BIN" tx sign "$TEMP_DIR/combined_tx_$i.json" \
        --from "validator-$i" \
        --chain-id "$CHAIN_ID" \
        --keyring-backend "$KEYRING" \
        --home "$HOME_DIR" \
        --node="$RPC_NODE" \
        --output-document="$TEMP_DIR/signed_tx_$i.json" 2>&1)
    
    # Broadcast the signed transaction
    BROADCAST_RESULT=$("$PCHAIND_BIN" tx broadcast "$TEMP_DIR/signed_tx_$i.json" \
        --node="$RPC_NODE" \
        --broadcast-mode sync 2>&1)
    
    # Check result
    if echo "$BROADCAST_RESULT" | grep -q "txhash"; then
        TX_HASH=$(echo "$BROADCAST_RESULT" | grep -o 'txhash: [A-F0-9]*' | cut -d' ' -f2 || echo "$BROADCAST_RESULT" | jq -r '.txhash' 2>/dev/null)
        echo "   ✅ 4 grants created! TX: ${TX_HASH:0:16}..."
        TOTAL_GRANTS=$((TOTAL_GRANTS + 4))
    else
        echo "   ⚠️ Batch may have failed, trying individual grants..."
        # Fallback to individual grants
        for MSG_TYPE in "${MSG_TYPES[@]}"; do
            MSG_NAME=$(basename "$MSG_TYPE")
            GRANT_RESULT=$("$PCHAIND_BIN" tx authz grant "$HOTKEY_ADDR" generic \
                --msg-type="$MSG_TYPE" \
                --from "validator-$i" \
                --chain-id "$CHAIN_ID" \
                --keyring-backend "$KEYRING" \
                --home "$HOME_DIR" \
                --node="$RPC_NODE" \
                --gas=auto \
                --gas-adjustment=1.5 \
                --gas-prices="1000000000upc" \
                --yes 2>&1)
            
            if echo "$GRANT_RESULT" | grep -q "txhash"; then
                TOTAL_GRANTS=$((TOTAL_GRANTS + 1))
            fi
            sleep 2
        done
    fi
    
    sleep 2  # Wait between validators
done

# Cleanup
rm -rf "$TEMP_DIR"

set -e

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📊 Total AuthZ grants created: $TOTAL_GRANTS/16"

if [ "$TOTAL_GRANTS" -ge 16 ]; then
    echo "✅ All grants created successfully!"
else
    echo "⚠️ Some grants may be missing"
fi

echo ""
echo "✅ Universal validator setup complete!"
echo ""
echo "You can now start universal validators with:"
echo "  ./devnet start-uv"
