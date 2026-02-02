#!/bin/bash
set -eu

# Load environment
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$SCRIPT_DIR/env.sh"

echo "🔑 Generating accounts for local-native setup..."

# Create accounts directory
mkdir -p "$ACCOUNTS_DIR"

# Temporary keyring for generation (use a unique temp dir)
TEMP_HOME=$(mktemp -d)
trap "rm -rf $TEMP_HOME" EXIT

# Generate 5 genesis accounts
echo "📋 Generating 5 genesis accounts..."
GENESIS_ACCOUNTS="[]"
for i in 1 2 3 4 5; do
    # Generate key and capture output
    output=$("$PCHAIND_BIN" keys add "genesis-acc-$i" \
        --keyring-backend test \
        --algo eth_secp256k1 \
        --home "$TEMP_HOME" \
        --output json 2>&1)
    
    # Extract mnemonic from JSON output (it's in the "mnemonic" field)
    mnemonic=$(echo "$output" | jq -r '.mnemonic // empty' 2>/dev/null)
    
    # If that didn't work, try to get it from the last line (older versions output mnemonic at end)
    if [ -z "$mnemonic" ]; then
        # The mnemonic is typically the last line after "**Important**"
        mnemonic=$(echo "$output" | grep -A1 "Important" | tail -1 | tr -d '\n')
    fi
    
    # Get address
    address=$("$PCHAIND_BIN" keys show "genesis-acc-$i" -a --keyring-backend test --home "$TEMP_HOME" 2>/dev/null)
    
    echo "  genesis-acc-$i: $address"
    
    GENESIS_ACCOUNTS=$(echo "$GENESIS_ACCOUNTS" | jq --arg m "$mnemonic" --arg a "$address" --argjson i "$i" \
        '. + [{"id": $i, "name": "genesis-acc-\($i)", "address": $a, "mnemonic": $m}]')
done

echo "$GENESIS_ACCOUNTS" > "$ACCOUNTS_DIR/genesis_accounts.json"
echo "✅ Genesis accounts saved"

# Generate 4 validator accounts
echo "📋 Generating 4 validator accounts..."
VALIDATORS="[]"
for i in 1 2 3 4; do
    output=$("$PCHAIND_BIN" keys add "validator-$i" \
        --keyring-backend test \
        --algo eth_secp256k1 \
        --home "$TEMP_HOME" \
        --output json 2>&1)
    
    mnemonic=$(echo "$output" | jq -r '.mnemonic // empty' 2>/dev/null)
    if [ -z "$mnemonic" ]; then
        mnemonic=$(echo "$output" | grep -A1 "Important" | tail -1 | tr -d '\n')
    fi
    
    address=$("$PCHAIND_BIN" keys show "validator-$i" -a --keyring-backend test --home "$TEMP_HOME" 2>/dev/null)
    valoper=$("$PCHAIND_BIN" keys show "validator-$i" --bech val -a --keyring-backend test --home "$TEMP_HOME" 2>/dev/null)
    
    echo "  validator-$i: $address"
    
    VALIDATORS=$(echo "$VALIDATORS" | jq --arg m "$mnemonic" --arg a "$address" --arg v "$valoper" --argjson i "$i" \
        '. + [{"id": $i, "name": "validator-\($i)", "address": $a, "valoper_address": $v, "mnemonic": $m}]')
done

echo "$VALIDATORS" > "$ACCOUNTS_DIR/validators.json"
echo "✅ Validator accounts saved"

# Generate 4 hotkey accounts
echo "📋 Generating 4 hotkey accounts..."
HOTKEYS="[]"
for i in 1 2 3 4; do
    output=$("$PCHAIND_BIN" keys add "hotkey-$i" \
        --keyring-backend test \
        --algo eth_secp256k1 \
        --home "$TEMP_HOME" \
        --output json 2>&1)
    
    mnemonic=$(echo "$output" | jq -r '.mnemonic // empty' 2>/dev/null)
    if [ -z "$mnemonic" ]; then
        mnemonic=$(echo "$output" | grep -A1 "Important" | tail -1 | tr -d '\n')
    fi
    
    address=$("$PCHAIND_BIN" keys show "hotkey-$i" -a --keyring-backend test --home "$TEMP_HOME" 2>/dev/null)
    
    echo "  hotkey-$i: $address"
    
    HOTKEYS=$(echo "$HOTKEYS" | jq --arg m "$mnemonic" --arg a "$address" --argjson i "$i" \
        '. + [{"id": $i, "name": "hotkey-\($i)", "address": $a, "mnemonic": $m}]')
done

echo "$HOTKEYS" > "$ACCOUNTS_DIR/hotkeys.json"
echo "✅ Hotkey accounts saved"

echo ""
echo "📁 Accounts saved to: $ACCOUNTS_DIR"
echo "   - genesis_accounts.json (5 accounts)"
echo "   - validators.json (4 accounts)"
echo "   - hotkeys.json (4 accounts)"
