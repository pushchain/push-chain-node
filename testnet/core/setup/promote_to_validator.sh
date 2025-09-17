#!/bin/bash
set -eu

# ========================
# Promote to Validator
# ========================

# === CHAIN CONFIG ===
CHAIN_ID="push_42101-1"
MONIKER="validator"
KEY_NAME="validator-key"
KEYRING="os"
KEYALGO="eth_secp256k1"
DENOM="upc"
STAKE_AMOUNT="100000000000000000000000"  # 100k * 10^18

# === Resolve Paths ===
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BINARY="$APP_DIR/binary/pchaind"
HOME_DIR="$APP_DIR/.pchain"

echo "📁 Using HOME_DIR: $HOME_DIR"
echo "🔍 Fetching pubkey from tendermint..."
PUBKEY_JSON=$("$BINARY" tendermint show-validator --home "$HOME_DIR")

echo "🔐 Creating validator key (manual entry)"
"$BINARY" keys add "$KEY_NAME" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOME_DIR" || true
VALIDATOR_ADDR=$("$BINARY" keys show "$KEY_NAME" -a --keyring-backend "$KEYRING" --home "$HOME_DIR")

echo "💸 Fund the account: $VALIDATOR_ADDR with more than ${STAKE_AMOUNT}${DENOM} for stake + Tx Gas fee"
read -p "⏳ Press ENTER once the account is funded..."

# === TEMP FILE ===
VALIDATOR_JSON="$HOME_DIR/validator.json"

cat > "$VALIDATOR_JSON" <<EOF
{
  "pubkey": $PUBKEY_JSON,
  "amount": "${STAKE_AMOUNT}${DENOM}",
  "moniker": "$MONIKER",
  "identity": "",
  "website": "",
  "security": "",
  "details": "Push Chain Validator",
  "commission-rate": "0.10",
  "commission-max-rate": "0.20",
  "commission-max-change-rate": "0.01",
  "min-self-delegation": "1"
}
EOF

echo "🚀 Submitting create-validator transaction..."
"$BINARY" tx staking create-validator "$VALIDATOR_JSON" \
  --from "$KEY_NAME" \
  --chain-id "$CHAIN_ID" \
  --keyring-backend "$KEYRING" \
  --home "$HOME_DIR" \
  --gas=auto --gas-adjustment=1.3 --gas-prices="1000000000${DENOM}" \
  --yes

echo "🧹 Cleaning up temporary file..."
rm -f "$VALIDATOR_JSON"

echo "✅ Node successfully promoted to validator!"
