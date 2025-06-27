#!/bin/bash
# Push Chain Validator Setup Script V2 - Improved Flow
# Enhanced with better fee handling, pre-flight checks, and clearer UX

set -e

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
DEFAULT_FEE_AMOUNT="0.1"  # 0.1 PUSH default fee
MIN_STAKE="1"  # Minimum 1 PUSH stake

# Check if running inside container
check_container "/scripts/setup-validator.sh"

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
    echo "$wallet_output"
    
    # Extract address
    local address=$(echo "$wallet_output" | grep -A1 "address:" | tail -1 | awk '{print $1}')
    if [ -z "$address" ]; then
        address=$(pchaind keys show "$wallet_name" -a --keyring-backend "$KEYRING" --home "$PCHAIN_HOME")
    fi
    
    echo
    echo -e "${BOLD}${GREEN}✓ Wallet created successfully!${NC}"
    echo -e "${BLUE}Address: ${address}${NC}"
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
    log_info "Importing existing wallet: $wallet_name"
    echo
    echo -e "${YELLOW}Enter your mnemonic phrase (will be hidden):${NC}"
    
    # Read mnemonic securely
    read -s -r mnemonic
    echo
    
    # Import wallet
    echo "$mnemonic" | pchaind keys add "$wallet_name" --recover --keyring-backend "$KEYRING" --home "$PCHAIN_HOME" 2>&1
    
    # Get address
    local address=$(pchaind keys show "$wallet_name" -a --keyring-backend "$KEYRING" --home "$PCHAIN_HOME")
    
    echo
    echo -e "${BOLD}${GREEN}✓ Wallet imported successfully!${NC}"
    echo -e "${BLUE}Address: ${address}${NC}"
    echo
    
    return 0
}

# Smart balance check - use genesis node if local is syncing
check_balance_smart() {
    local address="$1"
    local balance="0"
    
    # First try local node
    local sync_status=$(pchaind status --home "$PCHAIN_HOME" 2>/dev/null | jq -r '.sync_info.catching_up // true')
    
    if [ "$sync_status" = "false" ]; then
        # Local node is synced, use it
        balance=$(pchaind query bank balances "$address" --node tcp://localhost:26657 -o json 2>/dev/null | jq -r '.balances[] | select(.denom=="'$DENOM'") | .amount // "0"')
    else
        # Local node syncing, use genesis node
        log_info "Local node still syncing, checking balance via genesis node..."
        balance=$(pchaind query bank balances "$address" --node "$GENESIS_NODE_RPC" -o json 2>/dev/null | jq -r '.balances[] | select(.denom=="'$DENOM'") | .amount // "0"')
    fi
    
    if [ -z "$balance" ] || [ "$balance" = "null" ]; then
        balance="0"
    fi
    
    echo "$balance"
}

# Convert upc to PUSH
format_balance() {
    local upc_amount="$1"
    if [ "$upc_amount" = "0" ]; then
        echo "0"
    else
        # Convert from upc to PUSH (divide by 10^18)
        echo "scale=6; $upc_amount / 1000000000000000000" | bc
    fi
}

# Convert push address to EVM format
push_to_evm_address() {
    local push_addr="$1"
    # Use pchaind to convert address
    local evm_addr=$(pchaind debug addr "$push_addr" 2>/dev/null | grep "hex" | awk '{print "0x"$2}')
    echo "$evm_addr"
}

# Calculate required funds (stake + fees + buffer)
calculate_required_funds() {
    local stake_amount="$1"
    local fee_amount="${2:-$DEFAULT_FEE_AMOUNT}"
    
    # Add 10% buffer for safety
    local total=$(echo "scale=6; $stake_amount + $fee_amount + 0.1" | bc)
    echo "$total"
}

# Show funding requirements clearly
show_funding_requirements() {
    local stake_amount="$1"
    local fee_amount="${2:-$DEFAULT_FEE_AMOUNT}"
    local total_required=$(calculate_required_funds "$stake_amount" "$fee_amount")
    
    echo -e "${BOLD}${BLUE}💰 Funding Requirements${NC}"
    echo "════════════════════════════════════════════════"
    echo -e "Stake amount:    ${GREEN}$stake_amount PUSH${NC}"
    echo -e "Transaction fee: ${YELLOW}~$fee_amount PUSH${NC}"
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
    local required_upc=$(echo "$required_amount * 1000000000000000000" | bc | cut -d'.' -f1)
    
    echo -n "✓ Balance check: "
    if [ "$balance" -ge "$required_upc" ]; then
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
    if curl -s --connect-timeout 5 "$GENESIS_NODE_RPC/status" >/dev/null 2>&1; then
        echo -e "${GREEN}Connected${NC}"
    else
        echo -e "${RED}Cannot reach network${NC}"
        all_good=false
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
    local max_stake=$(echo "$balance_push - 0.2" | bc)  # Reserve 0.2 PUSH for fees
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
        
        if (( $(echo "$stake_amount < $MIN_STAKE" | bc -l) )); then
            echo -e "${RED}Stake must be at least $MIN_STAKE PUSH${NC}"
            continue
        fi
        
        if (( $(echo "$stake_amount > $max_stake" | bc -l) )); then
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
    
    # Commission rates with better defaults
    echo
    echo -e "${BLUE}Commission Settings:${NC}"
    read -p "Commission rate (default 10%): " commission_rate
    commission_rate="${commission_rate:-0.1}"
    
    read -p "Max commission rate (default 20%): " commission_max_rate
    commission_max_rate="${commission_max_rate:-0.2}"
    
    read -p "Max commission change rate (default 1%): " commission_max_change_rate
    commission_max_change_rate="${commission_max_change_rate:-0.01}"
    
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
    local fee_amount="${DEFAULT_FEE_AMOUNT}${ONE_PUSH}"
    
    # Create validator with retry logic
    log_info "Creating validator transaction..."
    
    local attempt=1
    local max_attempts=3
    local tx_hash=""
    
    while [ $attempt -le $max_attempts ]; do
        echo -e "${BLUE}Attempt $attempt of $max_attempts...${NC}"
        
        local tx_result=$(pchaind tx staking create-validator /tmp/register-validator.json \
            --chain-id "$CHAIN_ID" \
            --fees "$fee_amount" \
            --gas "$DEFAULT_GAS" \
            --from "$wallet_name" \
            --node="$GENESIS_NODE_RPC" \
            --keyring-backend "$KEYRING" \
            --yes \
            --output json 2>&1)
        
        tx_hash=$(echo "$tx_result" | jq -r '.txhash // empty' 2>/dev/null)
        
        if [ -n "$tx_hash" ]; then
            log_success "Transaction submitted! Hash: $tx_hash"
            break
        else
            log_warning "Transaction failed on attempt $attempt"
            
            # If insufficient fee error, reduce fee and retry
            if echo "$tx_result" | grep -q "insufficient fee"; then
                fee_amount=$(echo "$fee_amount * 0.5" | bc)  # Halve the fee
                log_info "Retrying with lower fee..."
            fi
            
            ((attempt++))
            sleep 2
        fi
    done
    
    if [ -z "$tx_hash" ]; then
        log_error "Failed to create validator after $max_attempts attempts"
        echo "$tx_result"
        return 1
    fi
    
    # Wait for confirmation
    echo -e "${BLUE}Waiting for confirmation...${NC}"
    sleep 10
    
    # Check transaction with detailed error info
    local tx_query=$(pchaind query tx "$tx_hash" --chain-id "$CHAIN_ID" --node="$GENESIS_NODE_RPC" --output json 2>/dev/null || echo "{}")
    local tx_code=$(echo "$tx_query" | jq -r '.code // "-1"')
    
    if [ "$tx_code" = "0" ]; then
        echo
        echo -e "${BOLD}${GREEN}🎉 Congratulations! Your validator is now active!${NC}"
        echo
        
        # Get validator operator address
        local val_addr=$(pchaind keys show $wallet_name --bech val -a --keyring-backend $KEYRING --home $PCHAIN_HOME)
        
        echo -e "${BLUE}Verifying validator status...${NC}"
        sleep 5
        
        # Query validator
        local validator_info=$(pchaind query staking validators --node="$GENESIS_NODE_RPC" --output json 2>/dev/null | jq ".validators[] | select(.description.moniker==\"$validator_name\")" || echo "{}")
        
        if [ -n "$validator_info" ] && [ "$validator_info" != "{}" ]; then
            local val_status=$(echo "$validator_info" | jq -r '.status // "UNKNOWN"')
            local tokens=$(echo "$validator_info" | jq -r '.tokens // "0"')
            local tokens_push=$(format_balance "$tokens")
            
            echo -e "${GREEN}✓ Validator found in active set!${NC}"
            echo -e "Status: $val_status"
            echo -e "Staked: $tokens_push PUSH"
        fi
        
        echo
        echo -e "${BOLD}${BLUE}📊 Validator Details:${NC}"
        echo "- Name: $validator_name"
        echo "- Operator Address: $val_addr"
        echo "- Explorer: ${EXPLORER}validators/${val_addr}"
        echo
        echo -e "${BLUE}Next steps:${NC}"
        echo "1. Monitor your validator: ./push-validator status"
        echo "2. Check balance: ./push-validator balance"
        echo "3. View logs: ./push-validator logs"
        echo
        
        # Show remaining balance
        local new_balance=$(check_balance_smart "$wallet_address")
        local new_balance_push=$(format_balance "$new_balance")
        echo -e "${GREEN}Remaining balance: $new_balance_push PUSH${NC}"
        
        return 0
    else
        log_error "Transaction failed! Code: $tx_code"
        
        # Extract and display user-friendly error
        local raw_log=$(echo "$tx_query" | jq -r '.raw_log // "Unknown error"')
        
        if echo "$raw_log" | grep -q "insufficient funds"; then
            echo -e "${RED}Error: Insufficient funds for staking + fees${NC}"
            echo "Please ensure you have enough PUSH to cover:"
            echo "- Stake amount: $stake_amount PUSH"
            echo "- Transaction fees: ~$DEFAULT_FEE_AMOUNT PUSH"
        else
            echo -e "${RED}Error details:${NC}"
            echo "$raw_log" | head -5
        fi
        
        return 1
    fi
}

# Main setup flow
main() {
    print_banner
    
    # Step 1: Wallet Setup
    echo -e "${BOLD}${BLUE}Step 1: Wallet Setup${NC}"
    echo "════════════════════════════════════════════════"
    echo
    
    local wallet_name="validator"
    
    # Check for existing wallet
    if check_wallet "$wallet_name"; then
        local address=$(pchaind keys show "$wallet_name" -a --keyring-backend "$KEYRING" --home "$PCHAIN_HOME")
        log_info "Found existing wallet: $wallet_name"
        echo -e "${BLUE}Address: $address${NC}"
        echo
        
        read -p "Use existing wallet? (yes/no): " use_existing
        if [[ ! "$use_existing" =~ ^[Yy][Ee][Ss]$ ]]; then
            echo
            echo "1) Create new wallet"
            echo "2) Import existing wallet"
            read -p "Choose option (1/2): " wallet_option
            
            case "$wallet_option" in
                1)
                    read -p "Enter new wallet name: " new_wallet_name
                    wallet_name="${new_wallet_name:-validator-new}"
                    create_wallet "$wallet_name"
                    ;;
                2)
                    read -p "Enter wallet name to import: " import_wallet_name
                    wallet_name="${import_wallet_name:-validator-imported}"
                    import_wallet "$wallet_name"
                    ;;
                *)
                    log_error "Invalid option"
                    exit 1
                    ;;
            esac
        fi
    else
        echo "No existing wallet found."
        echo
        echo "1) Create new wallet"
        echo "2) Import existing wallet"
        read -p "Choose option (1/2): " wallet_option
        
        case "$wallet_option" in
            1)
                create_wallet "$wallet_name"
                ;;
            2)
                import_wallet "$wallet_name"
                ;;
            *)
                log_error "Invalid option"
                exit 1
                ;;
        esac
    fi
    
    # Get wallet address
    local wallet_address=$(pchaind keys show "$wallet_name" -a --keyring-backend "$KEYRING" --home "$PCHAIN_HOME")
    
    # Step 2: Check Balance with smart detection
    echo
    echo -e "${BOLD}${BLUE}Step 2: Checking Wallet Balance${NC}"
    echo "════════════════════════════════════════════════"
    echo
    
    log_info "Checking current balance..."
    local balance=$(check_balance_smart "$wallet_address")
    local balance_push=$(format_balance "$balance")
    echo -e "${BLUE}Current balance: ${BOLD}$balance_push PUSH${NC}"
    
    # Calculate minimum required (1 PUSH stake + 0.3 PUSH for fees/buffer)
    local min_required_push="1.3"
    local min_required_upc=$(echo "$min_required_push * 1000000000000000000" | bc | cut -d'.' -f1)
    
    if [ "$balance" -lt "$min_required_upc" ]; then
        echo
        log_warning "Insufficient balance for validator registration"
        
        # Show clear requirements
        show_funding_requirements "$MIN_STAKE"
        
        # Convert to EVM address for faucet
        local evm_address=$(push_to_evm_address "$wallet_address")
        
        # Show faucet instructions
        show_faucet_instructions "$wallet_address" "$evm_address" "$min_required_push"
        
        echo -e "${BOLD}${BLUE}Funding Options:${NC}"
        echo "1) Open web browser to request tokens"
        echo "2) I've already requested tokens, start monitoring"
        echo "3) Skip funding (exit)"
        echo
        read -p "Choose option (1-3): " funding_option
        
        case "$funding_option" in
            1)
                # Try to open browser
                if command -v open >/dev/null 2>&1; then
                    open "$FAUCET"
                elif command -v xdg-open >/dev/null 2>&1; then
                    xdg-open "$FAUCET"
                else
                    echo -e "${YELLOW}Please open your browser and visit: $FAUCET${NC}"
                fi
                echo
                read -p "Press Enter after requesting tokens to start monitoring..."
                wait_for_funding "$wallet_address" "$min_required_upc"
                ;;
            2)
                wait_for_funding "$wallet_address" "$min_required_upc"
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
    else
        log_success "✅ Wallet has sufficient balance!"
        echo -e "${GREEN}Ready to proceed with validator registration${NC}"
    fi
    
    # Step 3: Validator Registration
    echo
    echo -e "${BOLD}${BLUE}Step 3: Validator Registration${NC}"
    echo "════════════════════════════════════════════════"
    echo
    
    read -p "Ready to register as a validator? (yes/no): " register_now
    if [[ "$register_now" =~ ^[Yy][Ee][Ss]$ ]]; then
        register_validator "$wallet_name" "$wallet_address"
    else
        echo
        log_info "You can complete registration later by running:"
        echo -e "${CYAN}./push-validator setup${NC}"
        echo
        echo "Current status:"
        echo "- Wallet: $wallet_name"
        echo "- Address: $wallet_address"
        echo "- Balance: $balance_push PUSH"
    fi
}

# Enhanced wait for funding with better UX
wait_for_funding() {
    local address="$1"
    local required_amount="$2"
    
    log_info "Monitoring wallet for incoming tokens..."
    echo -e "${BLUE}Required: $(format_balance $required_amount) PUSH${NC}"
    echo
    
    local spinner=('⣾' '⣽' '⣻' '⢿' '⡿' '⣟' '⣯' '⣷')
    local count=0
    local check_count=0
    local last_balance="0"
    
    echo -e "${CYAN}Press Ctrl+C to cancel monitoring${NC}"
    echo
    
    while true; do
        local balance=$(check_balance_smart "$address")
        
        # Check if balance increased
        if [ "$balance" -gt "$last_balance" ] && [ "$last_balance" != "0" ]; then
            echo -e "\n${GREEN}💰 Tokens received! $(format_balance $balance) PUSH${NC}"
        fi
        last_balance="$balance"
        
        if [ "$balance" -ge "$required_amount" ]; then
            echo -e "\n${BOLD}${GREEN}✓ Wallet successfully funded!${NC}"
            echo -e "${GREEN}Balance: $(format_balance $balance) PUSH${NC}"
            return 0
        fi
        
        # Show status every 10 checks (~50 seconds)
        if [ $((check_count % 10)) -eq 0 ] && [ $check_count -gt 0 ]; then
            echo -e "\n${BLUE}Still waiting... Current balance: $(format_balance $balance) PUSH${NC}"
        fi
        
        printf "\r${BLUE}Checking balance... ${spinner[$count]} ${NC}"
        count=$(( (count + 1) % 8 ))
        check_count=$((check_count + 1))
        
        sleep 5
    done
}

# Run main function
main "$@"