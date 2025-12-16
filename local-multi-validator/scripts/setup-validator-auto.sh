#!/bin/bash
set -eu
shopt -s expand_aliases

# ---------------------------
# === CONFIGURATION ===
# ---------------------------

CHAIN_ID=${CHAIN_ID:-"localchain_9000-1"}
MONIKER=${MONIKER:-"validator"}
VALIDATOR_ID=${VALIDATOR_ID:-"2"}
KEYRING="test"
KEYALGO="eth_secp256k1"
DENOM="upc"

# Paths
BINARY="/usr/bin/pchaind"
HOME_DIR="/root/.pchain"

# Ports (from environment)
RPC_PORT=${RPC_PORT:-26657}
REST_PORT=${REST_PORT:-1317}
GRPC_PORT=${GRPC_PORT:-9090}
GRPC_WEB_PORT=$((GRPC_PORT + 1))
P2P_PORT=${P2P_PORT:-26656}
PROFF_PORT=$((RPC_PORT + 3))
GENESIS_PEER=${GENESIS_PEER:-"core-validator-1:26656"}

# Validator stake amount
VALIDATOR_STAKE="100000000000000000000000"  # 100k * 10^18

# ---------------------------
# === LOAD PRE-GENERATED ACCOUNTS ===
# ---------------------------

TMP_DIR="/tmp/push-accounts"
GENESIS_ACCOUNTS_FILE="$TMP_DIR/genesis_accounts.json"
VALIDATORS_FILE="$TMP_DIR/validators.json"

# Check if account files exist
if [ ! -f "$GENESIS_ACCOUNTS_FILE" ] || [ ! -f "$VALIDATORS_FILE" ]; then
  echo "❌ Account files not found. Please run generate-accounts.sh first:"
  echo "   /opt/scripts/generate-accounts.sh"
  exit 1
fi

echo "📋 Loading pre-generated accounts for validator $VALIDATOR_ID..."

# Load validator mnemonic for this specific validator
VALIDATOR_MNEMONIC=$(jq -r ".[] | select(.id == $VALIDATOR_ID) | .mnemonic" "$VALIDATORS_FILE")
VALIDATOR_KEY="validator-$VALIDATOR_ID"

if [ "$VALIDATOR_MNEMONIC" = "null" ] || [ -z "$VALIDATOR_MNEMONIC" ]; then
  echo "❌ No validator found with ID $VALIDATOR_ID"
  exit 1
fi

echo "🔑 Using validator: $VALIDATOR_KEY"

# Genesis funding account (will fund this validator) - use validator-specific genesis account
FUNDING_INDEX=$((VALIDATOR_ID - 1))  # Convert to 0-based index
FUNDING_MNEMONIC=$(jq -r ".[$FUNDING_INDEX].mnemonic" "$GENESIS_ACCOUNTS_FILE")
FUNDING_KEY="genesis-acc-$VALIDATOR_ID"
FUNDING_AMOUNT="200000000000000000000000"  # 200k * 10^18 (enough for staking + fees)

# ---------------------------
# === WAIT FOR GENESIS VALIDATOR ===
# ---------------------------

echo "⏳ Waiting for genesis validator to be ready..."
GENESIS_RPC="http://core-validator-1:26657"

# Wait for genesis validator to be accessible
max_attempts=60
attempt=0
while [ $attempt -lt $max_attempts ]; do
  if curl -s "$GENESIS_RPC/status" > /dev/null 2>&1; then
    echo "✅ Genesis validator is ready!"
    break
  fi
  echo "Waiting for genesis validator... (attempt $((attempt + 1))/$max_attempts)"
  sleep 5
  attempt=$((attempt + 1))
done

if [ $attempt -eq $max_attempts ]; then
  echo "❌ Genesis validator not ready after ${max_attempts} attempts"
  exit 1
fi

# ---------------------------
# === CHECK IF ALREADY INITIALIZED AND SYNCED ===
# ---------------------------

# Check if node is already synced (has data and can respond to RPC)
if [ -f "$HOME_DIR/data/priv_validator_state.json" ]; then
  # Check if the state file has valid content (not just initial state)
  HEIGHT=$(cat "$HOME_DIR/data/priv_validator_state.json" | jq -r '.height // "0"' 2>/dev/null || echo "0")
  if [ "$HEIGHT" != "0" ] && [ "$HEIGHT" != "\"0\"" ]; then
    echo "✅ Node already initialized with block height $HEIGHT, starting node..."

    # Start node in background so we can check UV registration
    $BINARY start \
      --home "$HOME_DIR" \
      --pruning=nothing \
      --minimum-gas-prices="1000000000${DENOM}" \
      --rpc.laddr="tcp://0.0.0.0:${RPC_PORT}" \
      --json-rpc.api=eth,txpool,personal,net,debug,web3 \
      --chain-id="$CHAIN_ID" &

    NODE_PID=$!

    # Wait for node to be ready
    echo "⏳ Waiting for node to be ready..."
    sleep 10

    # Check if UV registration is needed (for validators 2, 3, and 4)
    if [ "$VALIDATOR_ID" = "2" ] || [ "$VALIDATOR_ID" = "3" ] || [ "$VALIDATOR_ID" = "4" ]; then
      echo "🔍 Checking universal validator registration status..."

      GENESIS_RPC="http://core-validator-1:26657"

      # Pre-computed peer_ids
      case $VALIDATOR_ID in
        2)
          PEER_ID="12D3KooWJWoaqZhDaoEFshF7Rh1bpY9ohihFhzcW6d69Lr2NASuq"
          TSS_PORT=39001
          ;;
        3)
          PEER_ID="12D3KooWRndVhVZPCiQwHBBBdg769GyrPUW13zxwqQyf9r3ANaba"
          TSS_PORT=39002
          ;;
        4)
          PEER_ID="12D3KooWPT98FXMfDQYavZm66EeVjTqP9Nnehn1gyaydqV8L8BQw"
          TSS_PORT=39003
          ;;
      esac

      # Check if already registered by querying for our peer_id
      UV_CHECK=$($BINARY query uvalidator all-universal-validators --node="$GENESIS_RPC" --output json 2>/dev/null || echo "{}")

      if echo "$UV_CHECK" | grep -q "$PEER_ID"; then
        echo "✅ Universal-validator-$VALIDATOR_ID already registered"
      else
        echo "📝 Universal-validator-$VALIDATOR_ID not registered, registering now..."

        # Get valoper address
        VALOPER_ADDR=$($BINARY keys show validator-$VALIDATOR_ID --bech val -a --keyring-backend "$KEYRING" --home "$HOME_DIR" 2>/dev/null)

        if [ -n "$VALOPER_ADDR" ]; then
          MULTI_ADDR="/dns4/universal-validator-$VALIDATOR_ID/tcp/$TSS_PORT"
          NETWORK_JSON="{\"peer_id\": \"$PEER_ID\", \"multi_addrs\": [\"$MULTI_ADDR\"]}"

          # Import genesis account for signing
          GENESIS_ACCOUNTS_FILE="/tmp/push-accounts/genesis_accounts.json"
          if [ -f "$GENESIS_ACCOUNTS_FILE" ]; then
            GENESIS_ACC_MNEMONIC=$(jq -r '.[0].mnemonic' "$GENESIS_ACCOUNTS_FILE")
            echo "$GENESIS_ACC_MNEMONIC" | $BINARY keys add genesis-acc-1 --recover --keyring-backend "$KEYRING" --home "$HOME_DIR" 2>/dev/null || true
          fi

          # Retry loop for registration (handles sequence mismatch race condition)
          MAX_RETRIES=5
          RETRY_COUNT=0
          REGISTERED=false

          while [ "$RETRY_COUNT" -lt "$MAX_RETRIES" ] && [ "$REGISTERED" = "false" ]; do
            RETRY_COUNT=$((RETRY_COUNT + 1))

            # Stagger validators to reduce race conditions (validator 2 waits 2s, validator 3 waits 4s)
            if [ "$RETRY_COUNT" -eq 1 ]; then
              STAGGER_DELAY=$((VALIDATOR_ID * 2))
              echo "⏳ Waiting ${STAGGER_DELAY}s to stagger registration..."
              sleep $STAGGER_DELAY
            fi

            echo "📤 Registering universal-validator-$VALIDATOR_ID (attempt $RETRY_COUNT/$MAX_RETRIES)..."
            RESULT=$($BINARY tx uvalidator add-universal-validator \
              --core-validator-address "$VALOPER_ADDR" \
              --network "$NETWORK_JSON" \
              --from genesis-acc-1 \
              --chain-id "$CHAIN_ID" \
              --keyring-backend "$KEYRING" \
              --home "$HOME_DIR" \
              --node="$GENESIS_RPC" \
              --fees 1000000000000000upc \
              --yes \
              --output json 2>&1 || echo "{}")

            if echo "$RESULT" | grep -q '"txhash"'; then
              TX_HASH=$(echo "$RESULT" | jq -r '.txhash' 2>/dev/null)
              echo "✅ Universal-validator-$VALIDATOR_ID registered! TX: $TX_HASH"
              REGISTERED=true
            elif echo "$RESULT" | grep -q "sequence mismatch"; then
              echo "⚠️ Sequence mismatch, retrying in 3s..."
              sleep 3
            elif echo "$RESULT" | grep -q "already registered\|already exists"; then
              echo "✅ Universal-validator-$VALIDATOR_ID already registered"
              REGISTERED=true
            else
              echo "⚠️ Registration attempt failed: $(echo "$RESULT" | head -1)"
              sleep 2
            fi
          done

          if [ "$REGISTERED" = "false" ]; then
            echo "❌ Registration TX failed after $MAX_RETRIES attempts"
          fi
        else
          echo "⚠️ Could not get valoper address"
        fi
      fi
    fi

    echo "🔄 Node running as validator..."
    wait $NODE_PID
    exit 0
  fi
fi

# ---------------------------
# === INITIALIZATION ===
# ---------------------------

echo "🚨 Starting validator $VALIDATOR_ID setup..."
echo "Chain ID: $CHAIN_ID"
echo "Moniker: $MONIKER"

# Clean start
rm -rf "$HOME_DIR"/* "$HOME_DIR"/.[!.]* "$HOME_DIR"/..?* 2>/dev/null || true

echo "🧱 Initializing chain..."
$BINARY init "$MONIKER" --chain-id "$CHAIN_ID" --default-denom "$DENOM" --home "$HOME_DIR"

# ---------------------------
# === FETCH GENESIS ===
# ---------------------------

echo "🌍 Getting genesis.json from shared volume..."

# Wait for genesis file to be available in shared volume
# (genesis validator copies it there after setup)
SHARED_GENESIS="/tmp/push-accounts/genesis.json"
max_genesis_attempts=60
genesis_attempt=0
while [ $genesis_attempt -lt $max_genesis_attempts ]; do
  if [ -f "$SHARED_GENESIS" ]; then
    echo "✅ Found genesis in shared volume!"
    break
  fi
  echo "Waiting for genesis file... (attempt $((genesis_attempt + 1))/$max_genesis_attempts)"
  sleep 5
  genesis_attempt=$((genesis_attempt + 1))
done

if [ ! -f "$SHARED_GENESIS" ]; then
  echo "❌ Genesis file not found in shared volume after ${max_genesis_attempts} attempts"
  exit 1
fi

# Copy genesis from shared volume (ensures exact same file as genesis validator)
cp "$SHARED_GENESIS" "$HOME_DIR/config/genesis.json"
echo "📋 Genesis copied from shared volume"

# Debug: Output checksum for comparison with genesis validator
echo "📊 GENESIS CHECKSUM (validator-$VALIDATOR_ID):"
echo "  Shared: $(sha256sum $SHARED_GENESIS)"
echo "  Local:  $(sha256sum $HOME_DIR/config/genesis.json)"

echo "🔍 Getting genesis validator node ID..."
GENESIS_NODE_ID=$(curl -s "$GENESIS_RPC/status" | jq -r '.result.node_info.id')
PERSISTENT_PEER="$GENESIS_NODE_ID@$GENESIS_PEER"

echo "🔗 Setting persistent peer: $PERSISTENT_PEER"
sed -i -e "s/^persistent_peers *=.*/persistent_peers = \"$PERSISTENT_PEER\"/" "$HOME_DIR/config/config.toml"

# ---------------------------
# === CREATE VALIDATOR KEY ===
# ---------------------------

echo "🔐 Creating validator key with pre-defined mnemonic..."
echo "$VALIDATOR_MNEMONIC" | $BINARY keys add "$VALIDATOR_KEY" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOME_DIR" --recover

VALIDATOR_ADDR=$($BINARY keys show "$VALIDATOR_KEY" -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
echo "Validator address: $VALIDATOR_ADDR"

# ---------------------------
# === SKIP FUNDING (ALREADY IN GENESIS) ===
# ---------------------------

echo "💰 Validator-$VALIDATOR_ID already funded in genesis state"
echo "   Skipping runtime funding from genesis accounts"

# Verify balance (should be from genesis)
sleep 5
BALANCE=$($BINARY query bank balances "$VALIDATOR_ADDR" --chain-id "$CHAIN_ID" --home "$HOME_DIR" --node="$GENESIS_RPC" --output json | jq -r ".balances[0].amount // \"0\"")
echo "💰 Validator balance (from genesis): $BALANCE $DENOM"

if [ "$BALANCE" = "0" ] || [ -z "$BALANCE" ]; then
  echo "❌ No balance found. Validator should be funded in genesis state."
  exit 1
fi

echo "✅ Validator has balance from genesis!"

# ---------------------------
# === CONFIG PATCHING ===
# ---------------------------

echo "⚙️ Configuring network settings..."

# RPC configuration
sed -i -e "s/laddr = \"tcp:\/\/127.0.0.1:26657\"/laddr = \"tcp:\/\/0.0.0.0:${RPC_PORT}\"/g" $HOME_DIR/config/config.toml
sed -i -e 's/cors_allowed_origins = \[\]/cors_allowed_origins = \["\*"\]/g' $HOME_DIR/config/config.toml

# REST configuration
sed -i -e "s/address = \"tcp:\/\/localhost:1317\"/address = \"tcp:\/\/0.0.0.0:${REST_PORT}\"/g" $HOME_DIR/config/app.toml
sed -i -e 's/enable = false/enable = true/g' $HOME_DIR/config/app.toml
sed -i -e 's/enabled-unsafe-cors = false/enabled-unsafe-cors = true/g' $HOME_DIR/config/app.toml

# P2P configuration
sed -i -e "s/laddr = \"tcp:\/\/0.0.0.0:26656\"/laddr = \"tcp:\/\/0.0.0.0:${P2P_PORT}\"/g" $HOME_DIR/config/config.toml

# GRPC configuration
sed -i -e "s/address = \"localhost:9090\"/address = \"0.0.0.0:${GRPC_PORT}\"/g" $HOME_DIR/config/app.toml
sed -i -e "s/address = \"localhost:9091\"/address = \"0.0.0.0:${GRPC_WEB_PORT}\"/g" $HOME_DIR/config/app.toml

# Profiling
sed -i -e "s/pprof_laddr = \"localhost:6060\"/pprof_laddr = \"localhost:${PROFF_PORT}\"/g" $HOME_DIR/config/config.toml

# Block time
sed -i -e 's/timeout_commit = "5s"/timeout_commit = "1s"/g' $HOME_DIR/config/config.toml

# ---------------------------
# === START NODE AND AUTO-PROMOTE ===
# ---------------------------

echo "🚀 Starting validator node..."

# Start node in background
$BINARY start \
  --home "$HOME_DIR" \
  --pruning=nothing \
  --minimum-gas-prices="1000000000${DENOM}" \
  --rpc.laddr="tcp://0.0.0.0:${RPC_PORT}" \
  --json-rpc.api=eth,txpool,personal,net,debug,web3 \
  --chain-id="$CHAIN_ID" &

NODE_PID=$!

# Wait for node to sync
echo "⏳ Waiting for node to sync..."
max_sync_attempts=120
sync_attempt=0

while [ $sync_attempt -lt $max_sync_attempts ]; do
  if curl -s "http://localhost:${RPC_PORT}/status" > /dev/null 2>&1; then
    CATCHING_UP=$(curl -s "http://localhost:${RPC_PORT}/status" | jq -r '.result.sync_info.catching_up')
    if [ "$CATCHING_UP" = "false" ]; then
      echo "✅ Node is synced!"
      break
    fi
  fi
  echo "Syncing... (attempt $((sync_attempt + 1))/$max_sync_attempts)"
  sleep 10
  sync_attempt=$((sync_attempt + 1))
done

if [ $sync_attempt -eq $max_sync_attempts ]; then
  echo "❌ Node failed to sync after ${max_sync_attempts} attempts"
  kill $NODE_PID
  exit 1
fi

# ---------------------------
# === CREATE VALIDATOR (RUNTIME STAKING) ===
# ---------------------------

echo "📝 Creating validator-$VALIDATOR_ID with stake..."

# Wait for chain to produce blocks
sleep 10

# Get validator's valoper address
VALOPER_ADDR=$($BINARY keys show "$VALIDATOR_KEY" --bech val -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
echo "Validator operator address: $VALOPER_ADDR"

# Check if already bonded
VALIDATOR_STATUS=$($BINARY query staking validator "$VALOPER_ADDR" \
  --node="$GENESIS_RPC" \
  --output json 2>/dev/null | jq -r '.status' || echo "NOT_FOUND")

if [ "$VALIDATOR_STATUS" = "BOND_STATUS_BONDED" ]; then
  echo "✅ Validator-$VALIDATOR_ID is already bonded!"
  VALIDATOR_TOKENS=$($BINARY query staking validator "$VALOPER_ADDR" \
    --node="$GENESIS_RPC" \
    --output json 2>/dev/null | jq -r '.tokens' || echo "0")
  echo "   Bonded tokens: $VALIDATOR_TOKENS"
else
  echo "📤 Submitting create-validator transaction..."

  # Get the validator pubkey
  PUBKEY=$($BINARY tendermint show-validator --home "$HOME_DIR")

  # Create validator.json file (required by new CLI syntax)
  VALIDATOR_JSON="$HOME_DIR/validator.json"
  cat > "$VALIDATOR_JSON" <<EOF
{
  "pubkey": $PUBKEY,
  "amount": "${VALIDATOR_STAKE}${DENOM}",
  "moniker": "validator-$VALIDATOR_ID",
  "identity": "",
  "website": "",
  "security": "",
  "details": "Validator $VALIDATOR_ID",
  "commission-rate": "0.10",
  "commission-max-rate": "0.20",
  "commission-max-change-rate": "0.01",
  "min-self-delegation": "1"
}
EOF

  echo "📋 Validator config:"
  cat "$VALIDATOR_JSON"

  # Ensure file is flushed to disk
  sync

  # Stagger validators to avoid race conditions (validator 2 waits 2s, validator 3 waits 4s)
  STAGGER_DELAY=$((VALIDATOR_ID * 2))
  echo "⏳ Waiting ${STAGGER_DELAY}s to stagger create-validator..."
  sleep $STAGGER_DELAY

  # Disable exit-on-error for create-validator (it may fail but we want to see the error)
  set +e

  # Retry loop for create-validator (handles intermittent failures)
  MAX_CREATE_RETRIES=3
  CREATE_RETRY=0
  CREATED=false

  while [ "$CREATE_RETRY" -lt "$MAX_CREATE_RETRIES" ] && [ "$CREATED" = "false" ]; do
    CREATE_RETRY=$((CREATE_RETRY + 1))
    echo "📤 Creating validator (attempt $CREATE_RETRY/$MAX_CREATE_RETRIES)..."

    CREATE_RESULT=$($BINARY tx staking create-validator "$VALIDATOR_JSON" \
      --from="$VALIDATOR_KEY" \
      --chain-id="$CHAIN_ID" \
      --keyring-backend="$KEYRING" \
      --home="$HOME_DIR" \
      --node="$GENESIS_RPC" \
      --gas=auto \
      --gas-adjustment=1.5 \
      --gas-prices="1000000000${DENOM}" \
      --yes \
      --output json 2>&1)

    # Check if it looks like a successful TX (has txhash and no Usage message)
    if echo "$CREATE_RESULT" | grep -q '"txhash"' && ! echo "$CREATE_RESULT" | grep -q "Usage:"; then
      TX_HASH=$(echo "$CREATE_RESULT" | jq -r '.txhash // ""' 2>/dev/null)
      echo "✅ Create-validator TX submitted: $TX_HASH"
      CREATED=true
    else
      echo "⚠️ Create-validator attempt failed, retrying in 3s..."
      echo "   Result: $(echo "$CREATE_RESULT" | head -c 200)"
      sleep 3
    fi
  done

  if [ "$CREATED" = "false" ]; then
    echo "❌ Create-validator failed after $MAX_CREATE_RETRIES attempts"
  fi

  # Re-enable exit-on-error
  set -e

  echo "⏳ Waiting for validator to be bonded..."
  sleep 15

  # Verify bonding
  VALIDATOR_STATUS=$($BINARY query staking validator "$VALOPER_ADDR" \
    --node="$GENESIS_RPC" \
    --output json 2>/dev/null | jq -r '.status' || echo "NOT_FOUND")

  if [ "$VALIDATOR_STATUS" = "BOND_STATUS_BONDED" ]; then
    echo "✅ Validator-$VALIDATOR_ID is now bonded!"
    VALIDATOR_TOKENS=$($BINARY query staking validator "$VALOPER_ADDR" \
      --node="$GENESIS_RPC" \
      --output json 2>/dev/null | jq -r '.tokens' || echo "0")
    echo "   Bonded tokens: $VALIDATOR_TOKENS"
  elif [ "$VALIDATOR_STATUS" = "BOND_STATUS_UNBONDING" ]; then
    echo "⚠️  Validator-$VALIDATOR_ID is unbonding"
  elif [ "$VALIDATOR_STATUS" = "BOND_STATUS_UNBONDED" ]; then
    echo "⚠️  Validator-$VALIDATOR_ID is unbonded"
  else
    echo "⚠️  Validator status: $VALIDATOR_STATUS"
  fi
fi

echo "✅ Validator setup complete!"

# ---------------------------
# === REGISTER UNIVERSAL VALIDATOR ===
# ---------------------------

echo "📝 Registering universal-validator-$VALIDATOR_ID..."

# Wait for validator to be bonded
sleep 10

# Get valoper address
VALOPER_ADDR=$($BINARY keys show validator-$VALIDATOR_ID --bech val -a --keyring-backend "$KEYRING" --home "$HOME_DIR" 2>/dev/null)

if [ -n "$VALOPER_ADDR" ]; then
  echo "📋 Validator-$VALIDATOR_ID valoper: $VALOPER_ADDR"

  # Pre-computed peer_ids (computed via puniversald tss-peer-id)
  case $VALIDATOR_ID in
    2)
      PEER_ID="12D3KooWJWoaqZhDaoEFshF7Rh1bpY9ohihFhzcW6d69Lr2NASuq"
      TSS_PORT=39001
      ;;
    3)
      PEER_ID="12D3KooWRndVhVZPCiQwHBBBdg769GyrPUW13zxwqQyf9r3ANaba"
      TSS_PORT=39002
      ;;
    4)
      PEER_ID="12D3KooWPT98FXMfDQYavZm66EeVjTqP9Nnehn1gyaydqV8L8BQw"
      TSS_PORT=39003
      ;;
    *)
      echo "⚠️ Unknown validator ID for UV registration"
      PEER_ID=""
      ;;
  esac

  if [ -n "$PEER_ID" ]; then
    MULTI_ADDR="/dns4/universal-validator-$VALIDATOR_ID/tcp/$TSS_PORT"
    NETWORK_JSON="{\"peer_id\": \"$PEER_ID\", \"multi_addrs\": [\"$MULTI_ADDR\"]}"

    echo "  Peer ID: $PEER_ID"
    echo "  Multi-addr: /dns4/universal-validator-$VALIDATOR_ID/tcp/$TSS_PORT"

    # Use genesis-acc-1 which is the admin
    # Import it first (mnemonic should be in shared volume)
    GENESIS_ACCOUNTS_FILE="/tmp/push-accounts/genesis_accounts.json"
    if [ -f "$GENESIS_ACCOUNTS_FILE" ]; then
      GENESIS_ACC_MNEMONIC=$(jq -r '.[0].mnemonic' "$GENESIS_ACCOUNTS_FILE")
      echo "$GENESIS_ACC_MNEMONIC" | $BINARY keys add genesis-acc-1 --recover --keyring-backend "$KEYRING" --home "$HOME_DIR" 2>/dev/null || true
    fi

    # Retry loop for registration (handles sequence mismatch race condition)
    MAX_RETRIES=5
    RETRY_COUNT=0
    REGISTERED=false

    while [ "$RETRY_COUNT" -lt "$MAX_RETRIES" ] && [ "$REGISTERED" = "false" ]; do
      RETRY_COUNT=$((RETRY_COUNT + 1))

      # Stagger validators to reduce race conditions (validator 2 waits 4s, validator 3 waits 6s)
      if [ "$RETRY_COUNT" -eq 1 ]; then
        STAGGER_DELAY=$((VALIDATOR_ID * 2))
        echo "⏳ Waiting ${STAGGER_DELAY}s to stagger registration..."
        sleep $STAGGER_DELAY
      fi

      echo "📤 Registering universal-validator-$VALIDATOR_ID (attempt $RETRY_COUNT/$MAX_RETRIES)..."
      RESULT=$($BINARY tx uvalidator add-universal-validator \
        --core-validator-address "$VALOPER_ADDR" \
        --network "$NETWORK_JSON" \
        --from genesis-acc-1 \
        --chain-id "$CHAIN_ID" \
        --keyring-backend "$KEYRING" \
        --home "$HOME_DIR" \
        --node="$GENESIS_RPC" \
        --fees 1000000000000000upc \
        --yes \
        --output json 2>&1 || echo "{}")

      # Check TX result
      TX_CODE=$(echo "$RESULT" | jq -r '.code // "null"' 2>/dev/null)
      TX_HASH=$(echo "$RESULT" | jq -r '.txhash // ""' 2>/dev/null)

      if [ "$TX_CODE" = "0" ] && [ -n "$TX_HASH" ]; then
        echo "✅ Universal-validator-$VALIDATOR_ID registered! TX: $TX_HASH"
        REGISTERED=true
      elif echo "$RESULT" | grep -q "sequence mismatch"; then
        echo "⚠️ Sequence mismatch, retrying in 3s..."
        sleep 3
      elif echo "$RESULT" | grep -q "already registered\|already exists"; then
        echo "✅ Universal-validator-$VALIDATOR_ID already registered"
        REGISTERED=true
      else
        RAW_LOG=$(echo "$RESULT" | jq -r '.raw_log // ""' 2>/dev/null)
        echo "⚠️ Registration attempt failed (code: $TX_CODE): ${RAW_LOG:-$(echo "$RESULT" | head -1)}"
        sleep 2
      fi
    done

    if [ "$REGISTERED" = "false" ]; then
      echo "❌ Registration failed after $MAX_RETRIES attempts"
    fi
  fi
else
  echo "⚠️ Could not get valoper address, skipping UV registration"
fi

echo "🔄 Node will continue running as validator..."

# Note: AuthZ grants are created by genesis validator (setup-genesis-auto.sh)
# since it has all validator keys and can create all grants immediately.

# Wait for the background process
wait $NODE_PID