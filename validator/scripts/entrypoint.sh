#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m'

# Configuration - loaded from networks.json
NETWORKS_CONFIG="/configs/networks.json"

# Load configuration
if [ -f "$NETWORKS_CONFIG" ]; then
    # Extract directories
    PCHAIN_HOME=$(jq -r '.directories.home' "$NETWORKS_CONFIG")
    CONFIG_DIR=$(jq -r '.directories.config' "$NETWORKS_CONFIG")
    DATA_DIR=$(jq -r '.directories.data' "$NETWORKS_CONFIG")
else
    # Fallback defaults
    PCHAIN_HOME="${PCHAIN_HOME:-/root/.pchain}"
    CONFIG_DIR="$PCHAIN_HOME/config"
    DATA_DIR="$PCHAIN_HOME/data"
fi

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
    
    # Get chain ID from network config
    local chain_id="${CHAIN_ID:-$(jq -r ".networks.$NETWORK.chain_id // empty" "$NETWORKS_CONFIG")}"
    
    if [ -z "$chain_id" ]; then
        log_error "No chain ID configured for network: $NETWORK"
        return 1
    fi
    
    # Initialize with moniker and chain ID
    pchaind init "$MONIKER" --chain-id "$chain_id" --home "$PCHAIN_HOME"
    
    log_success "Node initialized with moniker: $MONIKER"
}

# Download and verify genesis file
download_genesis() {
    if [ -z "$GENESIS_URL" ]; then
        log_error "No genesis URL configured for network: $NETWORK"
        return 1
    fi
    
    log_info "Downloading genesis file from $GENESIS_URL..."
    
    # Backup existing genesis if any
    if [ -f "$CONFIG_DIR/genesis.json" ]; then
        cp "$CONFIG_DIR/genesis.json" "$CONFIG_DIR/genesis.json.backup"
    fi
    
    # Handle different genesis URL types
    if [[ "$GENESIS_URL" == file://* ]]; then
        # Local file copy
        local source_file="${GENESIS_URL#file://}"
        if [ -f "$source_file" ]; then
            cp "$source_file" "$CONFIG_DIR/genesis.json"
            log_success "Genesis file copied from local source"
        else
            log_error "Genesis file not found at $source_file"
            return 1
        fi
    else
        # Download from URL
        if curl -sSL "$GENESIS_URL" -o "$CONFIG_DIR/genesis.json"; then
            log_success "Genesis file downloaded successfully"
        else
            log_error "Failed to download genesis file"
            return 1
        fi
    fi
    
    # Verify genesis file
    if jq -e . "$CONFIG_DIR/genesis.json" >/dev/null 2>&1; then
        log_success "Genesis file is valid JSON"
    else
        log_error "Downloaded genesis file is not valid JSON"
        return 1
    fi
}

# Load network configuration from networks.json
load_network_config() {
    if [ ! -f "$NETWORKS_CONFIG" ]; then
        log_error "Network configuration file not found: $NETWORKS_CONFIG"
        return 1
    fi
    
    # Check if network is enabled
    local network_enabled=$(jq -r ".networks.$NETWORK.enabled // false" "$NETWORKS_CONFIG")
    if [ "$network_enabled" != "true" ]; then
        log_error "Network '$NETWORK' is not enabled yet"
        return 1
    fi
    
    # Load all network-specific values
    CHAIN_ID=$(jq -r ".networks.$NETWORK.chain_id // empty" "$NETWORKS_CONFIG")
    GENESIS_URL=$(jq -r ".networks.$NETWORK.genesis_url // empty" "$NETWORKS_CONFIG")
    MINIMUM_GAS_PRICES=$(jq -r ".networks.$NETWORK.minimum_gas_prices // empty" "$NETWORKS_CONFIG")
    
    # Load genesis node info
    local genesis_node_id=$(jq -r ".networks.$NETWORK.genesis_node.id // empty" "$NETWORKS_CONFIG")
    local genesis_node_ip=$(jq -r ".networks.$NETWORK.genesis_node.ip // empty" "$NETWORKS_CONFIG")
    local genesis_node_p2p=$(jq -r ".networks.$NETWORK.genesis_node.p2p_port // empty" "$NETWORKS_CONFIG")
    
    if [ -n "$genesis_node_id" ] && [ -n "$genesis_node_ip" ]; then
        GENESIS_NODE_URL="${genesis_node_id}@${genesis_node_ip}:${genesis_node_p2p}"
    fi
    
    # Load persistent peers
    PERSISTENT_PEERS=$(jq -r ".networks.$NETWORK.persistent_peers[]? // empty" "$NETWORKS_CONFIG" | tr '\n' ',' | sed 's/,$//')
    
    # Load seeds
    SEEDS=$(jq -r ".networks.$NETWORK.seeds[]? // empty" "$NETWORKS_CONFIG" | tr '\n' ',' | sed 's/,$//')
    
    log_info "Loaded configuration for network: $NETWORK"
}

# Configure node settings
configure_node() {
    log_info "Configuring node settings..."
    
    # Load network configuration
    load_network_config
    
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

# Create or import validator key
setup_keys() {
    log_info "Setting up validator keys..."
    
    # Check if keys already exist
    if pchaind keys show validator --keyring-backend "$KEYRING" --home "$PCHAIN_HOME" >/dev/null 2>&1; then
        log_info "Validator key already exists"
        return 0
    fi
    
    # Check if we should import a key
    if [ -n "$VALIDATOR_KEY_MNEMONIC" ]; then
        log_info "Importing validator key from mnemonic..."
        echo "$VALIDATOR_KEY_MNEMONIC" | pchaind keys add validator --recover --keyring-backend "$KEYRING" --home "$PCHAIN_HOME"
    else
        log_info "Creating new validator key..."
        pchaind keys add validator --keyring-backend "$KEYRING" --home "$PCHAIN_HOME"
        log_warning "⚠️  IMPORTANT: Save the mnemonic phrase shown above!"
    fi
    
    log_success "Validator key setup complete"
}

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
    load_network_config || exit 1
    
    local network_name=$(jq -r ".networks.$NETWORK.name // '$NETWORK'" "$NETWORKS_CONFIG")
    log_info "Network: $network_name"
    log_info "Moniker: $MONIKER"
    log_info "Chain ID: $CHAIN_ID"
    
    # Auto-initialization if enabled
    if [ "$AUTO_INIT" = "true" ]; then
        initialize_node
        download_genesis
        configure_node
        
        # Setup keys if in validator mode
        if [ "$VALIDATOR_MODE" = "true" ]; then
            setup_keys
        fi
        
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