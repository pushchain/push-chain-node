#!/bin/bash
# Simplified Validator Registration - Inspired by testnet/v1/setup/promote_to_validator.sh
set -eu

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m'
BOLD='\033[1m'

print_status() { echo -e "${BLUE}$1${NC}"; }
print_success() { echo -e "${GREEN}$1${NC}"; }
print_error() { echo -e "${RED}$1${NC}"; }
print_warning() { echo -e "${YELLOW}$1${NC}"; }

# === CHAIN CONFIG ===
CHAIN_ID="push_42101-1"
DENOM="upc"
KEYRING="test"
KEYALGO="eth_secp256k1"
STAKE_AMOUNT="2000000000000000000"  # 2 * 10^18

# === Resolve Paths ===
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NATIVE_BINARY="$SCRIPT_DIR/../build/pchaind"

# Native-only mode
if [ ! -f "$NATIVE_BINARY" ]; then
    print_error "❌ Native binary not found at: $NATIVE_BINARY"
    echo "🔧 Run './setup-native-dependencies.sh' to build the binary"
    exit 1
fi

HOME_DIR="$HOME/.pchain"
BINARY="$NATIVE_BINARY"
print_status "📁 Using native binary: $BINARY"

print_status "📁 Using HOME_DIR: $HOME_DIR"

# Get user input for validator details
echo
echo -e "${BOLD}=== Push Chain Validator Registration ===${NC}"
echo

# Get moniker
read -p "Enter validator name (moniker): " MONIKER
MONIKER=${MONIKER:-push-validator}

# Get key name
read -p "Enter key name for validator (default: validator-key): " KEY_NAME
KEY_NAME=${KEY_NAME:-validator-key}

print_status "🔍 Fetching pubkey from tendermint..."
PUBKEY_JSON=$("$BINARY" tendermint show-validator --home "$HOME_DIR")

print_status "🔐 Creating validator key..."
echo "You will be prompted to enter your mnemonic phrase or create a new key"

"$BINARY" keys add "$KEY_NAME" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOME_DIR" || true
VALIDATOR_ADDR=$("$BINARY" keys show "$KEY_NAME" -a --keyring-backend "$KEYRING" --home "$HOME_DIR")

# Get EVM address for faucet
EVM_ADDR=$("$BINARY" debug addr "$VALIDATOR_ADDR" --home "$HOME_DIR" | grep "hex" | awk '{print "0x"$3}')

print_warning "💸 Fund the account: $VALIDATOR_ADDR"
print_warning "💰 EVM address for faucet: $EVM_ADDR"
print_warning "🌐 Faucet URL: https://faucet.push.org"
echo
print_warning "⚠️  You need more than ${STAKE_AMOUNT}${DENOM} for stake + Tx Gas fees"
read -p "⏳ Press ENTER once the account is funded..."

# === CREATE VALIDATOR JSON ===
VALIDATOR_JSON="$HOME/.pchain_validator.json"

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

print_status "🚀 Submitting create-validator transaction..."

"$BINARY" tx staking create-validator "$VALIDATOR_JSON" \
  --from "$KEY_NAME" \
  --chain-id "$CHAIN_ID" \
  --keyring-backend "$KEYRING" \
  --home "$HOME_DIR" \
  --gas=auto --gas-adjustment=1.3 --gas-prices="1000000000${DENOM}" \
  --yes

print_status "🧹 Cleaning up temporary file..."
rm -f "$VALIDATOR_JSON"

print_success "✅ Node successfully promoted to validator!"
print_success "🎉 Validator '$MONIKER' registered with key '$KEY_NAME'"