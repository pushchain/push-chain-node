#!/bin/bash
# Wait for node to fully sync

echo "Waiting for node to sync..."

while true; do
    STATUS=$(pchaind status 2>/dev/null || echo "{}")
    CATCHING_UP=$(echo "$STATUS" | jq -r '.SyncInfo.catching_up // true')
    HEIGHT=$(echo "$STATUS" | jq -r '.SyncInfo.latest_block_height // "0"')
    
    if [ "$CATCHING_UP" = "false" ]; then
        echo "✅ Node is fully synced at height: $HEIGHT"
        break
    else
        echo "⏳ Syncing... Current height: $HEIGHT"
        sleep 10
    fi
done