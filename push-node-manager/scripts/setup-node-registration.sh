#!/bin/bash
# Push Chain Validator Setup Script V2 - Improved Flow
# Enhanced with better fee handling, pre-flight checks, and clearer UX

set -e

# Error handler
error_handler() {
    local line_no=$1
    local exit_code=$?
    echo -e "${RED}Error occurred in script at line: $line_no${NC}"
    echo -e "${RED}Exit code: $exit_code${NC}"
    
    # Don't exit if we're in the validator creation section
    if [ $line_no -ge 400 ] && [ $line_no -le 540 ]; then
        echo -e "${YELLOW}Note: This error occurred during validator creation which has retry logic${NC}"
        return 0
    fi
}

trap 'error_handler ${LINENO}' ERR

# Source common functions
source /scripts/common.sh

# Configuration
KEYRING="${KEYRING:-test}"
NETWORK="${NETWORK:-testnet}"
PCHAIN_HOME="${PCHAIN_HOME:-/root/.pchain}"

# Export for pchaind commands
export KEYRING
export PCHAIN_HOME

# Load network configuration
load_network_config "$NETWORK" || exit 1

# Calculate denomination (18 zeros for 1 PUSH)
ONE_PUSH="000000000000000000${DENOM}"
export ONE_PUSH
export CHAIN_ID

# Fee configuration
DEFAULT_GAS="300000"
DEFAULT_FEE_AMOUNT="100000000000000000"  # 0.1 PUSH in upc (without denom suffix)
MIN_STAKE="1"  # Minimum 1 PUSH stake

# Check if running inside container
check_container "/scripts/setup-node-registration.sh"

# Cool banner
print_banner() {
    echo -e "${BOLD}${CYAN}"
    echo "╔═══════════════════════════════════════════════════╗"
    echo "║        🚀 Push Chain Validator Setup 🚀           ║"
    echo "║                                                   ║"
    echo "║      Simplified Wallet & Registration Flow        ║"
    echo "╚═══════════════════════════════════════════════════╝"
    echo -e "${NC}"
}

# Check if wallet exists
check_wallet() {
    local wallet_name="$1"
    if pchaind keys show "$wallet_name" --keyring-backend "$KEYRING" --home "$PCHAIN_HOME" >/dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

# Create new wallet
create_wallet() {
    local wallet_name="$1"
    log_info "Creating new wallet: $wallet_name"
    echo
    echo -e "${BOLD}${YELLOW}⚠️  IMPORTANT: Save the mnemonic phrase shown below!${NC}"
    echo -e "${YELLOW}This is the ONLY way to recover your wallet.${NC}"
    echo
    
    # Create wallet and capture output
    local wallet_output=$(pchaind keys add "$wallet_name" --keyring-backend "$KEYRING" --home "$PCHAIN_HOME" 2>&1)
    
    # Extract the mnemonic from output (the 24 words after the last blank line)
    local mnemonic=$(echo "$wallet_output" | awk '/^$/{mnemonic=""} {if(mnemonic!="" || $0!="") mnemonic=mnemonic" "$0} END{print mnemonic}' | xargs)
    
    # Check if we got 24 words
    local word_count=$(echo "$mnemonic" | wc -w)
    
    if [ "$word_count" -eq 24 ]; then
        # Display wallet info first
        echo "$wallet_output" | head -n 5
        echo
        
        # Format mnemonic clearly
        echo
        echo -e "${BOLD}${YELLOW}🔐 YOUR SECRET RECOVERY PHRASE 🔐${NC}"
        echo -e "${YELLOW}═══════════════════════════════════════════════════════════════════${NC}"
        echo
        
        # Split mnemonic into array
        read -ra WORDS <<< "$mnemonic"
        
        # Print 6 words per row, 4 rows total
        for i in $(seq 0 6 18); do
            # Build line with 6 words
            LINE=""
            for j in $(seq 0 5); do
                idx=$((i + j))
                if [ $idx -lt 24 ]; then
                    WORD="${WORDS[$idx]}"
                    # Pad each word to 12 characters for alignment
                    LINE="$LINE$(printf "%-12s" "$WORD")"
                fi
            done
            # Print centered line
            echo -e "        ${BOLD}${CYAN}$LINE${NC}"
            echo
        done
        
        echo -e "${YELLOW}═══════════════════════════════════════════════════════════════════${NC}"
        echo -e "${BOLD}${RED}⚠️  CRITICAL: Write these 24 words in order on paper NOW! ⚠️${NC}"
        echo -e "${RED}This is the ONLY way to recover your wallet if lost!${NC}"
        echo
    else
        # Fallback to original display
        echo "$wallet_output"
        echo
        echo -e "${BOLD}${RED}**Important** Write this mnemonic phrase in a safe place.${NC}"
        echo -e "${RED}It is the only way to recover your account if you ever forget your password.${NC}"
    fi
    echo
    
    # Get address directly from keys show command (more reliable)
    local address=$(pchaind keys show "$wallet_name" -a --keyring-backend "$KEYRING" --home "$PCHAIN_HOME" 2>/dev/null)
    
    if [ -z "$address" ] || [ "$address" = "" ]; then
        log_error "Failed to get wallet address"
        return 1
    fi
    
    # Get EVM address
    local evm_address=$(push_to_evm_address "$address")
    
    echo
    echo -e "${BOLD}${GREEN}✓ Wallet created successfully!${NC}"
    echo -e "${BLUE}Cosmos Address: $address${NC}"
    echo -e "${BLUE}ETH Address: ${BOLD}$evm_address${NC}"
    echo
    
    # Force user to confirm they saved mnemonic
    local saved="no"
    while [[ ! "$saved" =~ ^[Yy][Ee][Ss]$ ]]; do
        read -p "Have you saved your mnemonic phrase? Type 'yes' to continue: " saved
        if [[ ! "$saved" =~ ^[Yy][Ee][Ss]$ ]]; then
            echo -e "${RED}Please save your mnemonic before continuing!${NC}"
        fi
    done
    
    return 0
}

# Import existing wallet
import_wallet() {
    local wallet_name="$1"
    
    while true; do
        log_info "Importing existing wallet: $wallet_name"
        echo
        echo -e "${YELLOW}Enter your mnemonic phrase (will be hidden):${NC}"
        
        # Read mnemonic securely
        read -s -r mnemonic
        echo
        
        # Validate mnemonic is not empty
        if [ -z "$mnemonic" ]; then
            log_error "Mnemonic phrase cannot be empty"
            echo
            read -p "Try again? (yes/no): " try_again
            if [[ ! "$try_again" =~ ^[Yy][Ee][Ss]$ ]]; then
                return 1
            fi
            echo
            continue
        fi
        
        # Try to import wallet
        set +e
        local import_result=$(echo "$mnemonic" | pchaind keys add "$wallet_name" --recover --keyring-backend "$KEYRING" --home "$PCHAIN_HOME" 2>&1)
        local import_exit_code=$?
        set -e
        
        if [ $import_exit_code -eq 0 ]; then
            # Get address
            local address=$(pchaind keys show "$wallet_name" -a --keyring-backend "$KEYRING" --home "$PCHAIN_HOME")
            local evm_address=$(push_to_evm_address "$address")
            
            echo
            echo -e "${BOLD}${GREEN}✓ Wallet imported successfully!${NC}"
            echo -e "${BLUE}ETH Address: ${evm_address}${NC}"
            echo
            return 0
        else
            log_error "Failed to import wallet"
            
            # Check specific error types
            if echo "$import_result" | grep -q "invalid mnemonic"; then
                echo "Error: Invalid mnemonic phrase. Please check and try again."
            elif echo "$import_result" | grep -q "already exists"; then
                echo "Error: A wallet with name '$wallet_name' already exists."
                echo "Please choose a different name or use the existing wallet."
                return 1
            else
                echo "Error details:"
                echo "$import_result" | head -5
            fi
            
            echo
            read -p "Try again? (yes/no): " try_again
            if [[ ! "$try_again" =~ ^[Yy][Ee][Ss]$ ]]; then
                return 1
            fi
            echo
        fi
    done
}

# Smart balance check - always use genesis node during setup
check_balance_smart() {
    local address="$1"
    local balance="0"
    
    # Always use genesis node for balance checks during setup
    # Local node might be syncing and not reliable for queries
    balance=$(pchaind query bank balances "$address" --node "$GENESIS_NODE_RPC" -o json 2>/dev/null | jq -r '.balances[] | select(.denom=="'$DENOM'") | .amount // "0"')
    
    if [ -z "$balance" ] || [ "$balance" = "null" ]; then
        balance="0"
    fi
    
    echo "$balance"
}

# Convert upc to PUSH
format_balance() {
    local upc_amount="$1"
    if [ "$upc_amount" = "0" ] || [ -z "$upc_amount" ]; then
        echo "0"
    else
        # Convert from upc to PUSH (divide by 10^18)
        # Using awk instead of bc for better portability
        echo "$upc_amount" | awk '{
            val = $1/1000000000000000000;
            if (val < 0.000001) 
                printf "0.000000"; 
            else 
                printf "%.6f", val
        }'
    fi
}

# Compare large numbers (for amounts in upc)
compare_amounts() {
    local amount1="$1"
    local amount2="$2"
    local operator="$3"  # -ge, -gt, -le, -lt, -eq
    
    # Use awk for comparison to handle large numbers
    case "$operator" in
        "-ge")
            echo "$amount1 $amount2" | awk '{if ($1 >= $2) exit 0; else exit 1}'
            ;;
        "-gt")
            echo "$amount1 $amount2" | awk '{if ($1 > $2) exit 0; else exit 1}'
            ;;
        "-le")
            echo "$amount1 $amount2" | awk '{if ($1 <= $2) exit 0; else exit 1}'
            ;;
        "-lt")
            echo "$amount1 $amount2" | awk '{if ($1 < $2) exit 0; else exit 1}'
            ;;
        "-eq")
            echo "$amount1 $amount2" | awk '{if ($1 == $2) exit 0; else exit 1}'
            ;;
        "-gt")
            echo "$amount1 $amount2" | awk '{if ($1 > $2) exit 0; else exit 1}'
            ;;
        *)
            return 1
            ;;
    esac
}

# Convert push address to EVM format
push_to_evm_address() {
    local push_addr="$1"
    # Use pchaind to convert address
    local evm_addr=$(pchaind debug addr "$push_addr" 2>/dev/null | grep "Address (hex):" | awk '{print "0x"$3}')
    echo "$evm_addr"
}

# Calculate required funds (stake + fees + buffer)
calculate_required_funds() {
    local stake_amount="$1"
    local fee_amount_upc="${2:-$DEFAULT_FEE_AMOUNT}"
    
    # Convert fee from upc to PUSH
    local fee_amount_push=$(echo "$fee_amount_upc" | awk '{printf "%.6f", $1/1000000000000000000}')
    
    # Add 10% buffer for safety
    local total=$(echo "$stake_amount $fee_amount_push" | awk '{printf "%.6f", $1 + $2 + 0.1}')
    echo "$total"
}

# Show funding requirements clearly
show_funding_requirements() {
    local stake_amount="$1"
    local fee_amount_upc="${2:-$DEFAULT_FEE_AMOUNT}"
    local fee_amount_push=$(echo "$fee_amount_upc" | awk '{printf "%.6f", $1/1000000000000000000}')
    local total_required=$(calculate_required_funds "$stake_amount" "$fee_amount_upc")
    
    echo -e "${BOLD}${BLUE}💰 Funding Requirements${NC}"
    echo "════════════════════════════════════════════════"
    echo -e "Stake amount:    ${GREEN}$stake_amount PUSH${NC}"
    echo -e "Transaction fee: ${YELLOW}~$fee_amount_push PUSH${NC}"
    echo -e "Safety buffer:   ${YELLOW}~0.1 PUSH${NC}"
    echo -e "${BOLD}Total needed:    ${CYAN}$total_required PUSH${NC}"
    echo
}

# Enhanced faucet instructions
show_faucet_instructions() {
    local address="$1"
    local evm_address="$2"
    local required_amount="$3"
    
    echo -e "${BOLD}${YELLOW}📧 Get Test Tokens${NC}"
    echo "════════════════════════════════════════════════"
    echo
    echo -e "${BLUE}You need at least ${BOLD}$required_amount PUSH${NC}"
    echo
    echo -e "${GREEN}Your funding address:${NC}"
    echo -e "${BOLD}${CYAN}$evm_address${NC}"
    echo
    echo -e "${GREEN}Steps to get tokens:${NC}"
    echo "1. Visit: $FAUCET"
    echo "2. Paste the address above"
    echo "3. Complete captcha & request tokens"
    echo
    echo -e "${YELLOW}Note: Faucet limit is once per 24 hours${NC}"
    echo
}

# Pre-flight checks before registration
pre_flight_checks() {
    local wallet_address="$1"
    local stake_amount="$2"
    
    echo -e "${BOLD}${BLUE}🔍 Pre-flight Checks${NC}"
    echo "════════════════════════════════════════════════"
    
    local all_good=true
    
    # Check 1: Balance
    local balance=$(check_balance_smart "$wallet_address")
    local balance_push=$(format_balance "$balance")
    local required_amount=$(calculate_required_funds "$stake_amount")
    local required_upc=$(echo "$required_amount" | awk '{printf "%.0f", $1 * 1000000000000000000}')
    
    echo -n "✓ Balance check: "
    if compare_amounts "$balance" "$required_upc" "-ge"; then
        echo -e "${GREEN}PASS${NC} ($balance_push PUSH)"
    else
        echo -e "${RED}FAIL${NC} (Have: $balance_push PUSH, Need: $required_amount PUSH)"
        all_good=false
    fi
    
    # Check 2: Node sync status
    echo -n "✓ Node sync: "
    local sync_status=$(pchaind status --home "$PCHAIN_HOME" 2>/dev/null | jq -r '.sync_info.catching_up // true')
    if [ "$sync_status" = "false" ]; then
        echo -e "${GREEN}Fully synced${NC}"
    else
        echo -e "${YELLOW}Still syncing (registration will work)${NC}"
    fi
    
    # Check 3: Validator key
    echo -n "✓ Validator key: "
    local val_pubkey=$(pchaind comet show-validator --home "$PCHAIN_HOME" 2>/dev/null)
    if [ -n "$val_pubkey" ]; then
        echo -e "${GREEN}Found${NC}"
    else
        echo -e "${RED}Not found${NC}"
        all_good=false
    fi
    
    # Check 4: Network connectivity
    echo -n "✓ Network connection: "
    # Convert tcp:// to http:// for curl
    local test_url="${GENESIS_NODE_RPC/tcp:\/\//http://}/status"
    if curl -s --connect-timeout 5 "$test_url" >/dev/null 2>&1; then
        echo -e "${GREEN}Connected${NC}"
    else
        echo -e "${YELLOW}Cannot reach genesis node (will use local submission)${NC}"
        # Don't fail the check - we can still submit locally
    fi
    
    echo
    
    if [ "$all_good" = "true" ]; then
        echo -e "${BOLD}${GREEN}✅ All checks passed! Ready to register.${NC}"
        return 0
    else
        echo -e "${BOLD}${RED}❌ Some checks failed. Please fix issues above.${NC}"
        return 1
    fi
}

# Enhanced validator registration with better error handling
register_validator() {
    local wallet_name="$1"
    local wallet_address="$2"
    
    echo
    echo -e "${BOLD}${PURPLE}🏛️  Validator Registration${NC}"
    echo "════════════════════════════════════════════════"
    echo
    
    # Get validator info
    local node_id=$(pchaind tendermint show-node-id --home "$PCHAIN_HOME")
    local validator_pubkey=$(pchaind comet show-validator --home "$PCHAIN_HOME")
    
    log_info "Node ID: $node_id"
    log_info "Validator Pubkey: ${validator_pubkey:0:20}..."
    
    # Get current balance
    local balance=$(check_balance_smart "$wallet_address")
    local balance_push=$(format_balance "$balance")
    echo -e "${BLUE}Current balance: ${BOLD}$balance_push PUSH${NC}"
    echo
    
    # Get validator details
    read -p "Enter validator name (moniker) [$MONIKER]: " validator_name
    validator_name="${validator_name:-$MONIKER}"
    
    read -p "Enter website URL (optional): " website
    read -p "Enter security contact email (optional): " security
    read -p "Enter validator description (optional): " details
    details="${details:-A Push Chain validator}"
    
    # Get stake amount with validation
    echo
    local max_stake=$(echo "$balance_push" | awk '{printf "%.6f", $1 - 0.2}')  # Reserve 0.2 PUSH for fees
    echo -e "${YELLOW}Maximum stakeable: $max_stake PUSH (keeping 0.2 PUSH for fees)${NC}"
    echo -e "${YELLOW}Minimum stake: $MIN_STAKE PUSH${NC}"
    echo
    
    local stake_amount
    while true; do
        read -p "Enter amount to stake (in PUSH): " stake_amount
        
        # Validate stake amount
        if ! [[ "$stake_amount" =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
            echo -e "${RED}Invalid amount. Please enter a number.${NC}"
            continue
        fi
        
        if [ "$(echo "$stake_amount $MIN_STAKE" | awk '{print ($1 < $2)}')" = "1" ]; then
            echo -e "${RED}Stake must be at least $MIN_STAKE PUSH${NC}"
            continue
        fi
        
        if [ "$(echo "$stake_amount $max_stake" | awk '{print ($1 > $2)}')" = "1" ]; then
            echo -e "${RED}Insufficient balance. Maximum stakeable is $max_stake PUSH${NC}"
            continue
        fi
        
        break
    done
    
    # Show funding requirements
    echo
    show_funding_requirements "$stake_amount"
    
    # Run pre-flight checks
    if ! pre_flight_checks "$wallet_address" "$stake_amount"; then
        echo
        echo -e "${RED}Pre-flight checks failed. Please resolve issues and try again.${NC}"
        return 1
    fi
    
    # Convert to upc
    local stake_upc="${stake_amount}${ONE_PUSH}"
    
    # Use default commission rates (hidden for now)
    local commission_rate="0.1"
    local commission_max_rate="0.2"
    local commission_max_change_rate="0.01"
    
    # Create validator config file
    cat <<EOF > /tmp/register-validator.json
{
    "pubkey": $validator_pubkey,
    "amount": "${stake_upc}",
    "moniker": "$validator_name",
    "website": "$website",
    "security": "$security",
    "details": "$details",
    "commission-rate": "$commission_rate",
    "commission-max-rate": "$commission_max_rate",
    "commission-max-change-rate": "$commission_max_change_rate",
    "min-self-delegation": "1"
}
EOF
    
    # Show summary
    echo
    echo -e "${BOLD}${BLUE}📋 Validator Configuration Summary${NC}"
    echo "════════════════════════════════════════════════"
    cat /tmp/register-validator.json | jq .
    echo
    
    # Final confirmation
    read -p "Proceed with validator registration? (yes/no): " confirm
    if [[ ! "$confirm" =~ ^[Yy][Ee][Ss]$ ]]; then
        log_info "Registration cancelled"
        return 1
    fi
    
    # Calculate optimal fee
    local fee_amount="${DEFAULT_FEE_AMOUNT}${DENOM}"  # e.g., 100000000000000000upc
    
    # Create validator with retry logic
    log_info "Creating validator transaction..."
    
    local attempt=1
    local max_attempts=3
    local tx_hash=""
    
    while [ $attempt -le $max_attempts ]; do
        echo -e "${LIGHT_BLUE}Attempt $attempt of $max_attempts...${NC}"
        
        # Try genesis node first, fallback to local
        local node_url="$GENESIS_NODE_RPC"
        if ! curl -s --connect-timeout 2 "${GENESIS_NODE_RPC/tcp:\/\//http://}/status" >/dev/null 2>&1; then
            log_info "Using local node for transaction submission"
            node_url="tcp://localhost:26657"
        fi
        
        # Debug: Show what we're about to execute
        echo -e "${YELLOW}Debug: Executing validator creation...${NC}"
        echo "Node URL: $node_url"
        echo "Chain ID: $CHAIN_ID"
        echo "Wallet: $wallet_name"
        echo "Fee: $fee_amount"
        
        # Temporarily disable exit on error for this command
        set +e
        local tx_result=$(pchaind tx staking create-validator /tmp/register-validator.json \
            --chain-id "$CHAIN_ID" \
            --fees "$fee_amount" \
            --gas "$DEFAULT_GAS" \
            --from "$wallet_name" \
            --node="$node_url" \
            --keyring-backend "$KEYRING" \
            --home "$PCHAIN_HOME" \
            --yes \
            --output json 2>&1)
        local exit_code=$?
        set -e
        
        echo -e "${YELLOW}Debug: Command exit code: $exit_code${NC}"
        
        # Try to extract tx hash
        tx_hash=$(echo "$tx_result" | jq -r '.txhash // empty' 2>/dev/null || echo "")
        
        if [ -n "$tx_hash" ] && [ "$tx_hash" != "null" ]; then
            log_success "Transaction submitted! Hash: $tx_hash"
            echo -e "${LIGHT_BLUE}Transaction hash: ${BOLD}$tx_hash${NC}"
            break
        else
            log_warning "Transaction failed on attempt $attempt"
            echo -e "${YELLOW}Error details:${NC}"
            echo "$tx_result" | jq . 2>/dev/null || echo "$tx_result"
            
            # Check for specific error types
            if echo "$tx_result" | grep -q "insufficient fee"; then
                # Extract numeric part and increase by 50%
                local fee_numeric=$(echo "$fee_amount" | sed "s/$DENOM//")
                fee_numeric=$(echo "$fee_numeric" | awk '{printf "%.0f", $1 * 1.5}')
                fee_amount="${fee_numeric}${DENOM}"
                log_info "Retrying with higher fee: $fee_amount"
            elif echo "$tx_result" | grep -q "account sequence mismatch"; then
                log_info "Sequence mismatch, retrying..."
                sleep 3
            elif echo "$tx_result" | grep -q "validator already exist"; then
                log_error "Validator already exists with this address!"
                return 1
            fi
            
            ((attempt++))
            
            if [ $attempt -le $max_attempts ]; then
                sleep 2
            fi
        fi
    done
    
    if [ -z "$tx_hash" ] || [ "$tx_hash" = "" ]; then
        log_error "Failed to create validator after $max_attempts attempts"
        echo "$tx_result"
        return 1
    fi
    
    # Wait for transaction confirmation
    echo -e "${LIGHT_BLUE}Waiting for transaction confirmation...${NC}"
    
    local confirmation_attempts=0
    local max_confirmation_attempts=30  # 30 attempts * 2 seconds = 60 seconds max wait
    local tx_confirmed=false
    local tx_code="-1"
    
    while [ $confirmation_attempts -lt $max_confirmation_attempts ]; do
        # Try to query the transaction
        set +e
        local tx_query=$(pchaind query tx "$tx_hash" --chain-id "$CHAIN_ID" --node="$node_url" --output json 2>/dev/null)
        local query_exit_code=$?
        set -e
        
        if [ $query_exit_code -eq 0 ] && [ -n "$tx_query" ]; then
            tx_code=$(echo "$tx_query" | jq -r '.code // "-1"' 2>/dev/null || echo "-1")
            
            if [ "$tx_code" != "-1" ]; then
                tx_confirmed=true
                break
            fi
        fi
        
        # Show progress with proper carriage return and clear line
        echo -ne "\r\033[K${LIGHT_BLUE}Waiting for transaction to be included in a block... $((confirmation_attempts * 2))/$((max_confirmation_attempts * 2)) seconds${NC}"
        
        ((confirmation_attempts++))
        sleep 2
    done
    
    echo  # New line after progress
    
    if [ "$tx_confirmed" = true ] && [ "$tx_code" = "0" ]; then
        echo
        echo -e "${BOLD}${GREEN}🎉 Congratulations! Your validator has been created!${NC}"
        echo
        
        # Get validator operator address
        local val_addr=$(pchaind keys show $wallet_name --bech val -a --keyring-backend $KEYRING --home $PCHAIN_HOME 2>/dev/null || echo "")
        
        echo -e "${LIGHT_BLUE}Verifying validator status...${NC}"
        
        # Wait a bit for validator to be indexed
        sleep 5
        
        # Query validator using the moniker
        set +e
        local validator_info=$(pchaind query staking validators --node="$GENESIS_NODE_RPC" --output json 2>/dev/null | \
            jq ".validators[] | select(.description.moniker==\"$validator_name\")" 2>/dev/null || echo "")
        set -e
        
        if [ -n "$validator_info" ] && [ "$validator_info" != "{}" ]; then
            local val_status=$(echo "$validator_info" | jq -r '.status // "UNKNOWN"' 2>/dev/null || echo "UNKNOWN")
            local tokens=$(echo "$validator_info" | jq -r '.tokens // "0"' 2>/dev/null || echo "0")
            local tokens_push=$(format_balance "$tokens")
            
            echo -e "${GREEN}✓ Validator found in active set!${NC}"
            echo -e "Status: $val_status"
            echo -e "Staked: $tokens_push PUSH"
            
            # Show validator status interpretation
            case "$val_status" in
                "BOND_STATUS_BONDED")
                    echo -e "${GREEN}✓ Your validator is active and producing blocks!${NC}"
                    ;;
                "BOND_STATUS_UNBONDING")
                    echo -e "${YELLOW}⚠ Your validator is unbonding${NC}"
                    ;;
                "BOND_STATUS_UNBONDED")
                    echo -e "${YELLOW}⚠ Your validator is not yet bonded${NC}"
                    ;;
            esac
        else
            echo -e "${YELLOW}⚠ Validator not found in active set yet. This is normal - it may take a few minutes.${NC}"
            echo "You can check status later with: ./push-node-manager status"
        fi
        
        # Always show validator list even if not found yet
        echo
        
        echo
        echo -e "${BOLD}${BLUE}📊 Validator Details:${NC}"
        echo "- Name: $validator_name"
        if [ -n "$val_addr" ]; then
            echo "- Operator Address: $val_addr"
        fi
        echo "- Transaction Hash: $tx_hash"
        echo
        echo -e "${BLUE}Next steps:${NC}"
        echo "1. Monitor your validator: ./push-node-manager status"
        echo "2. Check balance: ./push-node-manager balance"
        echo "3. View logs: ./push-node-manager logs"
        echo
        
        # List all validators
        echo -e "${BOLD}${CYAN}📋 All Active Validators:${NC}"
        echo "════════════════════════════════════════════════"
        
        # Temporarily disable exit on error for validator listing
        set +e
        local all_validators=$(pchaind query staking validators --node="$GENESIS_NODE_RPC" --output json 2>/dev/null)
        local query_result=$?
        
        if [ $query_result -eq 0 ] && [ -n "$all_validators" ] && [ "$all_validators" != "null" ]; then
            # Parse validators directly without base64 encoding
            local validator_count=$(echo "$all_validators" | jq '.validators | length' 2>/dev/null || echo "0")
            
            if [ "$validator_count" -gt 0 ]; then
                for i in $(seq 0 $((validator_count - 1))); do
                    # Use safer parsing with error handling
                    local moniker=$(echo "$all_validators" | jq -r ".validators[$i].description.moniker // \"Unknown\"" 2>/dev/null || echo "Unknown")
                    local status=$(echo "$all_validators" | jq -r ".validators[$i].status // \"UNKNOWN\"" 2>/dev/null || echo "UNKNOWN")
                    local tokens=$(echo "$all_validators" | jq -r ".validators[$i].tokens // \"0\"" 2>/dev/null || echo "0")
                    
                    # Handle format_balance errors
                    local tokens_push="0"
                    if command -v format_balance >/dev/null 2>&1; then
                        tokens_push=$(format_balance "$tokens" 2>/dev/null || echo "0")
                    else
                        # Fallback if format_balance is not available
                        tokens_push=$(echo "scale=6; $tokens / 1000000000000000000" | bc 2>/dev/null || echo "0")
                    fi
                    
                    # Format status nicely
                    local status_display=""
                    case "$status" in
                        "BOND_STATUS_BONDED")
                            status_display="${GREEN}BONDED${NC}"
                            ;;
                        "BOND_STATUS_UNBONDING")
                            status_display="${YELLOW}UNBONDING${NC}"
                            ;;
                        "BOND_STATUS_UNBONDED")
                            status_display="${RED}UNBONDED${NC}"
                            ;;
                        *)
                            status_display="$status"
                            ;;
                    esac
                    
                    if [ "$moniker" = "$validator_name" ]; then
                        echo -e "${GREEN}➤ $moniker - Status: $status_display - Stake: $tokens_push PUSH ${BOLD}(YOUR VALIDATOR)${NC}"
                    else
                        echo -e "  $moniker - Status: $status_display - Stake: $tokens_push PUSH"
                    fi
                done
            else
                echo "No validators found in the list"
            fi
        else
            echo "Unable to fetch validator list (this is normal if the network is still syncing)"
        fi
        # Re-enable exit on error
        set -e
        echo
        
        # Show remaining balance
        local new_balance=$(check_balance_smart "$wallet_address")
        local new_balance_push=$(format_balance "$new_balance")
        echo -e "${GREEN}Remaining balance: $new_balance_push PUSH${NC}"
        
        return 0
    else
        if [ "$tx_confirmed" = true ]; then
            log_error "Transaction failed! Code: $tx_code"
            
            # Extract and display user-friendly error
            set +e
            local raw_log=$(echo "$tx_query" | jq -r '.raw_log // "Unknown error"' 2>/dev/null || echo "Unknown error")
            set -e
            
            if echo "$raw_log" | grep -q "insufficient funds"; then
                echo -e "${RED}Error: Insufficient funds for staking + fees${NC}"
                echo "Please ensure you have enough PUSH to cover:"
                echo "- Stake amount: $stake_amount PUSH"
                echo "- Transaction fees: ~0.1 PUSH"
            else
                echo -e "${RED}Error details:${NC}"
                echo "$raw_log" | head -5
            fi
        else
            log_error "Transaction was not confirmed within timeout period"
            echo -e "${YELLOW}Transaction hash: $tx_hash${NC}"
            echo
            echo "You can verify the transaction manually with:"
            echo -e "${CYAN}export TX_ID=$tx_hash${NC}"
            echo -e "${CYAN}pchaind query tx \$TX_ID --chain-id $CHAIN_ID --output json | jq '{code, raw_log}'${NC}"
            echo
            echo "To check if your validator is in the active set:"
            echo -e "${CYAN}pchaind query staking validators --output json | jq '.validators[] | select(.description.moniker=="$validator_name")'${NC}"
            echo
            
            # Still show the validator list even if timeout
            echo -e "${BOLD}${CYAN}📋 Current Validators:${NC}"
            echo "════════════════════════════════════════════════"
            
            # Temporarily disable exit on error for validator listing
            set +e
            local all_validators=$(pchaind query staking validators --node="$GENESIS_NODE_RPC" --output json 2>/dev/null)
            local query_result=$?
            
            if [ $query_result -eq 0 ] && [ -n "$all_validators" ] && [ "$all_validators" != "null" ]; then
                local validator_count=$(echo "$all_validators" | jq '.validators | length' 2>/dev/null || echo "0")
                
                if [ "$validator_count" -gt 0 ]; then
                    for i in $(seq 0 $((validator_count - 1))); do
                        # Use safer parsing with error handling
                        local moniker=$(echo "$all_validators" | jq -r ".validators[$i].description.moniker // \"Unknown\"" 2>/dev/null || echo "Unknown")
                        local status=$(echo "$all_validators" | jq -r ".validators[$i].status // \"UNKNOWN\"" 2>/dev/null || echo "UNKNOWN")
                        local tokens=$(echo "$all_validators" | jq -r ".validators[$i].tokens // \"0\"" 2>/dev/null || echo "0")
                        
                        # Handle format_balance errors
                        local tokens_push="0"
                        if command -v format_balance >/dev/null 2>&1; then
                            tokens_push=$(format_balance "$tokens" 2>/dev/null || echo "0")
                        else
                            # Fallback if format_balance is not available
                            tokens_push=$(echo "scale=6; $tokens / 1000000000000000000" | bc 2>/dev/null || echo "0")
                        fi
                        
                        # Format status nicely
                        local status_display=""
                        case "$status" in
                            "BOND_STATUS_BONDED")
                                status_display="${GREEN}BONDED${NC}"
                                ;;
                            "BOND_STATUS_UNBONDING")
                                status_display="${YELLOW}UNBONDING${NC}"
                                ;;
                            "BOND_STATUS_UNBONDED")
                                status_display="${RED}UNBONDED${NC}"
                                ;;
                            *)
                                status_display="$status"
                                ;;
                        esac
                    
                    echo -e "  $moniker - Status: $status_display - Stake: $tokens_push PUSH"
                done
            else
                echo "No validators found in the list"
            fi
        else
            echo "Unable to fetch validator list"
        fi
        # Re-enable exit on error
        set -e
        fi
        
        return 1
    fi
}

# Main setup flow
main() {
    print_banner
    
    # First, check if node already has validator keys registered
    local node_pubkey=$(pchaind tendermint show-validator --home "$PCHAIN_HOME" 2>/dev/null | jq -r '.key' 2>/dev/null || echo "")
    local existing_validator_by_pubkey=""
    local using_existing_validator=false
    local force_wallet_import=false
    
    if [ -n "$node_pubkey" ]; then
        set +e
        existing_validator_by_pubkey=$(pchaind query staking validators --node="$GENESIS_NODE_RPC" --output json 2>/dev/null | \
            jq ".validators[] | select(.consensus_pubkey.value==\"$node_pubkey\")" 2>/dev/null || echo "")
        set -e
        
        if [ -n "$existing_validator_by_pubkey" ] && [ "$existing_validator_by_pubkey" != "null" ]; then
            # This node's validator key is already registered
            local pubkey_val_moniker=$(echo "$existing_validator_by_pubkey" | jq -r '.description.moniker // "Unknown"')
            local pubkey_val_operator=$(echo "$existing_validator_by_pubkey" | jq -r '.operator_address // "Unknown"')
            local pubkey_val_status=$(echo "$existing_validator_by_pubkey" | jq -r '.status // "UNKNOWN"')
            
            echo
            echo -e "${YELLOW}⚠️  This node's validator key is already registered!${NC}"
            echo
            echo -e "${BOLD}${CYAN}Existing Validator Details:${NC}"
            echo "- Name: $pubkey_val_moniker"
            echo "- Operator: $pubkey_val_operator"
            echo "- Status: $pubkey_val_status"
            echo
            echo "What would you like to do?"
            echo "1) Use the existing validator (need to import controlling wallet)"
            echo "2) Create a NEW validator (will reset node keys)"
            echo "3) Exit"
            echo
            read -p "Choose option (1-3): " validator_choice
            
            case "$validator_choice" in
                1)
                    echo
                    echo -e "${CYAN}To use the existing validator '$pubkey_val_moniker':${NC}"
                    echo "You MUST import the wallet that originally created this validator."
                    echo
                    echo "The validator operator address is: $pubkey_val_operator"
                    echo
                    read -p "Press Enter to continue..."
                    using_existing_validator=true
                    force_wallet_import=true
                    ;;
                2)
                    echo
                    echo -e "${YELLOW}⚠️  This will reset your validator keys and create a NEW validator!${NC}"
                    echo "Your current validator '$pubkey_val_moniker' will remain but won't be controlled by this node."
                    echo
                    read -p "Are you sure you want to create a new validator? (yes/no): " confirm_reset
                    
                    if [[ "$confirm_reset" =~ ^[Yy][Ee][Ss]$ ]]; then
                        echo
                        echo -e "${CYAN}Resetting validator keys...${NC}"
                        
                        # Remove the old validator key
                        rm -f "$CONFIG_DIR/priv_validator_key.json"
                        
                        # Generate new validator key
                        echo -e "${YELLOW}Please restart your node to generate new keys:${NC}"
                        echo -e "  ${CYAN}./push-node-manager restart${NC}"
                        echo
                        echo "After restart, run this command again to register as a new validator."
                        exit 0
                    else
                        echo "Reset cancelled. Exiting..."
                        exit 0
                    fi
                    ;;
                3)
                    echo "Exiting..."
                    exit 0
                    ;;
                *)
                    echo -e "${RED}Invalid choice. Exiting...${NC}"
                    exit 1
                    ;;
            esac
        fi
    fi
    
    # Step 1: Wallet Setup
    echo -e "${BOLD}${BLUE}Step 1: Wallet Setup${NC}"
    echo "════════════════════════════════════════════════"
    echo
    
    local wallet_name=""
    local wallet_setup_complete=false
    
    # Loop until wallet setup is complete
    while [ "$wallet_setup_complete" = false ]; do
        # First, check if there are any existing wallets
        local existing_wallets=$(pchaind keys list --keyring-backend "$KEYRING" --home "$PCHAIN_HOME" 2>/dev/null | grep -E "^  name:" | awk '{print $2}' || echo "")
        
        if [ -n "$existing_wallets" ]; then
            echo -e "${GREEN}Found existing wallet(s):${NC}"
            echo "$existing_wallets" | while read -r wallet; do
                if [ -n "$wallet" ]; then
                    # Get wallet address
                    local wallet_addr=$(pchaind keys show "$wallet" -a --keyring-backend "$KEYRING" --home "$PCHAIN_HOME" 2>/dev/null || echo "")
                    local wallet_evm=$(push_to_evm_address "$wallet_addr" 2>/dev/null || echo "")
                    if [ -n "$wallet_evm" ]; then
                        printf "  • %-20s %s\n" "$wallet" "($wallet_evm)"
                    else
                        echo "  • $wallet"
                    fi
                fi
            done
            
            echo
            echo "What would you like to do?"
            if [ "$force_wallet_import" = true ]; then
                echo "3) Import wallet from seed phrase"
                echo
                echo -e "${YELLOW}Note: You must import the wallet to use the existing validator${NC}"
                main_option=3
            else
                echo "1) Use existing wallet"
                echo "2) Create new wallet"
                echo "3) Import wallet from seed phrase"
                read -p "Choose option (1-3): " main_option
            fi
            
            case "$main_option" in
                1)
                    # If only one wallet, use it. Otherwise ask which one
                    local wallet_count=$(echo "$existing_wallets" | wc -l | tr -d ' ')
                    if [ "$wallet_count" -eq 1 ]; then
                        wallet_name=$(echo "$existing_wallets" | head -1)
                    else
                        echo
                        read -p "Enter wallet name to use: " wallet_name
                        # Verify wallet exists
                        if ! echo "$existing_wallets" | grep -q "^$wallet_name$"; then
                            log_error "Wallet '$wallet_name' not found"
                            echo
                            continue
                        fi
                    fi
                    
                    # Show wallet details
                    local address=$(pchaind keys show "$wallet_name" -a --keyring-backend "$KEYRING" --home "$PCHAIN_HOME")
                    local evm_address=$(push_to_evm_address "$address")
                    local balance=$(check_balance_smart "$address")
                    local balance_push=$(format_balance "$balance")
                    
                    echo
                    log_info "Using wallet: $wallet_name"
                    echo -e "${BLUE}ETH Address: $evm_address${NC}"
                    echo -e "${GREEN}Balance:     $balance_push PUSH${NC}"
                    echo
                    
                    read -p "Continue with this wallet? (yes/no): " confirm
                    if [[ "$confirm" =~ ^[Yy][Ee][Ss]$ ]]; then
                        wallet_setup_complete=true
                    else
                        echo
                        continue
                    fi
                    ;;
                2)
                    # Create new wallet
                    while true; do
                        read -p "Enter name for new wallet: " new_wallet_name
                        if [ -z "$new_wallet_name" ]; then
                            log_error "Wallet name cannot be empty"
                            echo
                            continue
                        fi
                        
                        # Check if wallet already exists
                        if echo "$existing_wallets" | grep -q "^$new_wallet_name$"; then
                            log_error "Wallet '$new_wallet_name' already exists"
                            echo
                            continue
                        fi
                        
                        wallet_name="$new_wallet_name"
                        if create_wallet "$wallet_name"; then
                            wallet_setup_complete=true
                            break
                        fi
                    done
                    ;;
                3)
                    # Import wallet
                    while true; do
                        read -p "Enter name for imported wallet: " import_wallet_name
                        if [ -z "$import_wallet_name" ]; then
                            log_error "Wallet name cannot be empty"
                            echo
                            continue
                        fi
                        
                        # Check if wallet already exists
                        if echo "$existing_wallets" | grep -q "^$import_wallet_name$"; then
                            log_error "Wallet '$import_wallet_name' already exists"
                            echo
                            continue
                        fi
                        
                        wallet_name="$import_wallet_name"
                        if import_wallet "$wallet_name"; then
                            wallet_setup_complete=true
                            break
                        else
                            # Import failed, go back to main menu
                            echo
                            break
                        fi
                    done
                    ;;
                
                *)
                    log_error "Invalid option. Please choose 1, 2, or 3."
                    echo
                    ;;
            esac
        else
            # No existing wallets
            echo -e "${YELLOW}No wallets found. Let's create one!${NC}"
            echo
            echo -e "${CYAN}A wallet is required to:${NC}"
            echo "  • Receive test tokens from the faucet"
            echo "  • Register as a validator"
            echo "  • Receive staking rewards"
            echo
            echo "What would you like to do?"
            echo "1) Create new wallet (recommended)"
            echo "2) Import existing wallet from seed phrase"
            read -p "Choose option (1-2): " wallet_option
            
            case "$wallet_option" in
                1)
                    read -p "Enter name for new wallet (default: validator): " new_wallet_name
                    if [ -z "$new_wallet_name" ]; then
                        new_wallet_name="validator"
                    fi
                    
                    wallet_name="$new_wallet_name"
                    if create_wallet "$wallet_name"; then
                        wallet_setup_complete=true
                    fi
                    ;;
                2)
                    read -p "Enter name for imported wallet: " import_wallet_name
                    if [ -z "$import_wallet_name" ]; then
                        log_error "Wallet name cannot be empty"
                        echo
                        continue
                    fi
                    
                    wallet_name="$import_wallet_name"
                    if import_wallet "$wallet_name"; then
                        wallet_setup_complete=true
                    else
                        # Import failed, try again
                        echo
                    fi
                    ;;
                *)
                    log_error "Invalid option. Please choose 1 or 2."
                    echo
                    ;;
            esac
        fi
    done
    
    # Get wallet address
    local wallet_address=$(pchaind keys show "$wallet_name" -a --keyring-backend "$KEYRING" --home "$PCHAIN_HOME")
    
    # Check if wallet is already a validator (skip if we already handled validator setup)
    if [ "$using_existing_validator" != true ]; then
        echo
        echo -e "${BOLD}${BLUE}Step 2: Checking Wallet Validator Status${NC}"
        echo "════════════════════════════════════════════════"
        echo
        
        log_info "Checking if wallet is already a validator..."
        
        # Get validator address
        local val_addr=$(pchaind keys show "$wallet_name" --bech val -a --keyring-backend "$KEYRING" --home "$PCHAIN_HOME" 2>/dev/null || echo "")
        
        # Check if validator exists by searching all validators
        set +e
        local existing_validator=$(pchaind query staking validators --node="$GENESIS_NODE_RPC" --output json 2>/dev/null | \
            jq ".validators[] | select(.operator_address==\"$val_addr\")" 2>/dev/null)
        set -e
    fi
    
    if [ "$using_existing_validator" != true ] && [ -n "$existing_validator" ] && [ "$existing_validator" != "null" ]; then
        # Validator already exists
        local val_moniker=$(echo "$existing_validator" | jq -r '.description.moniker // "Unknown"')
        local val_status=$(echo "$existing_validator" | jq -r '.status // "UNKNOWN"')
        local val_tokens=$(echo "$existing_validator" | jq -r '.tokens // "0"')
        local val_tokens_push=$(format_balance "$val_tokens")
        
        echo -e "${GREEN}✓ This wallet is already a validator!${NC}"
        echo
        echo -e "${BOLD}${CYAN}Validator Details:${NC}"
        echo "- Name: $val_moniker"
        echo "- Operator Address: $val_addr"
        echo "- Status: $val_status"
        echo "- Staked: $val_tokens_push PUSH"
        echo
        
        # Show all validators list
        echo -e "${BOLD}${CYAN}📋 All Active Validators:${NC}"
        echo "════════════════════════════════════════════════"
        
        set +e
        local all_validators=$(pchaind query staking validators --node="$GENESIS_NODE_RPC" --output json 2>/dev/null)
        set -e
        
        if [ -n "$all_validators" ] && [ "$all_validators" != "null" ]; then
            echo "$all_validators" | jq -r '.validators[] | @base64' | while IFS= read -r validator_b64; do
                local validator_json=$(echo "$validator_b64" | base64 -d)
                local moniker=$(echo "$validator_json" | jq -r '.description.moniker')
                local status=$(echo "$validator_json" | jq -r '.status')
                local tokens=$(echo "$validator_json" | jq -r '.tokens')
                local tokens_push=$(format_balance "$tokens")
                
                # Format status nicely
                local status_display=""
                case "$status" in
                    "BOND_STATUS_BONDED")
                        status_display="${GREEN}BONDED${NC}"
                        ;;
                    "BOND_STATUS_UNBONDING")
                        status_display="${YELLOW}UNBONDING${NC}"
                        ;;
                    "BOND_STATUS_UNBONDED")
                        status_display="${RED}UNBONDED${NC}"
                        ;;
                    *)
                        status_display="$status"
                        ;;
                esac
                
                if [ "$moniker" = "$val_moniker" ]; then
                    echo -e "${GREEN}➤ $moniker - Status: $status_display - Stake: $tokens_push PUSH ${BOLD}(YOUR VALIDATOR)${NC}"
                else
                    echo -e "  $moniker - Status: $status_display - Stake: $tokens_push PUSH"
                fi
            done
        fi
        
        echo
        echo -e "${BLUE}Validator management commands:${NC}"
        echo "- Monitor status: ./push-node-manager status"
        echo "- Check balance: ./push-node-manager balance"
        echo "- View logs: ./push-node-manager logs"
        
        exit 0
    fi
    
    # This check was already done at the beginning of main() if using_existing_validator is true
    
    # Check Balance with smart detection
    echo
    if [ "$using_existing_validator" = true ]; then
        echo -e "${BOLD}${BLUE}Step 2: Checking Wallet Balance${NC}"
    else
        echo -e "${BOLD}${BLUE}Step 3: Checking Wallet Balance${NC}"
    fi
    echo "════════════════════════════════════════════════"
    echo
    
    log_info "Checking current balance..."
    local balance=$(check_balance_smart "$wallet_address")
    local balance_push=$(format_balance "$balance")
    echo -e "${BLUE}Current balance: ${BOLD}$balance_push PUSH${NC}"
    
    # Calculate minimum required (1 PUSH stake + 0.3 PUSH for fees/buffer)
    local min_required_push="1.3"
    local min_required_upc=$(echo "$min_required_push" | awk '{printf "%.0f", $1 * 1000000000000000000}')
    
    # Keep checking balance until funded
    while ! compare_amounts "$balance" "$min_required_upc" "-ge"; do
        echo
        log_warning "Insufficient balance - need $min_required_push PUSH to register"
        echo
        
        # Convert to EVM address for faucet
        local evm_address=$(push_to_evm_address "$wallet_address")
        
        # Show simplified funding info
        echo -e "${BOLD}${YELLOW}📧 Get Test Tokens${NC}"
        echo "════════════════════════════════════════════════"
        echo
        echo -e "1) Go to ${CYAN}$FAUCET${NC} and paste:"
        echo -e "   ${BOLD}${CYAN}$evm_address${NC}"
        echo
        echo "2) Check balance (if already requested)"
        echo "3) Exit"
        echo
        read -p "Choose option (1-3): " funding_option
        
        case "$funding_option" in
            1)
                echo
                echo -e "${YELLOW}Faucet gives 100 PUSH (once per 24 hours)${NC}"
                echo
                wait_for_funding "$wallet_address" "$min_required_upc"
                # After funding completes, recheck balance
                balance=$(check_balance_smart "$wallet_address")
                balance_push=$(format_balance "$balance")
                ;;
            2)
                echo
                echo -e "${BLUE}Checking balance...${NC}"
                balance=$(check_balance_smart "$wallet_address")
                balance_push=$(format_balance "$balance")
                
                if compare_amounts "$balance" "$min_required_upc" "-ge"; then
                    echo -e "${BOLD}${GREEN}✓ Wallet funded! Balance: $balance_push PUSH${NC}"
                    sleep 2
                else
                    echo -e "${YELLOW}Balance: $balance_push PUSH (need $min_required_push PUSH)${NC}"
                fi
                ;;
            3)
                log_warning "Skipping funding. You'll need tokens to register as a validator."
                exit 0
                ;;
            *)
                log_error "Invalid option"
                exit 1
                ;;
        esac
    done
    
    # If we get here, wallet is funded
    log_success "✅ Wallet has sufficient balance!"
    
    # Check if we're using an existing validator
    if [ "$using_existing_validator" = true ]; then
        # Check if the imported wallet controls the existing validator
        local val_addr=$(pchaind keys show "$wallet_name" --bech val -a --keyring-backend "$KEYRING" --home "$PCHAIN_HOME" 2>/dev/null || echo "")
        local node_pubkey=$(pchaind tendermint show-validator --home "$PCHAIN_HOME" 2>/dev/null | jq -r '.key' 2>/dev/null || echo "")
        
        set +e
        local controlled_validator=$(pchaind query staking validators --node="$GENESIS_NODE_RPC" --output json 2>/dev/null | \
            jq ".validators[] | select(.operator_address==\"$val_addr\" and .consensus_pubkey.value==\"$node_pubkey\")" 2>/dev/null)
        set -e
        
        if [ -n "$controlled_validator" ] && [ "$controlled_validator" != "null" ]; then
            echo
            echo -e "${BOLD}${GREEN}✅ Successfully connected to existing validator!${NC}"
            echo
            local val_moniker=$(echo "$controlled_validator" | jq -r '.description.moniker // "Unknown"')
            local val_status=$(echo "$controlled_validator" | jq -r '.status // "UNKNOWN"')
            local val_tokens=$(echo "$controlled_validator" | jq -r '.tokens // "0"')
            local val_tokens_push=$(format_balance "$val_tokens")
            
            echo -e "${BOLD}${CYAN}Your Validator:${NC}"
            echo "- Name: $val_moniker"
            echo "- Operator: $val_addr"
            echo "- Status: $val_status"
            echo "- Staked: $val_tokens_push PUSH"
            echo
            echo -e "${GREEN}No additional registration needed - you are controlling the existing validator!${NC}"
            echo
            echo "You can manage your validator with these commands:"
            echo "- Check status: ./push-node-manager status"
            echo "- View logs: ./push-node-manager logs"
            echo "- List validators: ./push-node-manager validators"
            echo
            exit 0
        else
            echo
            log_warning "The imported wallet does not control the validator with this node's keys!"
            echo "The validator might be controlled by a different wallet."
            echo "Proceeding to create a new validator..."
            echo
            using_existing_validator=false
        fi
    fi
    
    echo -e "${GREEN}Ready to proceed with validator registration${NC}"
    
    # Validator Registration
    echo
    if [ "$using_existing_validator" = true ]; then
        echo -e "${BOLD}${BLUE}Step 3: Validator Registration${NC}"
    else
        echo -e "${BOLD}${BLUE}Step 4: Validator Registration${NC}"
    fi
    echo "════════════════════════════════════════════════"
    echo
    
    read -p "Ready to register as a validator? (yes/no): " register_now
    if [[ "$register_now" =~ ^[Yy][Ee][Ss]$ ]]; then
        # Call register_validator and catch any errors
        set +e
        register_validator "$wallet_name" "$wallet_address"
        local reg_result=$?
        set -e
        
        if [ $reg_result -ne 0 ]; then
            echo
            log_warning "Validator registration encountered an issue"
            echo "You can try again later with: ./push-node-manager setup"
        fi
    else
        echo
        log_info "You can complete registration later by running:"
        echo -e "${CYAN}./push-node-manager setup${NC}"
        echo
        echo "Current status:"
        echo "- Wallet: $wallet_name"
        echo "- Address: $wallet_address"
        echo "- Balance: $balance_push PUSH"
    fi
    
    # Ensure we exit gracefully
    exit 0
}

# Simplified wait for funding
wait_for_funding() {
    local address="$1"
    local required_amount="$2"
    
    echo -e "${BLUE}Monitoring wallet... (Press Ctrl+C to cancel)${NC}"
    
    local spinner=('⣾' '⣽' '⣻' '⢿' '⡿' '⣟' '⣯' '⣷')
    local count=0
    local last_balance="0"
    
    while true; do
        local balance=$(check_balance_smart "$address")
        local balance_push=$(format_balance "$balance")
        
        # Check if balance increased
        if compare_amounts "$balance" "$last_balance" "-gt" && [ "$last_balance" != "0" ]; then
            echo -e "\n${GREEN}💰 Received $(format_balance $balance) PUSH!${NC}"
        fi
        last_balance="$balance"
        
        if compare_amounts "$balance" "$required_amount" "-ge"; then
            echo -e "\n${BOLD}${GREEN}✓ Wallet funded! Balance: $balance_push PUSH${NC}"
            sleep 2
            return 0
        fi
        
        printf "\r${BLUE}Balance: $balance_push PUSH ${spinner[$count]} ${NC}"
        count=$(( (count + 1) % 8 ))
        
        sleep 5
    done
}

# Run main function
main "$@"