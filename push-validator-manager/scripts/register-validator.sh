#!/bin/bash
# Simplified Validator Registration - Inspired by testnet/v1/setup/promote_to_validator.sh
set -eu

# Function to run commands with Go warnings suppressed
run_silent() {
    local temp_err=$(mktemp)
    "$@" 2>"$temp_err"
    local exit_code=$?
    grep -v "WARNING:(ast)" "$temp_err" >&2 || true
    rm -f "$temp_err"
    return $exit_code
}

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
PCHAIND="$SCRIPT_DIR/../build/pchaind"

# Native-only mode
if [ ! -f "$PCHAIND" ]; then
    print_error "❌ Binary not found at: $PCHAIND"
    echo "🔧 Run './scripts/setup-dependencies.sh' to build the binary"
    exit 1
fi

HOME_DIR="$HOME/.pchain"
BINARY="$PCHAIND"

# Check sync status FIRST before asking for any input
print_status "Checking node sync status..."
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
        print_success "✅ Node synced"
    else
        print_success "✅ Node synced ($BLOCKS_BEHIND blocks behind)"
    fi
else
    print_error "❌ Node too far behind ($BLOCKS_BEHIND blocks). Run: push-validator-manager sync"
    exit 1
fi

echo -e "${BOLD}=== Validator Registration ===${NC}"

# Get moniker
read -p "Enter validator name (moniker): " MONIKER
MONIKER=${MONIKER:-push-validator}

# Get key name
read -p "Enter key name for validator (default: validator-key): " KEY_NAME
KEY_NAME=${KEY_NAME:-validator-key}

PUBKEY_JSON=$("$BINARY" tendermint show-validator --home "$HOME_DIR" 2>/dev/null)

# Detect if this node's consensus pubkey is already registered as a validator
GENESIS_TCP_RPC="tcp://${GENESIS_DOMAIN:-rpc-testnet-donut-node1.push.org}:26657"
PUBKEY_BASE64=$(echo "$PUBKEY_JSON" | jq -r '.key // empty' 2>/dev/null || echo "")
if [ -n "$PUBKEY_BASE64" ]; then
    EXISTING_VAL=$("$BINARY" query staking validators --node "$GENESIS_TCP_RPC" -o json 2>/dev/null | \
        jq -r --arg k "$PUBKEY_BASE64" '.validators[] | select(.consensus_pubkey.key == $k) | .description.moniker' | head -n1)
    if [ -n "$EXISTING_VAL" ]; then
        print_error "❌ Node already registered as validator: $EXISTING_VAL"
        exit 1
    fi
fi

# Check if key already exists
if run_silent "$BINARY" keys show "$KEY_NAME" --keyring-backend "$KEYRING" --home "$HOME_DIR" >/dev/null; then
    print_success "✅ Key '$KEY_NAME' exists"
    VALIDATOR_ADDR=$(run_silent "$BINARY" keys show "$KEY_NAME" -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
else
    print_status "Creating validator key..."
    if ! run_silent "$BINARY" keys add "$KEY_NAME" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOME_DIR"; then
        print_error "❌ Key creation failed"
        exit 1
    fi
    VALIDATOR_ADDR=$(run_silent "$BINARY" keys show "$KEY_NAME" -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
fi

# Get EVM address for faucet
EVM_ADDR=$(run_silent "$BINARY" debug addr "$VALIDATOR_ADDR" --home "$HOME_DIR" | grep "hex" | awk '{print "0x"$3}')

# Check current balance
BALANCE=$("$BINARY" query bank balances "$VALIDATOR_ADDR" --node "$GENESIS_TCP_RPC" -o json 2>/dev/null | \
    jq -r '.balances[] | select(.denom=="upc") | .amount // "0"' 2>/dev/null || echo "0")

# Convert to PC for display
if [ "$BALANCE" != "0" ] && [ -n "$BALANCE" ]; then
    PC_AMOUNT=$(awk -v bal="$BALANCE" 'BEGIN {printf "%.6f", bal/1000000000000000000}')
    print_success "Balance: $PC_AMOUNT PC"
else
    PC_AMOUNT="0.000000"
    print_warning "Balance: 0 PC"
fi

# Check if balance is sufficient (need balance for 2 PC stake + gas fees)
REQUIRED_FOR_STAKE_AND_GAS="2100000000000000000"  # 2.1 PC (2 PC stake + 0.1 PC for gas buffer)

if [ "$BALANCE" -ge "$REQUIRED_FOR_STAKE_AND_GAS" ] 2>/dev/null; then
    print_success "✅ Sufficient balance"
else
    # Current balance is less than ideal, but might still work
    STAKE_AMOUNT_NUM="2000000000000000000"
    if [ "$BALANCE" -ge "$STAKE_AMOUNT_NUM" ] 2>/dev/null; then
        print_warning "⚠️  Balance tight but proceeding"
    else
        # Show funding information
        print_warning "Need 2.1 PC (current: $PC_AMOUNT PC)"
        print_status "Fund via faucet: https://faucet.push.org"
        print_status "EVM address: $EVM_ADDR"
        read -p "Press ENTER once funded..."
        
        # Re-check balance after funding
        BALANCE=$("$BINARY" query bank balances "$VALIDATOR_ADDR" --node "$GENESIS_TCP_RPC" -o json 2>/dev/null | \
            jq -r '.balances[] | select(.denom=="upc") | .amount // "0"' 2>/dev/null || echo "0")

        if [ "$BALANCE" = "0" ] || [ -z "$BALANCE" ]; then
            print_error "❌ Still no balance. Fund account: $EVM_ADDR"
            exit 1
        fi

        # Convert to PC for display
        PC_AMOUNT=$(awk -v bal="$BALANCE" 'BEGIN {printf "%.6f", bal/1000000000000000000}')
        print_success "Updated balance: $PC_AMOUNT PC"
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

print_status "Submitting transaction..."
NODE_ENDPOINT="tcp://${GENESIS_DOMAIN:-rpc-testnet-donut-node1.push.org}:26657"

# Create temporary output file
TX_OUTPUT_FILE="/tmp/validator_tx_output.$$"

# Add signal handling for graceful cleanup
cleanup() {
    print_warning "Transaction interrupted"
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
        kill $TX_PID 2>/dev/null
        TX_EXIT_CODE=124  # Timeout
        break
    elif [ $((SECONDS % 15)) -eq 0 ] && [ $SECONDS -gt 0 ]; then
        print_status "Processing... (${SECONDS}s)"
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
        print_error "❌ Transaction timed out!"
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

# Read transaction output (even if still being written)
sync || true
TX_OUTPUT=$(cat "$TX_OUTPUT_FILE" 2>/dev/null || echo "No output captured")

# Reset signal handler
trap - INT TERM

# Check if command timed out
if [ $TX_EXIT_CODE -eq 124 ]; then
    print_error "❌ Transaction timed out - try again in a few minutes"
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
        print_success "✅ Validator '$MONIKER' registered successfully!"
        TXHASH=$(echo "$TX_OUTPUT" | awk -F'txhash: ' '/txhash/{print $2; exit}')
        print_status "TxHash: $TXHASH"
        print_status "Stake: 2 PC | Commission: 10%"
        [ "${DEBUG_MODE}" = "1" ] && echo "$TX_OUTPUT"
    else
        print_error "❌ Transaction status unclear"
        [ "${DEBUG_MODE}" = "1" ] || print_status "(Set PUSH_DEBUG=1 for details)"
    fi
else
    # Handle specific error cases
    if echo "$TX_OUTPUT" | grep -q "validator already exist"; then
        print_error "❌ Validator key already registered"
    elif echo "$TX_OUTPUT" | grep -q "insufficient funds"; then
        print_error "❌ Insufficient funds - get tokens from faucet: https://faucet.push.org"
        print_status "EVM address: $EVM_ADDR"
    elif echo "$TX_OUTPUT" | grep -q "account sequence mismatch"; then
        print_error "❌ Sequence error - try again in a few seconds"
    else
        # Clean error: try to extract the most relevant line(s)
        CLEAN_ERR=$(echo "$TX_OUTPUT" | grep -E "rpc error:|failed to execute message|unknown request|insufficient|unauthorized|invalid" | tail -n1)
        if [ -n "$CLEAN_ERR" ]; then
            print_error "❌ Registration failed: $CLEAN_ERR"
        else
            print_error "❌ Registration failed"
        fi
        [ "${DEBUG_MODE}" = "1" ] || print_status "(Set PUSH_DEBUG=1 for details)"
    fi
    rm -f "$TX_OUTPUT_FILE"
    exit 1
fi

# Clean up temporary files on success
rm -f "$TX_OUTPUT_FILE"