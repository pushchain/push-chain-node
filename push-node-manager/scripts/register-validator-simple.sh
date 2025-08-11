#!/bin/bash
# Simplified Validator Registration - Inspired by testnet/v1/setup/promote_to_validator.sh
set -eu

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
WHITE='\033[1;37m'
NC='\033[0m'
BOLD='\033[1m'

print_status() { echo -e "${CYAN}$1${NC}"; }
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

# Check sync status FIRST before asking for any input
print_status "🔍 Checking node sync status before registration..."
SYNC_STATUS=$("$BINARY" status --node tcp://localhost:26657 2>/dev/null | jq -r '.sync_info.catching_up // "true"' 2>/dev/null || echo "true")

if [ "$SYNC_STATUS" = "true" ]; then
    print_error "❌ CRITICAL: Node must be fully synced before validator registration!"
    echo
    print_warning "⚠️  Why this matters:"
    print_status "   • Validators that aren't synced will miss blocks"
    print_status "   • Missing blocks leads to validator jailing and slashing"
    print_status "   • Jailed validators lose stake and need to be unjailed"
    echo
    print_status "📋 Required steps:"
    print_status "   1. Wait for full sync: ./push-node-manager sync"
    print_status "   2. Verify sync status: ./push-node-manager status"
    print_status "   3. Look for 'Catching Up: false' before proceeding"
    echo
    print_status "💡 Current sync status:"
    LOCAL_HEIGHT=$("$BINARY" status --node tcp://localhost:26657 2>/dev/null | jq -r '.sync_info.latest_block_height // "0"' 2>/dev/null || echo "0")
    REMOTE_HEIGHT=$("$BINARY" status --node https://rpc-testnet-donut-node1.push.org:443 2>/dev/null | jq -r '.sync_info.latest_block_height // "0"' 2>/dev/null || echo "0")
    print_status "   • Local height: $LOCAL_HEIGHT"
    print_status "   • Network height: $REMOTE_HEIGHT"
    if [ "$REMOTE_HEIGHT" -gt "$LOCAL_HEIGHT" ]; then
        BLOCKS_BEHIND=$((REMOTE_HEIGHT - LOCAL_HEIGHT))
        print_status "   • Blocks behind: $BLOCKS_BEHIND"
    fi
    echo
    print_error "❌ Validator registration cancelled - sync required first!"
    exit 1
else
    print_success "✅ Node is fully synced - safe to proceed with validator registration"
fi

# Get user input for validator details AFTER sync check passes
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

print_status "🔐 Setting up validator key..."

# Check if key already exists
if "$BINARY" keys show "$KEY_NAME" --keyring-backend "$KEYRING" --home "$HOME_DIR" >/dev/null 2>&1; then
    print_success "✅ Key '$KEY_NAME' already exists"
    VALIDATOR_ADDR=$("$BINARY" keys show "$KEY_NAME" -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
else
    print_status "Creating new validator key..."
    echo "You will be prompted to enter your mnemonic phrase or create a new key"
    
    if ! "$BINARY" keys add "$KEY_NAME" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOME_DIR"; then
        print_error "❌ Key creation failed or was cancelled"
        exit 1
    fi
    VALIDATOR_ADDR=$("$BINARY" keys show "$KEY_NAME" -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
fi

# Get EVM address for faucet
EVM_ADDR=$("$BINARY" debug addr "$VALIDATOR_ADDR" --home "$HOME_DIR" | grep "hex" | awk '{print "0x"$3}')

# Check current balance first
print_status "💰 Checking current account balance..."

# Check if node is synced first
SYNC_STATUS=$("$BINARY" status --node tcp://localhost:26657 2>/dev/null | jq -r '.sync_info.catching_up // "true"' 2>/dev/null || echo "true")

if [ "$SYNC_STATUS" = "true" ]; then
    print_warning "⚠️  Node is still syncing - balance queries may be inaccurate"
    print_status "💡 Recommendation: Wait for full sync or use remote RPC for accurate balance"
    
    # Try querying from remote node for accurate balance
    print_status "🔍 Checking balance via remote RPC..."
    GENESIS_RPC="https://rpc-testnet-donut-node1.push.org:443"
    BALANCE=$("$BINARY" query bank balances "$VALIDATOR_ADDR" --node "$GENESIS_RPC" -o json 2>/dev/null | \
        jq -r '.balances[] | select(.denom=="upc") | .amount // "0"' 2>/dev/null || echo "0")
    
    if [ "$BALANCE" = "0" ]; then
        # Fallback to local node query
        BALANCE=$("$BINARY" query bank balances "$VALIDATOR_ADDR" --node tcp://localhost:26657 -o json 2>/dev/null | \
            jq -r '.balances[] | select(.denom=="upc") | .amount // "0"' 2>/dev/null || echo "0")
    fi
else
    # Node is synced, use local query
    BALANCE=$("$BINARY" query bank balances "$VALIDATOR_ADDR" --node tcp://localhost:26657 -o json 2>/dev/null | \
        jq -r '.balances[] | select(.denom=="upc") | .amount // "0"' 2>/dev/null || echo "0")
fi

# Convert to PC for display
if [ "$BALANCE" != "0" ] && [ -n "$BALANCE" ]; then
    PC_AMOUNT=$(awk -v bal="$BALANCE" 'BEGIN {printf "%.6f", bal/1000000000000000000}')
    print_success "💰 Current balance: $PC_AMOUNT PC"
else
    PC_AMOUNT="0.000000"
    print_warning "💰 Current balance: 0 PC"
fi

# Check if balance is sufficient
MIN_REQUIRED="2000000000000000000"  # 2 PC is sufficient (stake + gas)
if [ "$BALANCE" -ge "$MIN_REQUIRED" ] 2>/dev/null; then
    print_success "✅ Sufficient balance! Proceeding with validator registration..."
    echo
else
    # Show funding information
    print_warning "💸 Fund the account: $VALIDATOR_ADDR"
    print_warning "💰 EVM address for faucet: $EVM_ADDR"
    print_warning "🌐 Faucet URL: https://faucet.push.org"
    echo
    print_warning "⚠️  You need 2 PC (minimum stake + gas fees)"
    print_status "   • Current balance: $PC_AMOUNT PC"
    print_status "   • Required: 2 PC (stake + gas)"
    echo
    print_status "💡 Steps to fund your account:"
    print_status "   1. Go to: https://faucet.push.org"
    print_status "   2. Enter EVM address: $EVM_ADDR"
    print_status "   3. Request test tokens (may take 1-2 minutes)"
    echo
    read -p "⏳ Press ENTER once you have funded the account..."
    
    # Re-check balance after funding
    print_status "💰 Rechecking account balance..."
    if [ "$SYNC_STATUS" = "true" ]; then
        # Use remote RPC if still syncing
        BALANCE=$("$BINARY" query bank balances "$VALIDATOR_ADDR" --node "$GENESIS_RPC" -o json 2>/dev/null | \
            jq -r '.balances[] | select(.denom=="upc") | .amount // "0"' 2>/dev/null || echo "0")
    else
        # Use local node if synced
        BALANCE=$("$BINARY" query bank balances "$VALIDATOR_ADDR" --node tcp://localhost:26657 -o json 2>/dev/null | \
            jq -r '.balances[] | select(.denom=="upc") | .amount // "0"' 2>/dev/null || echo "0")
    fi

    if [ "$BALANCE" = "0" ] || [ -z "$BALANCE" ]; then
        print_error "❌ Account still has no balance"
        print_warning "Please fund the account first and try again"
        print_status "EVM address for faucet: $EVM_ADDR"
        print_status "Faucet URL: https://faucet.push.org"
        exit 1
    fi

    # Convert to PC for display
    PC_AMOUNT=$(awk -v bal="$BALANCE" 'BEGIN {printf "%.6f", bal/1000000000000000000}')
    print_success "✅ Updated balance: $PC_AMOUNT PC"

    # Final balance check
    if [ "$BALANCE" -lt "$MIN_REQUIRED" ] 2>/dev/null; then
        print_warning "⚠️  Balance may still be insufficient for validator creation + gas"
        print_status "Proceeding anyway... If transaction fails, get more tokens from faucet"
    fi
fi

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

# Use remote RPC if local node is still syncing
if [ "$SYNC_STATUS" = "true" ]; then
    print_status "📡 Using remote RPC for transaction (local node still syncing)..."
    NODE_ENDPOINT="$GENESIS_RPC"
else
    print_status "🏠 Using local node for transaction..."
    NODE_ENDPOINT="tcp://localhost:26657"
fi

"$BINARY" tx staking create-validator "$VALIDATOR_JSON" \
  --from "$KEY_NAME" \
  --chain-id "$CHAIN_ID" \
  --keyring-backend "$KEYRING" \
  --home "$HOME_DIR" \
  --node "$NODE_ENDPOINT" \
  --gas=auto --gas-adjustment=1.3 --gas-prices="1000000000${DENOM}" \
  --yes

print_status "🧹 Cleaning up temporary file..."
rm -f "$VALIDATOR_JSON"

print_success "✅ Node successfully promoted to validator!"
print_success "🎉 Validator '$MONIKER' registered with key '$KEY_NAME'"