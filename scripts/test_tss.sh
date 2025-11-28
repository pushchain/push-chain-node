#!/bin/bash

# TSS Test Script
# Runs a single TSS node for testing
# Run this script multiple times in different terminals to test with multiple nodes

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$PROJECT_ROOT"

# All 3 arguments are required
if [ $# -lt 3 ]; then
    echo -e "${RED}Error: All 3 arguments are required${NC}"
    echo "Usage: $0 <validator-address> <p2p-port> <private-key-hex>"
    echo "Example: $0 pushvaloper1... 39001 30B0D912700C3DF94F4743F440D1613F7EA67E1CEF32C73B925DB6CD7F1A1544"
    exit 1
fi

VALIDATOR_ADDRESS="$1"
P2P_PORT="$2"
PRIVATE_KEY="$3"

# Cleanup function
cleanup() {
    echo -e "${YELLOW}Cleaning up previous runs...${NC}"
    
    if [ -n "$VALIDATOR_ADDRESS" ]; then
        pkill -f "tss.*-validator-address=$VALIDATOR_ADDRESS" || true
        pkill -f "tss node.*-validator-address=$VALIDATOR_ADDRESS" || true
    fi
    sleep 1
    
    # Clean up database file for this node (using sanitized validator address)
    SANITIZED=$(echo "$VALIDATOR_ADDRESS" | sed 's/:/_/g' | sed 's/\//_/g')
    DB_FILE="/tmp/tss-$SANITIZED.db"
    if [ -f "$DB_FILE" ]; then
        echo -e "${YELLOW}Removing database file: $DB_FILE${NC}"
        rm -f "$DB_FILE"
    fi
    
    # Clean up home directory for this node
    HOME_DIR="/tmp/tss-$SANITIZED"
    if [ -d "$HOME_DIR" ]; then
        echo -e "${YELLOW}Removing home directory: $HOME_DIR${NC}"
        rm -rf "$HOME_DIR"
    fi
}

# Cleanup at start
cleanup

# Create fresh home directory
SANITIZED=$(echo "$VALIDATOR_ADDRESS" | sed 's/:/_/g' | sed 's/\//_/g')
HOME_DIR="/tmp/tss-$SANITIZED"
mkdir -p "$HOME_DIR"
echo -e "${GREEN}Using home directory: $HOME_DIR${NC}"

# Always build binary to ensure we use the latest version
echo -e "${YELLOW}Building tss binary...${NC}"
mkdir -p build
if ! go build -o build/tss ./cmd/tss; then
    echo -e "${RED}Failed to build tss${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Binary built${NC}"
echo ""


# Note: Peer IDs are now deterministic from hardcoded keys, so no file-based discovery needed
# The -peer-ids flag is optional and can override if needed
PEER_IDS_FLAG=""

# Start the node
echo -e "${BLUE}Starting node...${NC}"
echo ""

# Build command with required private key
CMD="./build/tss node -validator-address=\"$VALIDATOR_ADDRESS\" -p2p-listen=\"/ip4/127.0.0.1/tcp/$P2P_PORT\" -home=\"$HOME_DIR\" -private-key=\"$PRIVATE_KEY\""
if [ -n "$PEER_IDS_FLAG" ]; then
    CMD="$CMD $PEER_IDS_FLAG"
fi

eval "$CMD" 2>&1 | tee "/tmp/tss-$SANITIZED.log"
