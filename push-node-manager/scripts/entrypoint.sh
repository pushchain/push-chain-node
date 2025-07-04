#!/bin/bash
set -e

# Source common functions
source /scripts/common.sh

# Load configuration from networks.json
if [ -f "$NETWORKS_CONFIG" ]; then
    # Extract directories
    PCHAIN_HOME=$(safe_jq "$NETWORKS_CONFIG" '.directories.home' "/root/.pchain")
    CONFIG_DIR=$(safe_jq "$NETWORKS_CONFIG" '.directories.config' "$PCHAIN_HOME/config")
    DATA_DIR=$(safe_jq "$NETWORKS_CONFIG" '.directories.data' "$PCHAIN_HOME/data")
fi

# Initialize node if needed
initialize_node() {
    if [ -f "$CONFIG_DIR/genesis.json" ]; then
        log_info "Node already initialized, skipping initialization..."
        return 0
    fi

    log_info "Setting up Push Chain node..."
    
    # Clean any existing configs but not the mounted volume
    rm -rf "$PCHAIN_HOME/config" "$PCHAIN_HOME/data"
    mkdir -p "$PCHAIN_HOME/config" "$PCHAIN_HOME/data"
    
    # Run resetConfigs.sh equivalent
    log_info "Initializing node configuration..."
    
    # Get chain ID from network config (already loaded)
    if [ -z "$CHAIN_ID" ]; then
        log_error "No chain ID configured for network: $NETWORK"
        return 1
    fi
    
    # Initialize with moniker and chain ID
    pchaind init "$MONIKER" --chain-id "$CHAIN_ID" --home "$PCHAIN_HOME"
    
    log_success "Node initialized with moniker: $MONIKER"
}

# Download genesis file from network
download_genesis() {
    log_info "Downloading genesis file for network: $NETWORK"
    
    # Backup existing genesis if any
    if [ -f "$CONFIG_DIR/genesis.json" ]; then
        cp "$CONFIG_DIR/genesis.json" "$CONFIG_DIR/genesis.json.backup"
    fi
    
    # Get genesis node RPC from config - ensure it uses http
    local genesis_rpc="${GENESIS_NODE_RPC:-34.57.209.0:26657}"
    # Fix protocol if it's tcp://
    genesis_rpc="${genesis_rpc/tcp:\/\//http://}"
    log_info "Downloading from: $genesis_rpc/genesis"
    
    # Download genesis with timeout and retries
    local max_attempts=3
    local attempt=1
    
    while [ $attempt -le $max_attempts ]; do
        log_info "Download attempt $attempt of $max_attempts..."
        
        # Download full response
        if curl -s --connect-timeout 30 --max-time 120 "$genesis_rpc/genesis" > /tmp/genesis-response.json 2>&1; then
            # Extract genesis part
            if jq -r '.result.genesis' /tmp/genesis-response.json > "$CONFIG_DIR/genesis.json" 2>&1; then
                # Verify it's valid JSON
                if jq -e . "$CONFIG_DIR/genesis.json" >/dev/null 2>&1; then
                    log_success "Genesis file downloaded successfully"
                    
                    # Show genesis info
                    local chain_id=$(jq -r '.chain_id' "$CONFIG_DIR/genesis.json")
                    local app_hash=$(jq -r '.app_hash // "empty"' "$CONFIG_DIR/genesis.json")
                    log_info "Chain ID: $chain_id"
                    log_info "App Hash: $app_hash"
                    
                    # If app_hash is empty, we might need to use state sync
                    if [ "$app_hash" = "" ] || [ "$app_hash" = "empty" ]; then
                        log_warning "Genesis has empty app_hash - this is normal for fresh chains"
                        log_info "If sync fails, state sync might be needed for existing chains"
                    fi
                    
                    rm -f /tmp/genesis-response.json
                    return 0
                else
                    log_error "Downloaded genesis is not valid JSON"
                fi
            else
                log_error "Failed to extract genesis from response"
            fi
        else
            log_error "Failed to download genesis from $genesis_rpc"
        fi
        
        ((attempt++))
        if [ $attempt -le $max_attempts ]; then
            log_info "Retrying in 5 seconds..."
            sleep 5
        fi
    done
    
    log_error "Failed to download genesis after $max_attempts attempts"
    return 1
}


# Configure node settings
configure_node() {
    log_info "Configuring node settings..."
    
    # Load network configuration from common.sh
    load_network_config "$NETWORK" || return 1
    
    # Update config.toml
    local config_file="$CONFIG_DIR/config.toml"
    
    # Set moniker using toml_edit.py (matching manual setup)
    python3 /scripts/toml_edit.py "$config_file" "moniker" "$MONIKER"
    
    # Set persistent peers - prefer auto-configured or use provided
    local peers="${PERSISTENT_PEERS:-$GENESIS_NODE_URL}"
    if [ -n "$peers" ]; then
        python3 /scripts/toml_edit.py "$config_file" "p2p.persistent_peers" "$peers"
        log_info "Persistent peers configured: $peers"
    fi
    
    # Set seeds if provided
    if [ -n "$SEEDS" ]; then
        python3 /scripts/toml_edit.py "$config_file" "p2p.seeds" "$SEEDS"
        log_info "Seeds configured: $SEEDS"
    fi
    
    # Set external address if provided
    if [ -n "$EXTERNAL_IP" ] && [ "$EXTERNAL_IP" != "auto" ]; then
        local p2p_port=$(jq -r '.ports.p2p // "26656"' "$NETWORKS_CONFIG")
        sed -i "s/external_address = \"\"/external_address = \"$EXTERNAL_IP:$p2p_port\"/" "$config_file"
        log_info "External address set to: $EXTERNAL_IP:$p2p_port"
    fi
    
    # Enable prometheus metrics
    sed -i 's/prometheus = false/prometheus = true/' "$config_file"
    
    # Bind RPC to all interfaces
    sed -i 's/laddr = "tcp:\/\/127.0.0.1:26657"/laddr = "tcp:\/\/0.0.0.0:26657"/' "$config_file"
    
    # Set log level
    sed -i "s/log_level = \"info\"/log_level = \"$LOG_LEVEL\"/" "$config_file"
    
    # Performance tuning
    sed -i 's/size = 5000/size = 10000/' "$config_file"  # Increase mempool size
    sed -i 's/cache_size = 10000/cache_size = 100000/' "$config_file"  # Increase cache
    
    # Update app.toml
    local app_file="$CONFIG_DIR/app.toml"
    
    # Set minimum gas prices
    sed -i "s/minimum-gas-prices = \"\"/minimum-gas-prices = \"$MINIMUM_GAS_PRICES\"/" "$app_file"
    
    # Enable API and bind to all interfaces
    sed -i 's/enable = false/enable = true/' "$app_file"
    sed -i 's/swagger = false/swagger = true/' "$app_file"
    sed -i 's/address = "tcp:\/\/localhost:1317"/address = "tcp:\/\/0.0.0.0:1317"/' "$app_file"
    
    # Enable gRPC and bind to all interfaces
    sed -i '/\[grpc\]/,/\[/ s/enable = false/enable = true/' "$app_file"
    sed -i '/\[grpc\]/,/\[/ s/address = "localhost:9090"/address = "0.0.0.0:9090"/' "$app_file"
    
    # Enable JSON-RPC (for EVM compatibility) and bind to all interfaces
    sed -i '/\[json-rpc\]/,/\[/ s/enable = false/enable = true/' "$app_file"
    sed -i '/\[json-rpc\]/,/\[/ s/address = "127.0.0.1:8545"/address = "0.0.0.0:8545"/' "$app_file"
    
    # Set pruning
    case "$PRUNING" in
        "nothing")
            sed -i 's/pruning = "default"/pruning = "nothing"/' "$app_file"
            ;;
        "everything")
            sed -i 's/pruning = "default"/pruning = "everything"/' "$app_file"
            ;;
        *)
            # Keep default
            ;;
    esac
    
    log_success "Node configuration complete"
}

# Create or import validator key - DEPRECATED
# Now handled by the setup wizard to ensure users see the mnemonic
# setup_keys() {
#     log_info "Setting up validator keys..."
#     
#     # Check if keys already exist
#     if pchaind keys show validator --keyring-backend "$KEYRING" --home "$PCHAIN_HOME" >/dev/null 2>&1; then
#         log_info "Validator key already exists"
#         return 0
#     fi
#     
#     # Check if we should import a key
#     if [ -n "$VALIDATOR_KEY_MNEMONIC" ]; then
#         log_info "Importing validator key from mnemonic..."
#         echo "$VALIDATOR_KEY_MNEMONIC" | pchaind keys add validator --recover --keyring-backend "$KEYRING" --home "$PCHAIN_HOME"
#     else
#         log_info "Creating new validator key..."
#         pchaind keys add validator --keyring-backend "$KEYRING" --home "$PCHAIN_HOME"
#         log_warning "⚠️  IMPORTANT: Save the mnemonic phrase shown above!"
#     fi
#     
#     log_success "Validator key setup complete"
# }

# Start the node
start_node() {
    log_info "Starting Push Chain node..."
    
    # Check if we should start in validator mode
    if [ "$VALIDATOR_MODE" = "true" ]; then
        log_info "Starting in validator mode"
    else
        log_info "Starting in full node mode"
    fi
    
    # Start the node with all necessary flags (matching deploy script)
    exec pchaind start \
        --home "$PCHAIN_HOME" \
        --pruning="${PRUNING:-nothing}" \
        --minimum-gas-prices="$MINIMUM_GAS_PRICES" \
        --rpc.laddr="tcp://0.0.0.0:26657" \
        --json-rpc.api=eth,txpool,personal,net,debug,web3,trace \
        --json-rpc.address="0.0.0.0:8545" \
        --json-rpc.ws-address="0.0.0.0:8546" \
        --chain-id="$CHAIN_ID" \
        --trace \
        --evm.tracer=json
}

# Main function
main() {
    log_info "Push Chain Validator Node Starting..."
    
    # Load network configuration first
    load_network_config "$NETWORK" || exit 1
    
    # Validate configuration
    validate_config || exit 1
    
    local network_name=$(safe_jq "$NETWORKS_CONFIG" ".networks.$NETWORK.name" "$NETWORK")
    log_info "Network: $network_name"
    log_info "Moniker: $MONIKER"
    log_info "Chain ID: $CHAIN_ID"
    log_info "Gas Prices: $MINIMUM_GAS_PRICES"
    
    # Auto-initialization if enabled
    if [ "$AUTO_INIT" = "true" ]; then
        initialize_node
        download_genesis
        configure_node
        
        # Don't auto-create keys - let the setup wizard handle it
        # This ensures users see and can save the mnemonic phrase
        # if [ "$VALIDATOR_MODE" = "true" ]; then
        #     setup_keys
        # fi
        
        # Initialize validator state file (matching manual setup)
        if [ ! -f "$DATA_DIR/priv_validator_state.json" ]; then
            echo '{"height":"0","round":0,"step":0}' > "$DATA_DIR/priv_validator_state.json"
            chmod 600 "$CONFIG_DIR/priv_validator_key.json" 2>/dev/null || true
            log_info "Initialized validator state file"
        fi
    fi
    
    # Handle commands
    case "${1:-start}" in
        start)
            start_node
            ;;
        init)
            initialize_node
            download_genesis
            configure_node
            ;;
        keys)
            shift
            pchaind keys "$@" --keyring-backend "$KEYRING" --home "$PCHAIN_HOME"
            ;;
        *)
            # Pass through to pchaind
            exec pchaind "$@" --home "$PCHAIN_HOME"
            ;;
    esac
}

# Run main function
main "$@"