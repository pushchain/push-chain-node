#!/bin/bash
# Common functions and utilities for Push Chain validator scripts

# Colors for output
export GREEN='\033[0;32m'
export BLUE='\033[0;34m'
export RED='\033[0;31m'
export YELLOW='\033[0;33m'
export PURPLE='\033[0;35m'
export CYAN='\033[0;36m'
export LIGHT_BLUE='\033[1;36m'
export BOLD='\033[1m'
export NC='\033[0m'

# Configuration paths
export NETWORKS_CONFIG="${NETWORKS_CONFIG:-/configs/networks.json}"
export PCHAIN_HOME="${PCHAIN_HOME:-/root/.pchain}"
export CONFIG_DIR="$PCHAIN_HOME/config"
export DATA_DIR="$PCHAIN_HOME/data"

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

# Print status message
print_status() {
    echo -e "${BLUE}$1${NC}"
}

print_success() {
    echo -e "${GREEN}$1${NC}"
}

print_error() {
    echo -e "${RED}$1${NC}"
}

print_warning() {
    echo -e "${YELLOW}$1${NC}"
}

# Load network configuration from networks.json
load_network_config() {
    local network="${1:-${NETWORK:-testnet}}"
    
    if [ ! -f "$NETWORKS_CONFIG" ]; then
        log_error "Network configuration file not found: $NETWORKS_CONFIG"
        return 1
    fi
    
    # Check if network is enabled
    local network_enabled=$(jq -r ".networks.$network.enabled // false" "$NETWORKS_CONFIG")
    if [ "$network_enabled" != "true" ]; then
        log_error "Network '$network' is not enabled yet"
        return 1
    fi
    
    # Export all network-specific values
    export CHAIN_ID=$(jq -r ".networks.$network.chain_id // empty" "$NETWORKS_CONFIG")
    export GENESIS_URL=$(jq -r ".networks.$network.genesis_url // empty" "$NETWORKS_CONFIG")
    export MINIMUM_GAS_PRICES=$(jq -r ".networks.$network.minimum_gas_prices // empty" "$NETWORKS_CONFIG")
    export DENOM=$(jq -r ".networks.$network.denom // \"upc\"" "$NETWORKS_CONFIG")
    export EXPLORER=$(jq -r ".networks.$network.explorer // empty" "$NETWORKS_CONFIG")
    export FAUCET=$(jq -r ".networks.$network.faucet // empty" "$NETWORKS_CONFIG")
    
    # Load genesis node info
    local genesis_node_id=$(jq -r ".networks.$network.genesis_node.id // empty" "$NETWORKS_CONFIG")
    local genesis_node_ip=$(jq -r ".networks.$network.genesis_node.ip // empty" "$NETWORKS_CONFIG")
    local genesis_node_p2p=$(jq -r ".networks.$network.genesis_node.p2p_port // empty" "$NETWORKS_CONFIG")
    
    if [ -n "$genesis_node_id" ] && [ -n "$genesis_node_ip" ]; then
        export GENESIS_NODE_URL="${genesis_node_id}@${genesis_node_ip}:${genesis_node_p2p}"
    fi
    
    # Load persistent peers
    export PERSISTENT_PEERS=$(jq -r ".networks.$network.persistent_peers[]? // empty" "$NETWORKS_CONFIG" | tr '\n' ',' | sed 's/,$//')
    
    # Load seeds
    export SEEDS=$(jq -r ".networks.$network.seeds[]? // empty" "$NETWORKS_CONFIG" | tr '\n' ',' | sed 's/,$//')
    
    # Get RPC endpoint
    local rpc=$(jq -r ".networks.$network.rpc_endpoints[0] // empty" "$NETWORKS_CONFIG" 2>/dev/null)
    if [ -n "$rpc" ]; then
        export GENESIS_NODE_RPC="$rpc"
    fi
    
    log_info "Loaded configuration for network: $network"
    return 0
}

# Validate required configuration
validate_config() {
    local errors=0
    
    # Check required values
    if [ -z "$CHAIN_ID" ]; then
        log_error "CHAIN_ID is not configured"
        ((errors++))
    fi
    
    if [ -z "$MINIMUM_GAS_PRICES" ]; then
        log_error "MINIMUM_GAS_PRICES is not configured"
        ((errors++))
    fi
    
    if [ -z "$MONIKER" ]; then
        log_error "MONIKER is not set"
        ((errors++))
    fi
    
    if [ $errors -gt 0 ]; then
        log_error "Configuration validation failed with $errors errors"
        return 1
    fi
    
    return 0
}

# Check if running inside container
check_container() {
    if [ ! -f /.dockerenv ]; then
        log_error "This script should be run inside the validator container"
        echo "Use: ./push-node-manager shell"
        echo "Then run: $1"
        exit 1
    fi
}

# Safe JSON parsing with error handling
safe_jq() {
    local json_file="$1"
    local query="$2"
    local default="${3:-}"
    
    if [ ! -f "$json_file" ]; then
        echo "$default"
        return 1
    fi
    
    local result=$(jq -r "$query" "$json_file" 2>/dev/null || echo "$default")
    if [ "$result" = "null" ] || [ -z "$result" ]; then
        echo "$default"
    else
        echo "$result"
    fi
}

# Verify binary with checksum
verify_binary() {
    local binary_path="$1"
    local expected_checksum="$2"
    
    if [ -z "$expected_checksum" ]; then
        log_warning "No checksum provided for binary verification"
        return 0
    fi
    
    local actual_checksum=$(sha256sum "$binary_path" | cut -d' ' -f1)
    
    if [ "$actual_checksum" != "$expected_checksum" ]; then
        log_error "Binary checksum verification failed!"
        log_error "Expected: $expected_checksum"
        log_error "Actual: $actual_checksum"
        return 1
    fi
    
    log_success "Binary checksum verified"
    return 0
}

# Download file with progress
download_with_progress() {
    local url="$1"
    local output="$2"
    local description="${3:-file}"
    
    log_info "Downloading $description..."
    
    if command -v wget >/dev/null 2>&1; then
        wget --progress=bar:force -O "$output" "$url" 2>&1 | \
            grep --line-buffered "%" | \
            sed -u -e "s,.*\([0-9]\+%\).*,\1,"
    else
        curl -L --progress-bar -o "$output" "$url"
    fi
    
    if [ $? -eq 0 ]; then
        log_success "$description downloaded successfully"
        return 0
    else
        log_error "Failed to download $description"
        return 1
    fi
}

# Validate user input
validate_input() {
    local input="$1"
    local type="$2"
    local min="${3:-}"
    local max="${4:-}"
    
    case "$type" in
        "number")
            if ! [[ "$input" =~ ^[0-9]+$ ]]; then
                return 1
            fi
            if [ -n "$min" ] && [ "$input" -lt "$min" ]; then
                return 1
            fi
            if [ -n "$max" ] && [ "$input" -gt "$max" ]; then
                return 1
            fi
            ;;
        "address")
            if ! [[ "$input" =~ ^push1[a-z0-9]{38}$ ]]; then
                return 1
            fi
            ;;
        "moniker")
            if [ -z "$input" ] || [ ${#input} -gt 70 ]; then
                return 1
            fi
            ;;
        *)
            return 1
            ;;
    esac
    
    return 0
}

# Export functions for use in other scripts
export -f log_info log_success log_error log_warning
export -f print_status print_success print_error print_warning
export -f load_network_config validate_config check_container
export -f safe_jq verify_binary download_with_progress validate_input