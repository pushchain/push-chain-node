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

# Default values
PARTY_ID="${1:-party-1}"
P2P_PORT="${2:-39001}"
API_PORT="${3:-8081}"

# Cleanup function
cleanup() {
    echo -e "${YELLOW}Cleaning up previous runs...${NC}"
    pkill -f "tss.*-party=$PARTY_ID" || true
    pkill -f "tss node.*-party=$PARTY_ID" || true
    sleep 1
}

# Cleanup at start
cleanup

# Clear home directory for this node
HOME_DIR="/tmp/tss-$PARTY_ID"
if [ -d "$HOME_DIR" ]; then
    echo -e "${YELLOW}Clearing home directory: $HOME_DIR${NC}"
    rm -rf "$HOME_DIR"
fi
mkdir -p "$HOME_DIR"

echo -e "${GREEN}=== TSS Test Node ===${NC}"
echo "Party ID: $PARTY_ID"
echo "P2P Port: $P2P_PORT"
echo ""

# Always build binary to ensure we use the latest version
echo -e "${YELLOW}Building tss binary...${NC}"
mkdir -p build
if ! go build -o build/tss ./cmd/tss; then
    echo -e "${RED}Failed to build tss${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Binary built${NC}"
echo ""


# Start the node
echo -e "${BLUE}Starting node...${NC}"
./build/tss node \
    -party="$PARTY_ID" \
    -p2p-listen="/ip4/127.0.0.1/tcp/$P2P_PORT" \
    -home="$HOME_DIR" \
    2>&1
