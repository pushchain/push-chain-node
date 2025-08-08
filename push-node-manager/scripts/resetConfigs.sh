#!/bin/bash
# Reset Push Chain configurations

set -e

PCHAIN_HOME="${PCHAIN_HOME:-/root/.pchain}"

echo "Resetting Push Chain configurations..."

# Initialize node with default chain ID
pchaind init temp-node --chain-id temp-chain --home "$PCHAIN_HOME" 2>/dev/null || true

# Clear data but keep config
rm -rf "$PCHAIN_HOME/data"
mkdir -p "$PCHAIN_HOME/data"

echo "Configuration reset complete"