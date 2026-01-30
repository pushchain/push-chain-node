#!/bin/bash
set -euo pipefail

# Load environment
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/env.sh"

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'
BOLD='\033[1m'

print_status() { echo -e "${CYAN}$1${NC}"; }
print_success() { echo -e "${GREEN}$1${NC}"; }
print_error() { echo -e "${RED}$1${NC}"; }
print_warning() { echo -e "${YELLOW}$1${NC}"; }

# Check binaries exist
check_binaries() {
    if [ ! -f "$PCHAIND_BIN" ]; then
        print_error "pchaind binary not found at $PCHAIND_BIN"
        print_status "Build it with: make build"
        exit 1
    fi
    if [ ! -f "$PUNIVERSALD_BIN" ]; then
        print_error "puniversald binary not found at $PUNIVERSALD_BIN"
        print_status "Build it with: make build"
        exit 1
    fi
    print_success "Binaries found"
}

# Create data directories
setup_dirs() {
    mkdir -p "$DATA_DIR"
    mkdir -p "$ACCOUNTS_DIR/keyring"
    for i in 1 2 3 4; do
        mkdir -p "$DATA_DIR/validator$i/.pchain"
        mkdir -p "$DATA_DIR/universal$i/.puniversal"
    done
    print_success "Data directories created"
}

# Generate accounts if not exists
generate_accounts() {
    if [ -f "$ACCOUNTS_DIR/genesis_accounts.json" ] && [ -f "$ACCOUNTS_DIR/validators.json" ] && [ -f "$ACCOUNTS_DIR/hotkeys.json" ]; then
        print_status "Accounts already generated, skipping..."
        return
    fi
    
    print_status "Generating accounts..."
    "$SCRIPT_DIR/scripts/generate-accounts.sh"
    print_success "Accounts generated"
}

# Start validator 1 (genesis)
start_validator1() {
    print_status "Starting validator 1 (genesis)..."
    "$SCRIPT_DIR/scripts/setup-genesis-auto.sh" &
    echo $! > "$DATA_DIR/validator1.pid"
    print_success "Validator 1 starting (PID: $(cat $DATA_DIR/validator1.pid))"
}

# Wait for genesis validator
wait_for_genesis() {
    print_status "Waiting for genesis validator to be ready..."
    local max_attempts=60
    local attempt=0
    while [ $attempt -lt $max_attempts ]; do
        if curl -s "http://localhost:26657/status" > /dev/null 2>&1; then
            local height=$(curl -s "http://localhost:26657/status" | jq -r '.result.sync_info.latest_block_height // "0"' 2>/dev/null || echo "0")
            if [ "$height" != "0" ] && [ "$height" != "null" ]; then
                print_success "Genesis validator ready! Block height: $height"
                return 0
            fi
        fi
        sleep 2
        attempt=$((attempt + 1))
        if [ $((attempt % 10)) -eq 0 ]; then
            print_status "Still waiting... (attempt $attempt/$max_attempts)"
        fi
    done
    print_error "Genesis validator not ready after $max_attempts attempts"
    return 1
}

# Main
main() {
    echo
    echo -e "${BOLD}${CYAN}Push Chain Local Native Setup${NC}"
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo
    
    check_binaries
    setup_dirs
    generate_accounts
    
    # Start genesis validator
    start_validator1
    
    # Wait for it to be ready
    if ! wait_for_genesis; then
        print_error "Failed to start genesis validator"
        exit 1
    fi
    
    print_success "Local network started!"
    echo
    print_status "Endpoints:"
    echo "  RPC:  http://localhost:26657"
    echo "  REST: http://localhost:1317"
    echo "  gRPC: localhost:9090"
    echo "  EVM:  http://localhost:8545"
    echo
    print_status "Logs: tail -f $DATA_DIR/validator1/validator.log"
    print_status "Stop:  ./stop.sh"
}

main "$@"
