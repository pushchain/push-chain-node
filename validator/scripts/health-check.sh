#!/bin/bash
# Health check script for Push Chain validator

set -e

# Check if pchaind process is running
if ! pgrep pchaind > /dev/null; then
    echo "pchaind process not running"
    exit 1
fi

# Try to get node status
NODE_STATUS=$(pchaind status --home /root/.pchain 2>/dev/null || echo "{}")

# Check if we got valid JSON response
if ! echo "$NODE_STATUS" | jq -e . >/dev/null 2>&1; then
    echo "Invalid node status response"
    exit 1
fi

# Extract sync info
CATCHING_UP=$(echo "$NODE_STATUS" | jq -r '.sync_info.catching_up // true')
LATEST_HEIGHT=$(echo "$NODE_STATUS" | jq -r '.sync_info.latest_block_height // "0"')

# If node is synced, it's healthy
if [ "$CATCHING_UP" = "false" ]; then
    echo "Node is synced at height $LATEST_HEIGHT"
    exit 0
fi

# If still syncing, check if making progress
if [ "$LATEST_HEIGHT" != "0" ] && [ "$LATEST_HEIGHT" != "null" ]; then
    echo "Node is syncing, current height: $LATEST_HEIGHT"
    exit 0
fi

# Node appears to be stuck
echo "Node is not making progress"
exit 1