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

# Custom setup following user's exact sequence
setup_node_custom() {
    log_info "Setting up node with custom configuration sequence..."
    
    # Step 1: Run resetConfigs.sh with automatic input
    log_info "Running resetConfigs.sh..."
    if [ -f "/root/app/resetConfigs.sh" ]; then
        cd /root/app
        # Provide the expected input automatically for non-interactive mode
        echo "DELETEALL" | ./resetConfigs.sh
    else
        log_error "resetConfigs.sh not found in /root/app/"
        return 1
    fi
    
    # Step 2: Copy additional config files from deploy-config if they exist
    log_info "Copying additional config files from deploy-config..."
    if [ -f "/root/app/config-tmp/config.toml" ]; then
        log_info "Using config.toml from deploy-config"
        cp /root/app/config-tmp/config.toml ~/.pchain/config/config.toml
    fi
    
    if [ -f "/root/app/config-tmp/app.toml" ]; then
        log_info "Using app.toml from deploy-config"
        cp /root/app/config-tmp/app.toml ~/.pchain/config/app.toml
    fi
    
    if [ -f "/root/app/config-tmp/client.toml" ]; then
        log_info "Using client.toml from deploy-config"
        cp /root/app/config-tmp/client.toml ~/.pchain/config/client.toml
    fi
    
    # Step 3: Set moniker using toml_edit.py (override any existing moniker)
    log_info "Setting moniker to: $MONIKER"
    python3 /root/app/toml_edit.py \
        ~/.pchain/config/config.toml \
        "moniker" \
        "$MONIKER"
    
    # Step 4: Set up persistent peers
    log_info "Configuring persistent peers..."
    if [ -n "$PN1_URL" ]; then
        python3 /root/app/toml_edit.py \
            ~/.pchain/config/config.toml \
            "p2p.persistent_peers" \
            "$PN1_URL"
        log_info "Persistent peers set to: $PN1_URL"
    fi
    
    # Step 5: Set up validator state
    log_info "Setting up validator state..."
    echo '{"height":"0","round":0,"step":0}' > ~/.pchain/data/priv_validator_state.json
    chmod 600 ~/.pchain/config/priv_validator_key.json 2>/dev/null || true
    
    log_success "Custom node setup complete"
}

# Copy genesis file from deploy-config
setup_genesis_custom() {
    log_info "Setting up genesis file from deploy-config..."
    
    # Use genesis file from deploy-config/genesis.json
    if [ -f "/root/app/config-tmp/genesis.json" ]; then
        log_info "Using genesis file from deploy-config"
        cp /root/app/config-tmp/genesis.json ~/.pchain/config/genesis.json
        
        # Verify genesis file
        if jq -e . ~/.pchain/config/genesis.json >/dev/null 2>&1; then
            local chain_id=$(jq -r '.chain_id' ~/.pchain/config/genesis.json)
            local genesis_time=$(jq -r '.genesis_time' ~/.pchain/config/genesis.json)
            log_success "Genesis file setup complete for chain: $chain_id"
            log_info "Genesis time: $genesis_time"
            
            # Update CHAIN_ID environment variable if it's different
            if [ "$CHAIN_ID" != "$chain_id" ]; then
                log_info "Updating CHAIN_ID from $CHAIN_ID to $chain_id"
                export CHAIN_ID="$chain_id"
            fi
        else
            log_error "Genesis file is not valid JSON"
            return 1
        fi
    else
        log_error "Genesis file not found in deploy-config (/root/app/config-tmp/genesis.json)"
        return 1
    fi
}

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

# Use provided genesis file instead of downloading
setup_genesis() {
    log_info "Setting up genesis file for network: $NETWORK"
    
    # Check if genesis file exists in the expected location
    if [ ! -f "$GENESIS_FILE" ]; then
        log_error "Genesis file not found at: $GENESIS_FILE"
        return 1
    fi
    
    # Copy genesis file to config directory
    cp "$GENESIS_FILE" "$CONFIG_DIR/genesis.json"
    
    # Verify it's valid JSON
    if ! jq -e . "$CONFIG_DIR/genesis.json" >/dev/null 2>&1; then
        log_error "Genesis file is not valid JSON"
        return 1
    fi
    
    # Show genesis info
    local chain_id=$(jq -r '.chain_id' "$CONFIG_DIR/genesis.json")
    local app_hash=$(jq -r '.app_hash // "empty"' "$CONFIG_DIR/genesis.json")
    log_success "Genesis file setup complete"
    log_info "Chain ID: $chain_id"
    log_info "App Hash: $app_hash"
    
    return 0
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
    local peers="${PERSISTENT_PEERS}"
    if [ -n "$peers" ]; then
        python3 /scripts/toml_edit.py "$config_file" "p2p.persistent_peers" "$peers"
        log_info "Persistent peers configured: $peers"
    fi
    
    # Set seeds if provided
    if [ -n "$SEEDS" ]; then
        python3 /scripts/toml_edit.py "$config_file" "p2p.seeds" "$SEEDS"
        log_info "Seeds configured: $SEEDS"
    fi
    
    # For local single validator setup, disable seed mode
    if [ "$NETWORK" = "localchain" ]; then
        sed -i 's/seed_mode = true/seed_mode = false/' "$config_file"
        sed -i 's/pex = true/pex = false/' "$config_file"
        log_info "Disabled seed mode and pex for local validator"
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
    
    # Update client.toml
    local client_file="$CONFIG_DIR/client.toml"
    
    # Set chain-id in client.toml
    sed -i "s/chain-id = \"\"/chain-id = \"$CHAIN_ID\"/" "$client_file"
    
    # Set keyring backend to test (matching docker environment)
    sed -i 's/keyring-backend = "os"/keyring-backend = "test"/' "$client_file"
    
    log_info "Client configuration updated with chain-id: $CHAIN_ID"
    
    log_success "Node configuration complete"
}

# Start the node using the app scripts
start_node_custom() {
    log_info "Starting Push Chain node using custom scripts..."
    
    # Change to app directory
    cd /root/app
    
    # Stop any existing processes
    if [ -f "./stop.sh" ]; then
        log_info "Stopping any existing processes..."
        ./stop.sh || true
    fi
    
    # Start the node
    if [ -f "./start.sh" ]; then
        log_info "Starting node with start.sh..."
        ./start.sh
        
        # Wait a moment then show logs
        sleep 2
        if [ -f "./chain.log" ]; then
            log_info "Showing last 100 lines of chain.log..."
            tail -n 100 ./chain.log
        fi
        
        # Wait for full sync if script exists
        if [ -f "./waitFullSync.sh" ]; then
            log_info "Waiting for full sync..."
            ./waitFullSync.sh
        fi
    else
        log_error "start.sh not found, falling back to direct start"
        start_node
    fi
}

# Start the node (fallback method)
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
    
    # Handle commands
    case "${1:-start}" in
        start)
            # Use custom setup if app directory exists
            if [ -d "/root/app" ] && [ -f "/root/app/resetConfigs.sh" ]; then
                log_info "Using custom setup sequence..."
                setup_node_custom
                setup_genesis_custom
                start_node_custom
            else
                # Fallback to original setup
                log_info "Using standard setup sequence..."
                if [ "$AUTO_INIT" = "true" ]; then
                    initialize_node
                    setup_genesis
                    configure_node
                    
                    # Initialize validator state file (matching manual setup)
                    if [ ! -f "$DATA_DIR/priv_validator_state.json" ]; then
                        echo '{"height":"0","round":0,"step":0}' > "$DATA_DIR/priv_validator_state.json"
                        chmod 600 "$CONFIG_DIR/priv_validator_key.json" 2>/dev/null || true
                        log_info "Initialized validator state file"
                    fi
                fi
                start_node
            fi
            ;;
        init)
            if [ -d "/root/app" ] && [ -f "/root/app/resetConfigs.sh" ]; then
                setup_node_custom
                setup_genesis_custom
            else
                initialize_node
                setup_genesis
                configure_node
            fi
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