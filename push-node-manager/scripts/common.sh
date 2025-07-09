#!/bin/bash

# Common functions and configuration for Push Chain node

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration file
NETWORKS_CONFIG="/scripts/networks.json"

# Default values
NETWORK="${NETWORK:-testnet}"
MONIKER="${MONIKER:-push-node}"
VALIDATOR_MODE="${VALIDATOR_MODE:-false}"
AUTO_INIT="${AUTO_INIT:-true}"
KEYRING="${KEYRING:-test}"
LOG_LEVEL="${LOG_LEVEL:-info}"
PRUNING="${PRUNING:-nothing}"
MINIMUM_GAS_PRICES="${MINIMUM_GAS_PRICES:-1000000000upc}"
PCHAIN_HOME="${PCHAIN_HOME:-/root/.pchain}"

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Safe jq function that handles missing keys
safe_jq() {
    local file="$1"
    local query="$2"
    local default="$3"
    
    if [ -f "$file" ]; then
        local result=$(jq -r "$query" "$file" 2>/dev/null)
        if [ "$result" = "null" ] || [ -z "$result" ]; then
            echo "$default"
        else
            echo "$result"
        fi
    else
        echo "$default"
    fi
}

# Load network configuration
load_network_config() {
    local network="$1"
    
    if [ ! -f "$NETWORKS_CONFIG" ]; then
        log_error "Networks configuration file not found: $NETWORKS_CONFIG"
        return 1
    fi
    
    # Check if network exists
    local network_exists=$(jq -r ".networks.$network" "$NETWORKS_CONFIG" 2>/dev/null)
    if [ "$network_exists" = "null" ]; then
        log_error "Network '$network' not found in configuration"
        return 1
    fi
    
    # Load network-specific configuration
    export CHAIN_ID=$(safe_jq "$NETWORKS_CONFIG" ".networks.$network.chain_id" "")
    export GENESIS_FILE=$(safe_jq "$NETWORKS_CONFIG" ".networks.$network.genesis_file" "")
    export MINIMUM_GAS_PRICES=$(safe_jq "$NETWORKS_CONFIG" ".networks.$network.minimum_gas_prices" "$MINIMUM_GAS_PRICES")
    export PERSISTENT_PEERS=$(safe_jq "$NETWORKS_CONFIG" ".networks.$network.persistent_peers" "")
    export SEEDS=$(safe_jq "$NETWORKS_CONFIG" ".networks.$network.seeds" "")
    
    # Load directories
    export PCHAIN_HOME=$(safe_jq "$NETWORKS_CONFIG" ".directories.home" "$PCHAIN_HOME")
    export CONFIG_DIR=$(safe_jq "$NETWORKS_CONFIG" ".directories.config" "$PCHAIN_HOME/config")
    export DATA_DIR=$(safe_jq "$NETWORKS_CONFIG" ".directories.data" "$PCHAIN_HOME/data")
    
    log_info "Network configuration loaded for: $network"
    return 0
}

# Validate configuration
validate_config() {
    local errors=0
    
    if [ -z "$CHAIN_ID" ]; then
        log_error "Chain ID is required"
        ((errors++))
    fi
    
    if [ -z "$MONIKER" ]; then
        log_error "Moniker is required"
        ((errors++))
    fi
    
    if [ ! -f "$GENESIS_FILE" ]; then
        log_error "Genesis file not found: $GENESIS_FILE"
        ((errors++))
    fi
    
    if [ $errors -gt 0 ]; then
        log_error "Configuration validation failed with $errors errors"
        return 1
    fi
    
    return 0
}

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Wait for service to be ready
wait_for_service() {
    local host="$1"
    local port="$2"
    local timeout="${3:-30}"
    local count=0
    
    log_info "Waiting for $host:$port to be ready..."
    
    while ! nc -z "$host" "$port" >/dev/null 2>&1; do
        if [ $count -ge $timeout ]; then
            log_error "Timeout waiting for $host:$port"
            return 1
        fi
        sleep 1
        ((count++))
    done
    
    log_success "$host:$port is ready"
    return 0
}

# Get node status
get_node_status() {
    if command_exists pchaind; then
        pchaind status --home "$PCHAIN_HOME" 2>/dev/null | jq -r '.sync_info.catching_up' 2>/dev/null || echo "unknown"
    else
        echo "unknown"
    fi
}

# Check if node is synced
is_node_synced() {
    local status=$(get_node_status)
    [ "$status" = "false" ]
} 