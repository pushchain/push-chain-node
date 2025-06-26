#!/bin/bash
# Push Chain Validator Registration Script

set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m'

# Configuration
NETWORKS_CONFIG="/configs/networks.json"
KEYRING="${KEYRING:-test}"
NETWORK="${NETWORK:-testnet}"

# Load network config
load_network_config() {
    if [ -f "$NETWORKS_CONFIG" ]; then
        CHAIN_ID=$(jq -r ".networks.$NETWORK.chain_id // empty" "$NETWORKS_CONFIG")
        DENOM=$(jq -r ".networks.$NETWORK.denom // 'upc'" "$NETWORKS_CONFIG")
        EXPLORER=$(jq -r ".networks.$NETWORK.explorer // empty" "$NETWORKS_CONFIG")
        FAUCET=$(jq -r ".networks.$NETWORK.faucet // empty" "$NETWORKS_CONFIG")
        
        # Get RPC endpoint
        local rpc=$(jq -r ".networks.$NETWORK.rpc_endpoints[0] // empty" "$NETWORKS_CONFIG" 2>/dev/null)
        if [ -n "$rpc" ]; then
            GENESIS_NODE_RPC="$rpc"
        fi
        
        # Calculate denomination (18 zeros for 1 PUSH)
        ONE_PUSH="000000000000000000${DENOM}"
    else
        # Fallback values
        CHAIN_ID="push_42101-1"
        DENOM="upc"
        ONE_PUSH="000000000000000000upc"
        GENESIS_NODE_RPC="tcp://localhost:26657"
    fi
}

# Load config
load_network_config

# Helper functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

# Check if running inside container
if [ ! -f /.dockerenv ]; then
    log_error "This script should be run inside the validator container"
    echo "Use: ./push-validator shell"
    echo "Then run: /scripts/register-validator.sh"
    exit 1
fi

# Main registration process
main() {
    log_info "Push Chain Validator Registration"
    echo "================================="
    
    # Get validator info
    VALIDATOR_NAME="${MONIKER:-MyValidator}"
    log_info "Validator Name: $VALIDATOR_NAME"
    
    # Show node ID
    NODE_ID=$(pchaind tendermint show-node-id)
    log_info "Node ID: $NODE_ID"
    
    # Get validator pubkey
    VALIDATOR_PUBKEY=$(pchaind comet show-validator)
    log_info "Validator Pubkey: $VALIDATOR_PUBKEY"
    
    # Check if we have a wallet
    log_info "Checking for existing wallet..."
    WALLET_NAME="${NODE_OWNER_WALLET_NAME:-validator}"
    
    if ! pchaind keys show $WALLET_NAME --keyring-backend $KEYRING >/dev/null 2>&1; then
        log_warning "No wallet found. Creating new wallet..."
        log_warning "⚠️  IMPORTANT: Save the mnemonic phrase shown below!"
        echo ""
        pchaind keys add $WALLET_NAME --keyring-backend $KEYRING
        echo ""
    fi
    
    # Get wallet address
    WALLET_ADDRESS=$(pchaind keys show $WALLET_NAME -a --keyring-backend $KEYRING)
    log_info "Wallet Address: $WALLET_ADDRESS"
    
    # Check balance
    log_info "Checking wallet balance..."
    BALANCE=$(pchaind query bank balances $WALLET_ADDRESS --node $GENESIS_NODE_RPC -o json | jq -r '.balances[] | select(.denom=="upc") | .amount // "0"')
    
    if [ "$BALANCE" = "0" ] || [ -z "$BALANCE" ]; then
        log_error "Wallet has no balance!"
        echo ""
        echo "Please fund your wallet address: $WALLET_ADDRESS"
        if [ -n "$FAUCET" ]; then
            echo "Faucet: $FAUCET"
        fi
        echo ""
        echo "After funding, run this script again."
        exit 1
    fi
    
    log_info "Wallet Balance: $BALANCE upc"
    
    # Get staking amount
    echo ""
    read -p "Enter amount to stake (in PUSH, minimum 1): " STAKE_AMOUNT
    
    # Validate amount
    if ! [[ "$STAKE_AMOUNT" =~ ^[0-9]+$ ]] || [ "$STAKE_AMOUNT" -lt 1 ]; then
        log_error "Invalid stake amount. Must be at least 1 PUSH"
        exit 1
    fi
    
    # Create validator JSON file
    cat <<EOF > /tmp/register-validator.json
{
    "pubkey": $VALIDATOR_PUBKEY,
    "amount": "${STAKE_AMOUNT}${ONE_PUSH}",
    "moniker": "$VALIDATOR_NAME",
    "website": "${VALIDATOR_WEBSITE:-}",
    "security": "${VALIDATOR_SECURITY:-}",
    "details": "${VALIDATOR_DETAILS:-A Push Chain validator}",
    "commission-rate": "${COMMISSION_RATE:-0.1}",
    "commission-max-rate": "${COMMISSION_MAX_RATE:-0.2}",
    "commission-max-change-rate": "${COMMISSION_MAX_CHANGE_RATE:-0.01}",
    "min-self-delegation": "${MIN_SELF_DELEGATION:-1}"
}
EOF
    
    log_info "Validator configuration:"
    cat /tmp/register-validator.json | jq .
    
    echo ""
    read -p "Proceed with validator registration? (y/N): " -n 1 -r
    echo ""
    
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        log_info "Registration cancelled"
        exit 0
    fi
    
    # Create validator
    log_info "Creating validator..."
    
    TX_RESULT=$(pchaind tx staking create-validator /tmp/register-validator.json \
        --chain-id $CHAIN_ID \
        --fees "1${ONE_PUSH}" \
        --gas "1000000" \
        --from $WALLET_NAME \
        --node=$GENESIS_NODE_RPC \
        --keyring-backend $KEYRING \
        --yes \
        --output json)
    
    # Extract transaction hash
    TX_HASH=$(echo "$TX_RESULT" | jq -r '.txhash // empty')
    
    if [ -z "$TX_HASH" ]; then
        log_error "Failed to create validator transaction"
        echo "$TX_RESULT" | jq .
        exit 1
    fi
    
    log_success "Transaction submitted! Hash: $TX_HASH"
    
    # Wait for transaction
    log_info "Waiting for transaction to be included in block..."
    sleep 10
    
    # Check transaction result
    TX_QUERY=$(pchaind query tx $TX_HASH --chain-id $CHAIN_ID --node=$GENESIS_NODE_RPC --output json 2>/dev/null || echo "{}")
    TX_CODE=$(echo "$TX_QUERY" | jq -r '.code // "-1"')
    
    if [ "$TX_CODE" = "0" ]; then
        log_success "Validator created successfully!"
        
        # Query validator
        log_info "Querying validator status..."
        sleep 5
        
        pchaind query staking validators --node=$GENESIS_NODE_RPC --output json | \
            jq ".validators[] | select(.description.moniker==\"$VALIDATOR_NAME\")" || \
            log_warning "Validator may take a moment to appear in queries"
            
        echo ""
        log_success "🎉 Congratulations! Your validator is now active!"
        echo ""
        echo "Monitor your validator:"
        if [ -n "$EXPLORER" ]; then
            echo "- Explorer: $EXPLORER"
        fi
        echo "- Status: ./push-validator status"
        echo ""
    else
        log_error "Transaction failed!"
        echo "$TX_QUERY" | jq '{code, raw_log}'
        exit 1
    fi
}

# Run main function
main "$@"