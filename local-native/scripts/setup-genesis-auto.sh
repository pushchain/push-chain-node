#!/bin/bash
set -eu

# Load environment
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$SCRIPT_DIR/env.sh"

# Validator 1 specific config
VALIDATOR_ID=1
HOME_DIR="$DATA_DIR/validator$VALIDATOR_ID/.pchain"
MONIKER="validator-$VALIDATOR_ID"
LOG_FILE="$DATA_DIR/validator$VALIDATOR_ID/validator.log"

# Ports for validator 1
RPC_PORT=26657
REST_PORT=1317
GRPC_PORT=9090
P2P_PORT=26656

echo "🚨 Starting genesis validator setup..."
echo "Chain ID: $CHAIN_ID"
echo "Home: $HOME_DIR"

# Clean and initialize
rm -rf "$HOME_DIR"
mkdir -p "$HOME_DIR"
mkdir -p "$(dirname "$LOG_FILE")"

"$PCHAIND_BIN" init "$MONIKER" --chain-id "$CHAIN_ID" --default-denom "$DENOM" --home "$HOME_DIR"

# Load accounts
GENESIS_ACCOUNTS_FILE="$ACCOUNTS_DIR/genesis_accounts.json"
VALIDATORS_FILE="$ACCOUNTS_DIR/validators.json"
HOTKEYS_FILE="$ACCOUNTS_DIR/hotkeys.json"

# Import genesis accounts
echo "🔐 Importing genesis accounts..."
for i in 1 2 3 4 5; do
    mnemonic=$(jq -r ".[$((i-1))].mnemonic" "$GENESIS_ACCOUNTS_FILE")
    echo "$mnemonic" | "$PCHAIND_BIN" keys add "genesis-acc-$i" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOME_DIR" --recover 2>/dev/null || true
done

# Import validator accounts
echo "🔐 Importing validator accounts..."
for i in 1 2 3 4; do
    mnemonic=$(jq -r ".[$((i-1))].mnemonic" "$VALIDATORS_FILE")
    echo "$mnemonic" | "$PCHAIND_BIN" keys add "validator-$i" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOME_DIR" --recover 2>/dev/null || true
done

# Get addresses
GENESIS_ADDR1=$("$PCHAIND_BIN" keys show genesis-acc-1 -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
GENESIS_ADDR2=$("$PCHAIND_BIN" keys show genesis-acc-2 -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
GENESIS_ADDR3=$("$PCHAIND_BIN" keys show genesis-acc-3 -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
GENESIS_ADDR4=$("$PCHAIND_BIN" keys show genesis-acc-4 -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
GENESIS_ADDR5=$("$PCHAIND_BIN" keys show genesis-acc-5 -a --keyring-backend "$KEYRING" --home "$HOME_DIR")

VALIDATOR1_ADDR=$("$PCHAIND_BIN" keys show validator-1 -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
VALIDATOR2_ADDR=$("$PCHAIND_BIN" keys show validator-2 -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
VALIDATOR3_ADDR=$("$PCHAIND_BIN" keys show validator-3 -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
VALIDATOR4_ADDR=$("$PCHAIND_BIN" keys show validator-4 -a --keyring-backend "$KEYRING" --home "$HOME_DIR")

# Fund genesis accounts
echo "💰 Funding genesis accounts..."
"$PCHAIND_BIN" genesis add-genesis-account "$GENESIS_ADDR1" "${TWO_BILLION}${DENOM}" --home "$HOME_DIR"
"$PCHAIND_BIN" genesis add-genesis-account "$GENESIS_ADDR2" "${TWO_BILLION}${DENOM}" --home "$HOME_DIR"
"$PCHAIND_BIN" genesis add-genesis-account "$GENESIS_ADDR3" "${TWO_BILLION}${DENOM}" --home "$HOME_DIR"
"$PCHAIND_BIN" genesis add-genesis-account "$GENESIS_ADDR4" "${TWO_BILLION}${DENOM}" --home "$HOME_DIR"
"$PCHAIND_BIN" genesis add-genesis-account "$GENESIS_ADDR5" "${TWO_BILLION}${DENOM}" --home "$HOME_DIR"

# Fund validators
echo "💰 Funding validators..."
"$PCHAIND_BIN" genesis add-genesis-account "$VALIDATOR1_ADDR" "${ONE_MILLION}${DENOM}" --home "$HOME_DIR"
"$PCHAIND_BIN" genesis add-genesis-account "$VALIDATOR2_ADDR" "${ONE_MILLION}${DENOM}" --home "$HOME_DIR"
"$PCHAIND_BIN" genesis add-genesis-account "$VALIDATOR3_ADDR" "${ONE_MILLION}${DENOM}" --home "$HOME_DIR"
"$PCHAIND_BIN" genesis add-genesis-account "$VALIDATOR4_ADDR" "${ONE_MILLION}${DENOM}" --home "$HOME_DIR"

# Fund hotkeys
echo "💰 Funding hotkeys..."
for i in 1 2 3 4; do
    hotkey_addr=$(jq -r ".[$((i-1))].address" "$HOTKEYS_FILE")
    "$PCHAIND_BIN" genesis add-genesis-account "$hotkey_addr" "${HOTKEY_FUNDING}${DENOM}" --home "$HOME_DIR"
done

# Fund contract deployer address
echo "💰 Funding contract deployer..."
CONTRACT_DEPLOYER="push1w7xnyp3hf79vyetj3cvw8l32u6unun8yr6zn60"
"$PCHAIND_BIN" genesis add-genesis-account "$CONTRACT_DEPLOYER" "${TWO_BILLION}${DENOM}" --home "$HOME_DIR"

# Create gentx
echo "📝 Creating gentx..."
"$PCHAIND_BIN" genesis gentx validator-1 "${VALIDATOR_STAKE}${DENOM}" \
    --keyring-backend "$KEYRING" \
    --chain-id "$CHAIN_ID" \
    --home "$HOME_DIR" \
    --gas-prices "1000000000${DENOM}"

"$PCHAIND_BIN" genesis collect-gentxs --home "$HOME_DIR"
"$PCHAIND_BIN" genesis validate-genesis --home "$HOME_DIR"

# Update genesis parameters
echo "🛠️ Updating genesis parameters..."
update_genesis() {
    cat "$HOME_DIR/config/genesis.json" | jq "$1" > "$HOME_DIR/config/tmp_genesis.json" && mv "$HOME_DIR/config/tmp_genesis.json" "$HOME_DIR/config/genesis.json"
}

update_genesis '.consensus["params"]["block"]["time_iota_ms"]="1000"'
update_genesis ".app_state[\"gov\"][\"params\"][\"min_deposit\"]=[{\"denom\":\"$DENOM\",\"amount\":\"1000000\"}]"
update_genesis '.app_state["gov"]["params"]["max_deposit_period"]="300s"'
update_genesis '.app_state["gov"]["params"]["voting_period"]="300s"'
update_genesis ".app_state[\"evm\"][\"params\"][\"evm_denom\"]=\"$DENOM\""
update_genesis ".app_state[\"evm\"][\"params\"][\"chain_config\"][\"chain_id\"]=$EVM_CHAIN_ID"
update_genesis '.app_state["evm"]["params"]["active_static_precompiles"]=["0x00000000000000000000000000000000000000CB","0x00000000000000000000000000000000000000ca","0x0000000000000000000000000000000000000100","0x0000000000000000000000000000000000000400","0x0000000000000000000000000000000000000800","0x0000000000000000000000000000000000000801","0x0000000000000000000000000000000000000802","0x0000000000000000000000000000000000000803","0x0000000000000000000000000000000000000804","0x0000000000000000000000000000000000000805"]'
update_genesis ".app_state[\"staking\"][\"params\"][\"bond_denom\"]=\"$DENOM\""
update_genesis ".app_state[\"mint\"][\"params\"][\"mint_denom\"]=\"$DENOM\""
update_genesis ".app_state[\"uregistry\"][\"params\"][\"admin\"]=\"$GENESIS_ADDR1\""
update_genesis ".app_state[\"utss\"][\"params\"][\"admin\"]=\"$GENESIS_ADDR1\""
update_genesis ".app_state[\"uvalidator\"][\"params\"][\"admin\"]=\"$GENESIS_ADDR1\""
update_genesis '.consensus["params"]["abci"]["vote_extensions_enable_height"]="2"'

# Config patches
echo "⚙️ Configuring network..."
# Allow multiple connections from same IP (needed for local multi-validator setup)
sed -i.bak 's/allow_duplicate_ip = false/allow_duplicate_ip = true/g' "$HOME_DIR/config/config.toml"
sed -i.bak "s/laddr = \"tcp:\/\/127.0.0.1:26657\"/laddr = \"tcp:\/\/0.0.0.0:${RPC_PORT}\"/g" "$HOME_DIR/config/config.toml"
sed -i.bak 's/cors_allowed_origins = \[\]/cors_allowed_origins = \["\*"\]/g' "$HOME_DIR/config/config.toml"
sed -i.bak "s/address = \"tcp:\/\/localhost:1317\"/address = \"tcp:\/\/0.0.0.0:${REST_PORT}\"/g" "$HOME_DIR/config/app.toml"
sed -i.bak 's/enable = false/enable = true/g' "$HOME_DIR/config/app.toml"
sed -i.bak "s/address = \"localhost:9090\"/address = \"0.0.0.0:${GRPC_PORT}\"/g" "$HOME_DIR/config/app.toml"
sed -i.bak 's/timeout_commit = "5s"/timeout_commit = "1s"/g' "$HOME_DIR/config/config.toml"

# Copy genesis for other validators
cp "$HOME_DIR/config/genesis.json" "$ACCOUNTS_DIR/genesis.json"
echo "✅ Genesis copied to $ACCOUNTS_DIR/genesis.json"

# Start validator (output goes to stdout, devnet script redirects to log file)
echo "🚀 Starting genesis validator..."
exec "$PCHAIND_BIN" start \
    --home "$HOME_DIR" \
    --pruning=nothing \
    --minimum-gas-prices="1000000000${DENOM}" \
    --rpc.laddr="tcp://0.0.0.0:${RPC_PORT}" \
    --json-rpc.address="0.0.0.0:8545" \
    --json-rpc.ws-address="0.0.0.0:8546" \
    --json-rpc.api=eth,txpool,personal,net,debug,web3 \
    --chain-id="$CHAIN_ID"
