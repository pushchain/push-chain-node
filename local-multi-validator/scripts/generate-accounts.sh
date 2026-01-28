#!/bin/bash

########################################################
# Generate Genesis and Validator Accounts for Multi-Validator Setup
# Based on testnet/v1/pre-setup/generate_genesis_accounts.sh
# Creates accounts with known mnemonics and stores them in tmp files
########################################################

set -eu

KEYRING="test"
KEYALGO="eth_secp256k1"
BINARY="pchaind"

# Output directory for temporary files
TMP_DIR="/tmp/push-accounts"
mkdir -p "$TMP_DIR"

echo "🔐 Generating accounts for multi-validator setup..."
echo "📁 Output directory: $TMP_DIR"

# Check if accounts already exist to avoid regenerating
if [ -f "$TMP_DIR/genesis_accounts.json" ] && [ -f "$TMP_DIR/validators.json" ] && [ -f "$TMP_DIR/hotkeys.json" ]; then
  echo "✅ Account files already exist. Skipping generation."
  echo "📋 Existing files:"
  echo "  - Genesis accounts: $TMP_DIR/genesis_accounts.json"
  echo "  - Validator accounts: $TMP_DIR/validators.json"
  echo "  - Universal validator hotkeys: $TMP_DIR/hotkeys.json"
  echo ""
  echo "🗂️  To force regeneration, delete these files first:"
  echo "  rm -f $TMP_DIR/genesis_accounts.json $TMP_DIR/validators.json $TMP_DIR/hotkeys.json"
  exit 0
fi

# ---------------------------
# === GENESIS FUNDING ACCOUNTS ===
# ---------------------------

echo "💰 Generating 5 genesis funding accounts..."

# Hardcoded admin mnemonic for genesis-acc-1 (matches default admin in uvalidator, uregistry, utss params)
# Address: push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20
ADMIN_MNEMONIC="surface task term spring horse impact tortoise often session cable off catch harvest rain able jealous coral cargo portion surge spring genre mix avoid"

GENESIS_ACCOUNTS_FILE="$TMP_DIR/genesis_accounts.json"
echo "[]" > "$GENESIS_ACCOUNTS_FILE"

for ((i=1; i<=5; i++)); do
  KEY_NAME="genesis-acc-$i"

  # Delete existing key if it exists
  if $BINARY keys show "$KEY_NAME" --keyring-backend="$KEYRING" > /dev/null 2>&1; then
    echo "⚠️  Key $KEY_NAME already exists. Deleting..."
    $BINARY keys delete "$KEY_NAME" --keyring-backend="$KEYRING" -y > /dev/null
  fi

  # For genesis-acc-1, use hardcoded admin mnemonic; for others, generate new
  if [ "$i" -eq 1 ]; then
    echo "🔐 Using hardcoded admin mnemonic for genesis-acc-1..."
    OUTPUT=$(echo "$ADMIN_MNEMONIC" | $BINARY keys add "$KEY_NAME" --keyring-backend="$KEYRING" --algo="$KEYALGO" --recover --output=json 2>/dev/null)
    MNEMONIC="$ADMIN_MNEMONIC"
    ADDRESS=$(echo "$OUTPUT" | jq -r '.address')
  else
    # Generate new account
    OUTPUT=$($BINARY keys add "$KEY_NAME" --keyring-backend="$KEYRING" --algo="$KEYALGO" --output=json 2>/dev/null)
    MNEMONIC=$(echo "$OUTPUT" | jq -r '.mnemonic')
    ADDRESS=$(echo "$OUTPUT" | jq -r '.address')
  fi

  echo "🧾 Genesis Account #$i"
  echo "  Name     : $KEY_NAME"
  echo "  Address  : $ADDRESS"
  echo "  Mnemonic : $MNEMONIC"
  echo

  # Add to JSON array
  ACCOUNT_JSON=$(jq -n --arg name "$KEY_NAME" --arg address "$ADDRESS" --arg mnemonic "$MNEMONIC" '{
    name: $name,
    address: $address,
    mnemonic: $mnemonic
  }')

  jq --argjson account "$ACCOUNT_JSON" '. += [$account]' "$GENESIS_ACCOUNTS_FILE" > "$TMP_DIR/tmp.json" && mv "$TMP_DIR/tmp.json" "$GENESIS_ACCOUNTS_FILE"
done

echo "✅ Genesis accounts saved to: $GENESIS_ACCOUNTS_FILE"

# ---------------------------
# === VALIDATOR ACCOUNTS ===
# ---------------------------

echo "🏛️ Generating validator accounts..."

VALIDATORS_FILE="$TMP_DIR/validators.json"
echo "[]" > "$VALIDATORS_FILE"

for ((i=1; i<=4; i++)); do
  KEY_NAME="validator-$i"

  # Delete existing key if it exists
  if $BINARY keys show "$KEY_NAME" --keyring-backend="$KEYRING" > /dev/null 2>&1; then
    echo "⚠️  Key $KEY_NAME already exists. Deleting..."
    $BINARY keys delete "$KEY_NAME" --keyring-backend="$KEYRING" -y > /dev/null
  fi

  # Generate new validator account
  OUTPUT=$($BINARY keys add "$KEY_NAME" --keyring-backend="$KEYRING" --algo="$KEYALGO" --output=json 2>/dev/null)

  MNEMONIC=$(echo "$OUTPUT" | jq -r '.mnemonic')
  ADDRESS=$(echo "$OUTPUT" | jq -r '.address')

  # Get the valoper address (bech32 with 'val' prefix)
  VALOPER_ADDRESS=$($BINARY keys show "$KEY_NAME" --bech val -a --keyring-backend="$KEYRING" 2>/dev/null)

  echo "👨‍⚖️ Validator #$i"
  echo "  Name     : $KEY_NAME"
  echo "  Address  : $ADDRESS"
  echo "  Valoper  : $VALOPER_ADDRESS"
  echo "  Mnemonic : $MNEMONIC"
  echo

  # Add to JSON array (including valoper address)
  VALIDATOR_JSON=$(jq -n --arg name "$KEY_NAME" --arg address "$ADDRESS" --arg valoper "$VALOPER_ADDRESS" --arg mnemonic "$MNEMONIC" --argjson id "$i" '{
    id: $id,
    name: $name,
    address: $address,
    valoper_address: $valoper,
    mnemonic: $mnemonic
  }')

  jq --argjson validator "$VALIDATOR_JSON" '. += [$validator]' "$VALIDATORS_FILE" > "$TMP_DIR/tmp.json" && mv "$TMP_DIR/tmp.json" "$VALIDATORS_FILE"
done

echo "✅ Validator accounts saved to: $VALIDATORS_FILE"

# ---------------------------
# === UNIVERSAL VALIDATOR HOTKEYS ===
# ---------------------------

echo "🔑 Generating universal validator hotkeys..."

HOTKEYS_FILE="$TMP_DIR/hotkeys.json"
echo "[]" > "$HOTKEYS_FILE"

for ((i=1; i<=4; i++)); do
  KEY_NAME="hotkey-$i"

  # Delete existing key if it exists
  if $BINARY keys show "$KEY_NAME" --keyring-backend="$KEYRING" > /dev/null 2>&1; then
    echo "⚠️  Key $KEY_NAME already exists. Deleting..."
    $BINARY keys delete "$KEY_NAME" --keyring-backend="$KEYRING" -y > /dev/null
  fi

  # Generate new hotkey account
  OUTPUT=$($BINARY keys add "$KEY_NAME" --keyring-backend="$KEYRING" --algo="$KEYALGO" --output=json 2>/dev/null)

  MNEMONIC=$(echo "$OUTPUT" | jq -r '.mnemonic')
  ADDRESS=$(echo "$OUTPUT" | jq -r '.address')

  echo "🔐 Hotkey #$i"
  echo "  Name     : $KEY_NAME"
  echo "  Address  : $ADDRESS"
  echo "  Mnemonic : $MNEMONIC"
  echo

  # Add to JSON array
  HOTKEY_JSON=$(jq -n --arg name "$KEY_NAME" --arg address "$ADDRESS" --arg mnemonic "$MNEMONIC" --argjson id "$i" '{
    id: $id,
    name: $name,
    address: $address,
    mnemonic: $mnemonic
  }')

  jq --argjson hotkey "$HOTKEY_JSON" '. += [$hotkey]' "$HOTKEYS_FILE" > "$TMP_DIR/tmp.json" && mv "$TMP_DIR/tmp.json" "$HOTKEYS_FILE"
done

echo "✅ Hotkey accounts saved to: $HOTKEYS_FILE"

# ---------------------------
# === SUMMARY ===
# ---------------------------

echo ""
echo "🎉 Account generation complete!"
echo "📁 Files created:"
echo "  - Genesis accounts: $GENESIS_ACCOUNTS_FILE"
echo "  - Validator accounts: $VALIDATORS_FILE"
echo "  - Universal validator hotkeys: $HOTKEYS_FILE"
echo ""
echo "📋 Summary:"
echo "  - 5 genesis funding accounts generated"
echo "  - 4 validator accounts generated"
echo "  - 4 universal validator hotkeys generated"
echo "  - All accounts use known mnemonics stored in JSON files"
echo ""
echo "Next steps:"
echo "  1. Use these accounts in genesis setup"
echo "  2. Fund genesis accounts in genesis state"
echo "  3. Use genesis accounts to fund validators at runtime"
echo "  4. Grant AuthZ permissions to hotkeys from core validators"