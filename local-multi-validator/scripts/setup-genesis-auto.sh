#!/bin/bash
set -eu
shopt -s expand_aliases

# ---------------------------
# === CONFIGURATION ===
# ---------------------------

CHAIN_ID=${CHAIN_ID:-"localchain_9000-1"}
EVM_CHAIN_ID="9000"
MONIKER=${MONIKER:-"genesis-validator"}
KEYRING="test"  # Use test keyring for local development
KEYALGO="eth_secp256k1"
DENOM="upc"

# Paths
BINARY="/usr/bin/pchaind"
HOME_DIR="/root/.pchain"

# Create alias for convenience (following original pattern)
alias BINARY="$BINARY --home=$HOME_DIR"

# Ports (from environment)
RPC_PORT=${RPC_PORT:-26657}
REST_PORT=${REST_PORT:-1317}
GRPC_PORT=${GRPC_PORT:-9090}
GRPC_WEB_PORT=$((GRPC_PORT + 1))
P2P_PORT=${P2P_PORT:-26656}
PROFF_PORT=$((RPC_PORT + 3))
ROSETTA_PORT=8080
BLOCK_TIME="1s"

# Genesis funding amounts
TWO_BILLION="2000000000000000000000000000"         # 2 * 10^9 * 10^18
ONE_MILLION="1000000000000000000000000"            # 1 * 10^6 * 10^18
VALIDATOR_STAKE="100000000000000000000000"         # 100,000 * 10^18
HOTKEY_FUNDING="10000000000000000000000"           # 10,000 * 10^18 (for gas fees)

# ---------------------------
# === LOAD PRE-GENERATED ACCOUNTS ===
# ---------------------------

TMP_DIR="/tmp/push-accounts"
GENESIS_ACCOUNTS_FILE="$TMP_DIR/genesis_accounts.json"
VALIDATORS_FILE="$TMP_DIR/validators.json"
HOTKEYS_FILE="$TMP_DIR/hotkeys.json"

# Check if account files exist
if [ ! -f "$GENESIS_ACCOUNTS_FILE" ] || [ ! -f "$VALIDATORS_FILE" ] || [ ! -f "$HOTKEYS_FILE" ]; then
  echo "❌ Account files not found. Please run generate-accounts.sh first:"
  echo "   /opt/scripts/generate-accounts.sh"
  exit 1
fi

echo "📋 Loading pre-generated accounts from:"
echo "  Genesis accounts: $GENESIS_ACCOUNTS_FILE"
echo "  Validator accounts: $VALIDATORS_FILE"
echo "  Hotkey accounts: $HOTKEYS_FILE"

# Load genesis account mnemonics
GENESIS_ACC1_MNEMONIC=$(jq -r '.[0].mnemonic' "$GENESIS_ACCOUNTS_FILE")
GENESIS_ACC2_MNEMONIC=$(jq -r '.[1].mnemonic' "$GENESIS_ACCOUNTS_FILE")
GENESIS_ACC3_MNEMONIC=$(jq -r '.[2].mnemonic' "$GENESIS_ACCOUNTS_FILE")
GENESIS_ACC4_MNEMONIC=$(jq -r '.[3].mnemonic' "$GENESIS_ACCOUNTS_FILE")
GENESIS_ACC5_MNEMONIC=$(jq -r '.[4].mnemonic' "$GENESIS_ACCOUNTS_FILE")

# Load validator mnemonic for genesis validator
VALIDATOR1_MNEMONIC=$(jq -r '.[] | select(.id == 1) | .mnemonic' "$VALIDATORS_FILE")

# ---------------------------
# === INITIALIZATION ===
# ---------------------------

echo "🚨 Starting genesis validator setup..."
echo "Chain ID: $CHAIN_ID"
echo "Moniker: $MONIKER"

# Clean start
rm -rf "$HOME_DIR"/* "$HOME_DIR"/.[!.]* "$HOME_DIR"/..?* 2>/dev/null || true

echo "🧱 Initializing chain..."
BINARY init "$MONIKER" --chain-id "$CHAIN_ID" --default-denom "$DENOM"

# ---------------------------
# === CREATE GENESIS FUNDING ACCOUNTS ===
# ---------------------------

echo "🔐 Creating genesis funding accounts with known mnemonics..."

# Create 5 genesis accounts that will hold the initial funds
echo "Adding genesis-acc-1 with mnemonic..."
if BINARY keys show genesis-acc-1 --keyring-backend $KEYRING >/dev/null 2>&1; then
    echo "Key genesis-acc-1 already exists, skipping..."
else
    echo $GENESIS_ACC1_MNEMONIC | BINARY keys add genesis-acc-1 --keyring-backend $KEYRING --algo $KEYALGO --recover || { echo "Failed to add genesis-acc-1"; exit 1; }
fi
echo $GENESIS_ACC2_MNEMONIC | BINARY keys add genesis-acc-2 --keyring-backend $KEYRING --algo $KEYALGO --recover
echo $GENESIS_ACC3_MNEMONIC | BINARY keys add genesis-acc-3 --keyring-backend $KEYRING --algo $KEYALGO --recover
echo $GENESIS_ACC4_MNEMONIC | BINARY keys add genesis-acc-4 --keyring-backend $KEYRING --algo $KEYALGO --recover
echo $GENESIS_ACC5_MNEMONIC | BINARY keys add genesis-acc-5 --keyring-backend $KEYRING --algo $KEYALGO --recover

# Get genesis account addresses
GENESIS_ADDR1=$(BINARY keys show genesis-acc-1 -a --keyring-backend $KEYRING)
GENESIS_ADDR2=$(BINARY keys show genesis-acc-2 -a --keyring-backend $KEYRING)
GENESIS_ADDR3=$(BINARY keys show genesis-acc-3 -a --keyring-backend $KEYRING)
GENESIS_ADDR4=$(BINARY keys show genesis-acc-4 -a --keyring-backend $KEYRING)
GENESIS_ADDR5=$(BINARY keys show genesis-acc-5 -a --keyring-backend $KEYRING)

echo "Genesis account addresses:"
echo "  Account 1: $GENESIS_ADDR1"
echo "  Account 2: $GENESIS_ADDR2"
echo "  Account 3: $GENESIS_ADDR3"
echo "  Account 4: $GENESIS_ADDR4"
echo "  Account 5: $GENESIS_ADDR5"

# ---------------------------
# === CREATE VALIDATOR KEYS (Only Genesis Validator) ===
# ---------------------------

echo "🔐 Creating genesis validator key..."
echo $VALIDATOR1_MNEMONIC | BINARY keys add validator-1 --keyring-backend $KEYRING --algo $KEYALGO --recover
VALIDATOR1_ADDR=$(BINARY keys show validator-1 -a --keyring-backend $KEYRING)
echo "Genesis validator address: $VALIDATOR1_ADDR"

# ---------------------------
# === FUND GENESIS ACCOUNTS ===
# ---------------------------

echo "💰 Funding genesis accounts in genesis state..."
BINARY genesis add-genesis-account "$GENESIS_ADDR1" "${TWO_BILLION}${DENOM}"
BINARY genesis add-genesis-account "$GENESIS_ADDR2" "${TWO_BILLION}${DENOM}"
BINARY genesis add-genesis-account "$GENESIS_ADDR3" "${TWO_BILLION}${DENOM}"
BINARY genesis add-genesis-account "$GENESIS_ADDR4" "${TWO_BILLION}${DENOM}"
BINARY genesis add-genesis-account "$GENESIS_ADDR5" "${TWO_BILLION}${DENOM}"

echo "💵 Funding genesis validator in genesis state..."
BINARY genesis add-genesis-account "$VALIDATOR1_ADDR" "${ONE_MILLION}${DENOM}"

# ---------------------------
# === FUND HOTKEY ACCOUNTS ===
# ---------------------------

echo "💵 Funding hotkey accounts in genesis state..."

# Load hotkey addresses early for funding
HOTKEY1_ADDR=$(jq -r '.[0].address' "$HOTKEYS_FILE")
HOTKEY2_ADDR=$(jq -r '.[1].address' "$HOTKEYS_FILE")
HOTKEY3_ADDR=$(jq -r '.[2].address' "$HOTKEYS_FILE")

echo "Hotkey addresses to fund:"
echo "  Hotkey 1: $HOTKEY1_ADDR"
echo "  Hotkey 2: $HOTKEY2_ADDR"
echo "  Hotkey 3: $HOTKEY3_ADDR"

BINARY genesis add-genesis-account "$HOTKEY1_ADDR" "${HOTKEY_FUNDING}${DENOM}"
BINARY genesis add-genesis-account "$HOTKEY2_ADDR" "${HOTKEY_FUNDING}${DENOM}"
BINARY genesis add-genesis-account "$HOTKEY3_ADDR" "${HOTKEY_FUNDING}${DENOM}"

echo "✅ Hotkey accounts funded with ${HOTKEY_FUNDING}${DENOM} each"

# ---------------------------
# === CREATE GENTX (only for genesis validator) ===
# ---------------------------

echo "📝 Generating gentx for genesis validator..."
BINARY genesis gentx validator-1 "${VALIDATOR_STAKE}${DENOM}" \
  --keyring-backend $KEYRING \
  --chain-id $CHAIN_ID \
  --gas-prices "1000000000${DENOM}"

BINARY genesis collect-gentxs
BINARY genesis validate-genesis

# ---------------------------
# === GENESIS PARAMETERS ===
# ---------------------------

echo "🛠️ Updating genesis parameters..."

update_genesis() {
  cat $HOME_DIR/config/genesis.json | jq "$1" > $HOME_DIR/config/tmp_genesis.json && mv $HOME_DIR/config/tmp_genesis.json $HOME_DIR/config/genesis.json
}

# Block settings
update_genesis '.consensus["params"]["block"]["time_iota_ms"]="1000"'

# Governance
update_genesis `printf '.app_state["gov"]["params"]["min_deposit"]=[{"denom":"%s","amount":"1000000"}]' $DENOM`
update_genesis '.app_state["gov"]["params"]["max_deposit_period"]="300s"'
update_genesis '.app_state["gov"]["params"]["voting_period"]="300s"'
update_genesis '.app_state["gov"]["params"]["expedited_voting_period"]="150s"'

# EVM
update_genesis `printf '.app_state["evm"]["params"]["evm_denom"]="%s"' $DENOM`
update_genesis '.app_state["evm"]["params"]["active_static_precompiles"]=["0x00000000000000000000000000000000000000CB","0x00000000000000000000000000000000000000ca","0x0000000000000000000000000000000000000100","0x0000000000000000000000000000000000000400","0x0000000000000000000000000000000000000800","0x0000000000000000000000000000000000000801","0x0000000000000000000000000000000000000802","0x0000000000000000000000000000000000000803","0x0000000000000000000000000000000000000804","0x0000000000000000000000000000000000000805"]'

# EVM Chain config
update_genesis `printf '.app_state["evm"]["params"]["chain_config"]["chain_id"]=%s' $EVM_CHAIN_ID`
update_genesis `printf '.app_state["evm"]["params"]["chain_config"]["denom"]="%s"' $DENOM`
update_genesis '.app_state["evm"]["params"]["chain_config"]["decimals"]="18"'

# ERC20
update_genesis '.app_state["erc20"]["params"]["native_precompiles"]=["0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"]'
update_genesis `printf '.app_state["erc20"]["token_pairs"]=[{contract_owner:1,erc20_address:"0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",denom:"%s",enabled:true}]' $DENOM`

# Fee market
update_genesis '.app_state["feemarket"]["params"]["no_base_fee"]=false'
update_genesis '.app_state["feemarket"]["params"]["base_fee"]="1000000.000000000000000000"'
update_genesis '.app_state["feemarket"]["params"]["min_gas_price"]="1000000.000000000000000000"'

# Staking
update_genesis `printf '.app_state["staking"]["params"]["bond_denom"]="%s"' $DENOM`
update_genesis '.app_state["staking"]["params"]["min_commission_rate"]="0.050000000000000000"'

# Mint
update_genesis `printf '.app_state["mint"]["params"]["mint_denom"]="%s"' $DENOM`

# Crisis
update_genesis `printf '.app_state["crisis"]["constant_fee"]={"denom":"%s","amount":"1000"}' $DENOM`

# Distribution
update_genesis '.app_state["distribution"]["params"]["community_tax"]="0.000000000000000000"'

# ABCI
update_genesis '.consensus["params"]["abci"]["vote_extensions_enable_height"]="2"'

# Token factory
update_genesis '.app_state["tokenfactory"]["params"]["denom_creation_fee"]=[]'
update_genesis '.app_state["tokenfactory"]["params"]["denom_creation_gas_consume"]=100000'

# Uregistry - Set admin to genesis-acc-1
update_genesis ".app_state[\"uregistry\"][\"params\"][\"admin\"]=\"$GENESIS_ADDR1\""

# Utss - Set admin to genesis-acc-1 (allows initiating TSS key processes)
update_genesis ".app_state[\"utss\"][\"params\"][\"admin\"]=\"$GENESIS_ADDR1\""

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
sed -i -e "s/timeout_commit = \"5s\"/timeout_commit = \"${BLOCK_TIME}\"/g" $HOME_DIR/config/config.toml

# ---------------------------
# === SETUP AUTHZ FOR UNIVERSAL VALIDATORS ===
# ---------------------------

echo "🔐 Setting up AuthZ grants for universal validator hotkeys..."

# Hotkey addresses were already loaded during genesis funding section
echo "Using hotkey addresses for AuthZ grants:"
echo "  Hotkey 1: $HOTKEY1_ADDR"
echo "  Hotkey 2: $HOTKEY2_ADDR"
echo "  Hotkey 3: $HOTKEY3_ADDR"

# Create AuthZ grants in genesis
# Each genesis account grants permissions to its corresponding hotkey
# Grant MsgVoteInbound and MsgVoteOutbound permissions

# Build the authz authorization array
AUTHZ_GRANTS='[]'

# Helper function to add a grant
add_authz_grant() {
  local granter=$1
  local grantee=$2
  local msg_type=$3
  AUTHZ_GRANTS=$(echo "$AUTHZ_GRANTS" | jq --arg granter "$granter" --arg grantee "$grantee" --arg msg "$msg_type" \
    '. += [{
      "granter": $granter,
      "grantee": $grantee,
      "authorization": {
        "@type": "/cosmos.authz.v1beta1.GenericAuthorization",
        "msg": $msg
      },
      "expiration": null
    }]')
}

# Genesis account 1 grants to Hotkey 1
add_authz_grant "$GENESIS_ADDR1" "$HOTKEY1_ADDR" "/uexecutor.v1.MsgVoteInbound"
add_authz_grant "$GENESIS_ADDR1" "$HOTKEY1_ADDR" "/uexecutor.v1.MsgVoteOutbound"
add_authz_grant "$GENESIS_ADDR1" "$HOTKEY1_ADDR" "/uexecutor.v1.MsgVoteGasPrice"
add_authz_grant "$GENESIS_ADDR1" "$HOTKEY1_ADDR" "/utss.v1.MsgVoteTssKeyProcess"

# Genesis account 2 grants to Hotkey 2
add_authz_grant "$GENESIS_ADDR2" "$HOTKEY2_ADDR" "/uexecutor.v1.MsgVoteInbound"
add_authz_grant "$GENESIS_ADDR2" "$HOTKEY2_ADDR" "/uexecutor.v1.MsgVoteOutbound"
add_authz_grant "$GENESIS_ADDR2" "$HOTKEY2_ADDR" "/uexecutor.v1.MsgVoteGasPrice"
add_authz_grant "$GENESIS_ADDR2" "$HOTKEY2_ADDR" "/utss.v1.MsgVoteTssKeyProcess"

# Genesis account 3 grants to Hotkey 3
add_authz_grant "$GENESIS_ADDR3" "$HOTKEY3_ADDR" "/uexecutor.v1.MsgVoteInbound"
add_authz_grant "$GENESIS_ADDR3" "$HOTKEY3_ADDR" "/uexecutor.v1.MsgVoteOutbound"
add_authz_grant "$GENESIS_ADDR3" "$HOTKEY3_ADDR" "/uexecutor.v1.MsgVoteGasPrice"
add_authz_grant "$GENESIS_ADDR3" "$HOTKEY3_ADDR" "/utss.v1.MsgVoteTssKeyProcess"

# Update genesis with AuthZ grants
echo "Adding AuthZ grants to genesis..."
cat $HOME_DIR/config/genesis.json | jq --argjson grants "$AUTHZ_GRANTS" '.app_state["authz"]["authorization"] = $grants' > $HOME_DIR/config/tmp_genesis.json && mv $HOME_DIR/config/tmp_genesis.json $HOME_DIR/config/genesis.json

echo "✅ AuthZ grants added to genesis"

# ---------------------------
# === SHARE GENESIS FILE ===
# ---------------------------

# Copy genesis to shared volume for other validators
# This ensures they get the exact same genesis file (RPC endpoint returns different JSON format)
echo "📋 Copying genesis to shared volume..."
cp "$HOME_DIR/config/genesis.json" "/tmp/push-accounts/genesis.json"
echo "✅ Genesis shared at /tmp/push-accounts/genesis.json"

# Debug: Output checksum for comparison with other validators
echo "📊 GENESIS CHECKSUM (validator-1):"
echo "  Local:  $(sha256sum $HOME_DIR/config/genesis.json)"
echo "  Shared: $(sha256sum /tmp/push-accounts/genesis.json)"

# ---------------------------
# === START VALIDATOR ===
# ---------------------------

echo "🚀 Starting genesis validator..."
echo "✅ Genesis setup complete!"

# Start validator in background so we can register universal validator
/usr/bin/pchaind start \
  --home "$HOME_DIR" \
  --pruning=nothing \
  --minimum-gas-prices="1000000000${DENOM}" \
  --rpc.laddr="tcp://0.0.0.0:${RPC_PORT}" \
  --json-rpc.api=eth,txpool,personal,net,debug,web3 \
  --chain-id="$CHAIN_ID" &

PCHAIND_PID=$!
echo "Validator started with PID: $PCHAIND_PID"

# ---------------------------
# === REGISTER UNIVERSAL VALIDATOR 1 ===
# ---------------------------

echo "📝 Waiting to register universal-validator-1..."

# Wait for first block
max_block_attempts=60
block_attempt=0
while [ $block_attempt -lt $max_block_attempts ]; do
  BLOCK_HEIGHT=$(curl -s "http://127.0.0.1:${RPC_PORT}/status" 2>/dev/null | jq -r '.result.sync_info.latest_block_height // "0"' 2>/dev/null || echo "0")
  if [ "$BLOCK_HEIGHT" != "0" ] && [ "$BLOCK_HEIGHT" != "null" ] && [ -n "$BLOCK_HEIGHT" ]; then
    echo "✅ Chain producing blocks (height: $BLOCK_HEIGHT)"
    break
  fi
  sleep 2
  block_attempt=$((block_attempt + 1))
done

if [ $block_attempt -lt $max_block_attempts ]; then
  # Get valoper address for validator-1
  sleep 5  # Wait a bit more for indexing
  VALOPER_ADDR=$($BINARY keys show validator-1 --bech val -a --keyring-backend "$KEYRING" --home "$HOME_DIR" 2>/dev/null)

  if [ -n "$VALOPER_ADDR" ]; then
    echo "📋 Validator-1 valoper: $VALOPER_ADDR"

    # Pre-computed peer_id for UV1 (key: 0101...01)
    # Computed via: puniversald tss-peer-id --private-key 0101010101010101010101010101010101010101010101010101010101010101
    PEER_ID_UV1="12D3KooWK99VoVxNE7XzyBwXEzW7xhK7Gpv85r9F3V3fyKSUKPH5"
    MULTI_ADDR_UV1="/dns4/universal-validator-1/tcp/39000"
    NETWORK_JSON="{\"peer_id\": \"$PEER_ID_UV1\", \"multi_addrs\": [\"$MULTI_ADDR_UV1\"]}"

    echo "📤 Registering universal-validator-1..."
    echo "  Peer ID: $PEER_ID_UV1"
    echo "  Multi-addr: $MULTI_ADDR_UV1"

    RESULT=$($BINARY tx uvalidator add-universal-validator \
      --core-validator-address "$VALOPER_ADDR" \
      --network "$NETWORK_JSON" \
      --from genesis-acc-1 \
      --chain-id "$CHAIN_ID" \
      --keyring-backend "$KEYRING" \
      --home "$HOME_DIR" \
      --fees 1000000000000000upc \
      --yes \
      --output json 2>&1 || echo "{}")

    if echo "$RESULT" | grep -q '"txhash"'; then
      TX_HASH=$(echo "$RESULT" | jq -r '.txhash' 2>/dev/null)
      echo "✅ Universal-validator-1 registered! TX: $TX_HASH"
    else
      echo "⚠️ Registration may have failed: $(echo "$RESULT" | head -1)"
    fi
  else
    echo "⚠️ Could not get valoper address, skipping UV-1 registration"
  fi
else
  echo "⚠️ Chain not producing blocks, skipping UV-1 registration"
fi

# Wait for the validator process
echo "🔄 Validator running..."
wait $PCHAIND_PID