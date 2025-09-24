#!/bin/bash
set -e

# Paths inside container
CSV_FILE="./testutils/upgrade-testing-utils/upgrade_list.csv"
STATE_FILE="./testutils/upgrade-testing-utils/.upgrade_index"

# If first run, set index = 2 (meaning line 2 = current commit done, upgrades start at line 3)
if [ ! -f "$STATE_FILE" ]; then
  echo 2 > "$STATE_FILE"
fi

CURRENT_INDEX=$(cat "$STATE_FILE")
NEXT_INDEX=$((CURRENT_INDEX + 1))

# Count total upgrades (excluding header row)
TOTAL_UPGRADES=$(($(wc -l < "$CSV_FILE") - 1))

echo "TOTAL_UPGRADES=$TOTAL_UPGRADES"
echo "CURRENT_INDEX=$CURRENT_INDEX"
echo "NEXT_INDEX=$NEXT_INDEX"

# If we’ve reached past last upgrade line
if [ "$NEXT_INDEX" -gt $((TOTAL_UPGRADES + 1)) ]; then
  echo "✅ All upgrades applied!"
  exit 0
fi

# Get the upgrade details from CSV
NEXT_UPGRADE=$(awk -F',' -v idx=$NEXT_INDEX 'NR==idx {print $1","$2}' "$CSV_FILE")

if [ -z "$NEXT_UPGRADE" ]; then
  echo "❌ No upgrade found at line $NEXT_INDEX."
  exit 1
fi

UPGRADE_NAME=$(echo "$NEXT_UPGRADE" | cut -d',' -f1 | xargs)
COMMIT_HASH=$(echo "$NEXT_UPGRADE" | cut -d',' -f2 | xargs)

echo ">>> Applying upgrade (line $NEXT_INDEX): $UPGRADE_NAME ($COMMIT_HASH)"

# Clone, checkout commit, build inside container
docker exec push-chain-node bash -c "
  set -e
  cd /app || mkdir -p /app && cd /app
  echo '>>> Removing existing repo (if any)'
  rm -rf push-chain-node

  echo '>>> Cloning push-chain-node repo'
  git clone https://github.com/push-protocol/push-chain-node.git push-chain-node

  cd push-chain-node
  
  echo '>>> Checking out commit $COMMIT_HASH'
  git checkout $COMMIT_HASH

  echo '>>> Building binary'
  CGO_ENABLED=0 make build

  if [ ! -f build/pchaind ]; then
    echo '❌ Build failed'
    exit 1
  fi

  echo '>>> Copying binary to /usr/local/bin'
  cp build/pchaind /usr/local/bin/pchaind
"

# Get current height inside container
HEIGHT=$(docker exec push-chain-node pchaind status | jq -r '.SyncInfo.latest_block_height')
TARGET_HEIGHT=$(( (HEIGHT/1000 + 1) * 1000 ))

echo ">>> Submitting governance proposal for $UPGRADE_NAME at height $TARGET_HEIGHT..."
docker exec push-chain-node pchaind tx upgrade software-upgrade "$UPGRADE_NAME" \
  --upgrade-height "$TARGET_HEIGHT" \
  --title "Test Upgrade $UPGRADE_NAME" \
  --summary "Automated upgrade test for $UPGRADE_NAME" \
  --from acc1 \
  --chain-id localchain_9000-1 \
  --deposit 1000000upc \
  --node tcp://localhost:26657 \
  --gas auto \
  --gas-adjustment 1.3 \
  --fees 310596000000000upc \
  --yes

# Wait for halt
echo ">>> Waiting for chain halt at height $TARGET_HEIGHT..."
while true; do
  CUR_HEIGHT=$(docker exec push-chain-node pchaind status | jq -r '.SyncInfo.latest_block_height')
  if [ "$CUR_HEIGHT" -ge "$TARGET_HEIGHT" ]; then
    echo "Chain halted at $CUR_HEIGHT."
    break
  fi
  sleep 5
done

# Restart node with upgraded binary
echo ">>> Restarting chain with upgraded binary..."
docker exec push-chain-node pkill pchaind || true
docker exec -d push-chain-node pchaind start

# Update state → store the line number we just processed
echo "$NEXT_INDEX" > "$STATE_FILE"
echo "✅ Upgrade $UPGRADE_NAME applied successfully!"
