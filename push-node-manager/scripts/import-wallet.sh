#!/bin/bash
# Import wallet from mnemonic for automated deployments
# Usage: MNEMONIC="your mnemonic phrase" ./import-wallet.sh

set -e

# Source common functions
source /scripts/common.sh

# Check if running inside container
check_container "/scripts/import-wallet.sh"

# Check if mnemonic is provided
if [ -z "$MNEMONIC" ]; then
    log_error "No mnemonic provided!"
    echo "Usage: MNEMONIC=\"your mnemonic phrase\" ./import-wallet.sh"
    exit 1
fi

# Check if wallet already exists
if pchaind keys show validator --keyring-backend test --home /root/.pchain >/dev/null 2>&1; then
    log_warning "Wallet 'validator' already exists!"
    exit 0
fi

# Import wallet
log_info "Importing wallet..."
echo "$MNEMONIC" | pchaind keys add validator --recover --keyring-backend test --home /root/.pchain >/dev/null 2>&1

# Get address
ADDRESS=$(pchaind keys show validator -a --keyring-backend test --home /root/.pchain)

log_success "Wallet imported successfully!"
echo "Address: $ADDRESS"

# Convert to EVM format
EVM_ADDRESS=$(pchaind debug addr "$ADDRESS" 2>/dev/null | grep "hex" | awk '{print "0x"$2}')
if [ -n "$EVM_ADDRESS" ]; then
    echo "EVM Address: $EVM_ADDRESS (for faucet)"
fi