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
    
    # Clean up database file for this node
    DB_FILE="/tmp/tss-$PARTY_ID.db"
    if [ -f "$DB_FILE" ]; then
        echo -e "${YELLOW}Removing database file: $DB_FILE${NC}"
        rm -f "$DB_FILE"
    fi
    
    # Clean up home directory for this node
    HOME_DIR="/tmp/tss-$PARTY_ID"
    if [ -d "$HOME_DIR" ]; then
        echo -e "${YELLOW}Removing home directory: $HOME_DIR${NC}"
        rm -rf "$HOME_DIR"
    fi
}

# Cleanup at start
cleanup

# Create fresh home directory
HOME_DIR="/tmp/tss-$PARTY_ID"
mkdir -p "$HOME_DIR"
echo -e "${GREEN}Using home directory: $HOME_DIR${NC}"

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


# Check for existing peer IDs file
PEER_IDS_FILE="/tmp/tss-peer-ids.json"
PEER_IDS_FLAG=""

# If peer IDs file exists, use it
if [ -f "$PEER_IDS_FILE" ]; then
    # Read peer IDs from file and format as flag
    PEER_IDS=$(cat "$PEER_IDS_FILE" | jq -r 'to_entries | map("\(.key):\(.value)") | join(",")' 2>/dev/null || echo "")
    if [ -n "$PEER_IDS" ]; then
        PEER_IDS_FLAG="-peer-ids=$PEER_IDS"
        echo -e "${GREEN}Using peer IDs from $PEER_IDS_FILE${NC}"
    fi
fi

# Start the node and capture peer ID
echo -e "${BLUE}Starting node...${NC}"
echo -e "${YELLOW}Note: Peer ID will be logged when node starts.${NC}"
echo -e "${YELLOW}To share peer IDs, copy them to $PEER_IDS_FILE in format:${NC}"
echo -e "${YELLOW}  {\"party-1\": \"peer-id-1\", \"party-2\": \"peer-id-2\"}${NC}"
echo ""

./build/tss node \
    -party="$PARTY_ID" \
    -p2p-listen="/ip4/127.0.0.1/tcp/$P2P_PORT" \
    -home="$HOME_DIR" \
    $PEER_IDS_FLAG \
    2>&1 | tee "/tmp/tss-$PARTY_ID.log"
