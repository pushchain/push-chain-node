#!/bin/bash
# Simplified Validator Registration - Inspired by testnet/v1/setup/promote_to_validator.sh
set -eu

# Colors for output - Standardized palette matching main script
GREEN='\033[0;32m'      # Success messages
RED='\033[0;31m'        # Error messages  
YELLOW='\033[0;33m'     # Warning messages
CYAN='\033[0;36m'       # Status/info messages
BLUE='\033[1;94m'       # Headers/titles (bright blue)
MAGENTA='\033[0;35m'    # Accent/highlight data
WHITE='\033[1;37m'      # Important values (bold white)
NC='\033[0m'            # No color/reset
BOLD='\033[1m'          # Emphasis
DEBUG_MODE="${PUSH_DEBUG:-0}"

# Print functions - Unified across all scripts
print_status() { echo -e "${CYAN}$1${NC}"; }
print_header() { echo -e "${BLUE}$1${NC}"; }
print_value() { echo -e "${MAGENTA}$1${NC}"; }
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
    echo "🔧 Run './scripts/setup-dependencies.sh' to build the binary"
    exit 1
fi

HOME_DIR="$HOME/.pchain"
BINARY="$NATIVE_BINARY"

# Check sync status FIRST before asking for any input
print_status "🔍 Checking node sync status..."
SYNC_STATUS=$("$BINARY" status --node tcp://localhost:26657 2>/dev/null | jq -r '.sync_info.catching_up // "true"' 2>/dev/null || echo "true")

# Get block heights for more accurate sync assessment
LOCAL_HEIGHT=$("$BINARY" status --node tcp://localhost:26657 2>/dev/null | jq -r '.sync_info.latest_block_height // "0"' 2>/dev/null || echo "0")
GENESIS_RPC="https://${GENESIS_DOMAIN:-rpc-testnet-donut-node1.push.org}:443"
REMOTE_HEIGHT=$("$BINARY" status --node "$GENESIS_RPC" 2>/dev/null | jq -r '.sync_info.latest_block_height // "0"' 2>/dev/null || echo "0")

# Calculate blocks behind
BLOCKS_BEHIND=0
if [ "$REMOTE_HEIGHT" -gt "$LOCAL_HEIGHT" ]; then
    BLOCKS_BEHIND=$((REMOTE_HEIGHT - LOCAL_HEIGHT))
fi

# Define sync threshold (120 blocks = ~2 minutes at 1 block/sec)
SYNC_THRESHOLD=120

# Check if node is sufficiently synced
if [ "$SYNC_STATUS" = "false" ] || [ "$BLOCKS_BEHIND" -le "$SYNC_THRESHOLD" ]; then
    if [ "$BLOCKS_BEHIND" -eq 0 ]; then
        print_success "✅ Node is fully synced - safe to proceed with validator registration"
    else
        print_success "✅ Node is sufficiently synced ($BLOCKS_BEHIND blocks behind) - safe to proceed with validator registration"
    fi
else
    print_error "❌ Node too far behind to register validator"
    echo
    print_status "Node is ${MAGENTA}$BLOCKS_BEHIND${NC} blocks behind (limit: $SYNC_THRESHOLD)"
    print_status "Run ${CYAN}push-node-manager sync${NC} to monitor progress"
    echo
    exit 1
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

# Detect if this node's consensus pubkey is already registered as a validator
GENESIS_TCP_RPC="tcp://${GENESIS_DOMAIN:-rpc-testnet-donut-node1.push.org}:26657"
PUBKEY_BASE64=$(echo "$PUBKEY_JSON" | jq -r '.key // empty' 2>/dev/null || echo "")
if [ -n "$PUBKEY_BASE64" ]; then
    EXISTING_VAL=$("$BINARY" query staking validators --node "$GENESIS_TCP_RPC" -o json 2>/dev/null | \
        jq -r --arg k "$PUBKEY_BASE64" '.validators[] | select(.consensus_pubkey.key == $k) | .description.moniker' | head -n1)
    if [ -n "$EXISTING_VAL" ]; then
        print_error "❌ This node's consensus key is already registered as validator: ${MAGENTA}$EXISTING_VAL${NC}"
        echo
        print_status "Why this happens:"
        print_status "  • A single node (consensus key) can only back one validator"
        print_status "  • You attempted to create a second validator using the same node key"
        echo
        print_status "Options:"
        print_status "  1) Manage the existing validator instead (edit description/commission)"
        print_status "  2) Run a second node with a different consensus key and register that"
        print_status "  3) Rotate this node's consensus key by re-initializing keys (advanced; may affect uptime)"
        echo
        print_status "Tip: Check current validators: ./push-node-manager validators"
        exit 1
    fi
fi

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

# Always use remote RPC for balance queries for accuracy
BALANCE=$("$BINARY" query bank balances "$VALIDATOR_ADDR" --node "$GENESIS_TCP_RPC" -o json 2>/dev/null | \
    jq -r '.balances[] | select(.denom=="upc") | .amount // "0"' 2>/dev/null || echo "0")

# Convert to PC for display
if [ "$BALANCE" != "0" ] && [ -n "$BALANCE" ]; then
    PC_AMOUNT=$(awk -v bal="$BALANCE" 'BEGIN {printf "%.6f", bal/1000000000000000000}')
    print_success "💰 Current balance: ${MAGENTA}$PC_AMOUNT PC${NC}"
else
    PC_AMOUNT="0.000000"
    print_warning "💰 Current balance: ${MAGENTA}0 PC${NC}"
fi

# Check if balance is sufficient (need balance for 2 PC stake + gas fees)
REQUIRED_FOR_STAKE_AND_GAS="2100000000000000000"  # 2.1 PC (2 PC stake + 0.1 PC for gas buffer)

if [ "$BALANCE" -ge "$REQUIRED_FOR_STAKE_AND_GAS" ] 2>/dev/null; then
    print_success "✅ Sufficient balance! Proceeding with validator registration..."
    echo
else
    # Current balance is less than ideal, but might still work
    STAKE_AMOUNT_NUM="2000000000000000000"
    if [ "$BALANCE" -ge "$STAKE_AMOUNT_NUM" ] 2>/dev/null; then
        print_warning "⚠️  Balance is tight but should work for validator registration"
        print_success "✅ You have $(awk -v bal="$BALANCE" 'BEGIN {printf "%.6f", bal/1000000000000000000}') PC - attempting registration..."
        echo
    else
        # Show funding information
        print_warning "💸 Fund the account: $VALIDATOR_ADDR"
        print_warning "💰 EVM address for faucet: $EVM_ADDR"
        print_warning "🌐 Faucet URL: https://faucet.push.org"
        echo
        print_warning "⚠️  You need 2.1 PC (minimum stake + gas fees)"
        print_status "   • Current balance: ${MAGENTA}$PC_AMOUNT PC${NC}"
        print_status "   • Required: ${MAGENTA}2.1 PC${NC} (2 PC stake + 0.1 PC gas buffer)"
        echo
        print_status "💡 Steps to fund your account:"
        print_status "   1. Go to: https://faucet.push.org"
        print_status "   2. Enter EVM address: $EVM_ADDR"
        print_status "   3. Request test tokens (may take 1-2 minutes)"
        echo
        read -p "⏳ Press ENTER once you have funded the account..."
        
        # Re-check balance after funding
        print_status "💰 Rechecking account balance..."
        BALANCE=$("$BINARY" query bank balances "$VALIDATOR_ADDR" --node "$GENESIS_TCP_RPC" -o json 2>/dev/null | \
            jq -r '.balances[] | select(.denom=="upc") | .amount // "0"' 2>/dev/null || echo "0")

        if [ "$BALANCE" = "0" ] || [ -z "$BALANCE" ]; then
            print_error "❌ Account still has no balance"
            print_warning "Please fund the account first and try again"
            print_status "EVM address for faucet: $EVM_ADDR"
            print_status "Faucet URL: https://faucet.push.org"
            exit 1
        fi

        # Convert to PC for display
        PC_AMOUNT=$(awk -v bal="$BALANCE" 'BEGIN {printf "%.6f", bal/1000000000000000000}')
        print_success "✅ Updated balance: ${MAGENTA}$PC_AMOUNT PC${NC}"

        # Final balance check
        if [ "$BALANCE" -lt "$REQUIRED_FOR_STAKE_AND_GAS" ] 2>/dev/null; then
            print_warning "⚠️  Balance may still be insufficient for validator creation + gas"
            print_status "Proceeding anyway... If transaction fails, get more tokens from faucet"
        fi
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

# Validator JSON created successfully

print_status "🚀 Submitting create-validator transaction..."

# Use RPC endpoint for transactions
print_status "📡 Using RPC endpoint for transaction..."
NODE_ENDPOINT="tcp://${GENESIS_DOMAIN:-rpc-testnet-donut-node1.push.org}:26657"

# Create temporary output file
TX_OUTPUT_FILE="/tmp/validator_tx_output.$$"

# Run transaction with timeout
print_status "🔧 Starting transaction execution..."
print_status "⏳ This may take up to 60 seconds - please wait..."

# Add signal handling for graceful cleanup
cleanup() {
    print_warning "\n⚠️  Transaction interrupted by user"
    rm -f "$TX_OUTPUT_FILE"
    rm -f "$VALIDATOR_JSON"
    exit 130
}
trap cleanup INT TERM

# Start transaction in background with progress indicator (portable: no GNU timeout required)
"$BINARY" tx staking create-validator "$VALIDATOR_JSON" \
  --from "$KEY_NAME" \
  --chain-id "$CHAIN_ID" \
  --keyring-backend "$KEYRING" \
  --home "$HOME_DIR" \
  --node "$NODE_ENDPOINT" \
  --gas=auto --gas-adjustment=1.3 --gas-prices="1000000000${DENOM}" \
  --yes > "$TX_OUTPUT_FILE" 2>&1 &

TX_PID=$!

# Show progress while transaction is running
SECONDS=0
while kill -0 $TX_PID 2>/dev/null; do
    if [ $SECONDS -ge 60 ]; then
        print_warning "⏳ Still processing... (${SECONDS}s elapsed)"
        kill $TX_PID 2>/dev/null
        TX_EXIT_CODE=124  # Timeout
        break
    elif [ $((SECONDS % 10)) -eq 0 ] && [ $SECONDS -gt 0 ]; then
        print_status "⏳ Transaction in progress... (${SECONDS}s elapsed)"
    fi
    sleep 1
done

# Wait for process to complete and get exit code with 60s watchdog
TX_EXIT_CODE=0
WATCHDOG=0
while kill -0 $TX_PID 2>/dev/null; do
    sleep 1
    WATCHDOG=$((WATCHDOG+1))
    if [ $WATCHDOG -ge 60 ]; then
        print_error "❌ Transaction timed out after 60 seconds!"
        kill $TX_PID 2>/dev/null || true
        TX_EXIT_CODE=124
        break
    fi
done

if [ $TX_EXIT_CODE -ne 124 ]; then
    set +e
    wait $TX_PID 2>/dev/null
    TX_EXIT_CODE=$?
    set -e
fi

print_status "🔍 Transaction finished with exit code: $TX_EXIT_CODE"

# Read transaction output (even if still being written)
sync || true
TX_OUTPUT=$(cat "$TX_OUTPUT_FILE" 2>/dev/null || echo "No output captured")

# Reset signal handler
trap - INT TERM

# Check if command timed out
if [ $TX_EXIT_CODE -eq 124 ]; then
    print_error "❌ Transaction timed out after 60 seconds!"
    echo
    print_warning "⚠️  Possible causes:"
    print_status "   • Network connectivity issues"
    print_status "   • RPC endpoint overloaded"
    print_status "   • Transaction stuck in mempool"
    echo
    print_status "💡 Try again in a few minutes or check:"
    print_status "   • Node status: ./push-node-manager status"
    print_status "   • Network connectivity to $GENESIS_DOMAIN"
    rm -f "$TX_OUTPUT_FILE"
    rm -f "$VALIDATOR_JSON"
    exit 1
fi

# Clean up validator JSON file  
rm -f "$VALIDATOR_JSON"

# Optionally show captured output for debugging
if [ "${DEBUG_MODE}" = "1" ]; then
  echo
  print_header "📦 Raw transaction output"
  echo "$TX_OUTPUT"
  echo
fi

# Parse transaction result
if [ $TX_EXIT_CODE -eq 0 ]; then
    # Check if transaction was successful (look for txhash)
    if echo "$TX_OUTPUT" | grep -q "txhash"; then
        print_success "✅ Node successfully promoted to validator!"
        print_success "🎉 Validator '$MONIKER' registered with key '$KEY_NAME'"
        echo
        print_status "🔍 Transaction details:"
        TXHASH=$(echo "$TX_OUTPUT" | awk -F'txhash: ' '/txhash/{print $2; exit}')
        print_status "   • Transaction hash: ${MAGENTA}$TXHASH${NC}"
        print_status "   • Moniker: ${MAGENTA}$MONIKER${NC}"
        print_status "   • Stake: ${MAGENTA}2 PC${NC}"
        print_status "   • Commission: ${MAGENTA}10%${NC}"
        echo
        # Also show the raw tx response block for transparency
        echo "$TX_OUTPUT"
    else
        print_error "❌ Transaction submitted but status unclear"
        [ "${DEBUG_MODE}" = "1" ] || print_status "(Set PUSH_DEBUG=1 to see full output)"
    fi
else
    # Handle specific error cases
    if echo "$TX_OUTPUT" | grep -q "validator already exist"; then
        print_error "❌ Validator registration failed!"
        echo
        print_warning "⚠️  This validator key is already registered on the network."
        echo
        print_status "💡 Possible solutions:"
        print_status "   1. Use a different key name when prompted"
        print_status "   2. Check if you're already a validator: ./push-node-manager validators"
        print_status "   3. Create a new validator key with a different name"
        echo
        print_status "🔍 Your current validator info:"
        VALIDATOR_ADDR=$("$BINARY" keys show "$KEY_NAME" -a --keyring-backend "$KEYRING" --home "$HOME_DIR" 2>/dev/null)
        OPERATOR_ADDR=$("$BINARY" keys show "$KEY_NAME" --bech=val -a --keyring-backend "$KEYRING" --home "$HOME_DIR" 2>/dev/null)
        print_status "   • Account: ${MAGENTA}$VALIDATOR_ADDR${NC}"
        print_status "   • Validator: ${MAGENTA}$OPERATOR_ADDR${NC}"
    elif echo "$TX_OUTPUT" | grep -q "insufficient funds"; then
        print_error "❌ Insufficient funds for validator registration!"
        echo
        print_warning "⚠️  Not enough tokens to cover stake + gas fees."
        print_status "💡 Get more tokens:"
        print_status "   • Faucet: https://faucet.push.org"
        EVM_ADDR=$("$BINARY" debug addr "$VALIDATOR_ADDR" --home "$HOME_DIR" 2>/dev/null | grep "hex" | awk '{print "0x"$3}')
        print_status "   • EVM Address: ${MAGENTA}$EVM_ADDR${NC}"
    elif echo "$TX_OUTPUT" | grep -q "account sequence mismatch"; then
        print_error "❌ Transaction sequence error!"
        echo
        print_warning "⚠️  Account sequence mismatch - another transaction may be pending."
        print_status "💡 Try again in a few seconds after the previous transaction is processed."
    else
        # Clean error: try to extract the most relevant line(s)
        CLEAN_ERR=$(echo "$TX_OUTPUT" | grep -E "rpc error:|failed to execute message|unknown request|insufficient|unauthorized|invalid" | tail -n1)
        if [ -n "$CLEAN_ERR" ]; then
            print_error "❌ Validator registration failed:"
            print_status "   • ${MAGENTA}${CLEAN_ERR}${NC}"
        else
            print_error "❌ Validator registration failed with unexpected error"
        fi
        [ "${DEBUG_MODE}" = "1" ] || print_status "(Set PUSH_DEBUG=1 to see full output)"
    fi
    rm -f "$TX_OUTPUT_FILE"
    exit 1
fi

# Clean up temporary files on success
rm -f "$TX_OUTPUT_FILE"