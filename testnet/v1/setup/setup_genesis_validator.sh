#!/bin/bash
set -eu
shopt -s expand_aliases

# ---------------------------
# === USER CONFIGURATION ===
# ---------------------------

CHAIN_ID="push_42101-1" 
EVM_CHAIN_ID="42101"
MONIKER="genesis-validator"
KEY_NAME="validator-key"
KEYRING="os"  # use 'os' for security; avoid 'test' in prod
KEYALGO="eth_secp256k1"
DENOM="upc"

# Base path
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BINARY="$APP_DIR/binary/pchaind"
HOME_DIR="$APP_DIR/.pchain"
LOG_DIR="$APP_DIR/logs"

# Ports (customizable)
RPC=${RPC:-26657}
REST=${REST:-1317}
GRPC=${GRPC:-9090}
GRPC_WEB=${GRPC_WEB:-9091}
PROFF=${PROFF:-6060}
P2P=${P2P:-26656}
ROSETTA=${ROSETTA:-8080}
BLOCK_TIME=${BLOCK_TIME:-"1s"}

# === Precomputed Genesis Addresses ===
ADDR1="push1p9wxp8uczwdmt0d5f4nzayqezhha5lrxv0heqg"
ADDR2="push14s60v703yd6mmjtpksvkc45xydshhh6044eg3a"
ADDR3="push1s5ash48rydteltqwxd3grdlnt7jkpqlsxmdwak"
ADDR4="push1777ape9p9j38ddwxn65weapnnf424ahzrlgt4y"
ADDR5="push1rqxv07ccljhfskceqwggtms8j034mae56njr5t"

# ---------------------------
# === FUNDING SETUP ===
# ---------------------------

TWO_BILLION="2000000000000000000000000000"         # 2 * 10^9 * 10^18
ONE_MILLION="1000000000000000000000000"            # 1 * 10^6 * 10^18
VALIDATOR_STAKE="100000000000000000000000"         # 100,000 * 10^18
LAST_ACCOUNT_AMOUNT="1999000000000000000000000000" # 2B - 1M

# ---------------------------
# === CLEAN START ===
# ---------------------------

echo "🚨 Removing old node at $HOME_DIR"
rm -rf "$HOME_DIR"

echo "🚨 Removing old logs at $LOG_DIR"
rm -rf "$LOG_DIR"

echo "🧱 Initializing chain: $MONIKER ($CHAIN_ID)"
"$BINARY" init "$MONIKER" --chain-id "$CHAIN_ID" --default-denom "$DENOM" --home "$HOME_DIR"

# ---------------------------
# === FUND GENESIS ACCOUNTS
# ---------------------------

echo "💰 Funding genesis accounts..."
"$BINARY" genesis add-genesis-account "$ADDR1" "${TWO_BILLION}${DENOM}" --home "$HOME_DIR"
"$BINARY" genesis add-genesis-account "$ADDR2" "${TWO_BILLION}${DENOM}" --home "$HOME_DIR"
"$BINARY" genesis add-genesis-account "$ADDR3" "${TWO_BILLION}${DENOM}" --home "$HOME_DIR"
"$BINARY" genesis add-genesis-account "$ADDR4" "${TWO_BILLION}${DENOM}" --home "$HOME_DIR"
"$BINARY" genesis add-genesis-account "$ADDR5" "${LAST_ACCOUNT_AMOUNT}${DENOM}" --home "$HOME_DIR"

# ---------------------------
# === CREATE VALIDATOR KEY
# ---------------------------

echo "🔐 Creating validator key (manual entry)"
"$BINARY" keys add "$KEY_NAME" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOME_DIR"

VALIDATOR_ADDR=$("$BINARY" keys show "$KEY_NAME" -a --keyring-backend "$KEYRING" --home "$HOME_DIR")

echo "💵 Funding validator with ${VALIDATOR_STAKE}${DENOM}..."
"$BINARY" genesis add-genesis-account "$VALIDATOR_ADDR" "${ONE_MILLION}${DENOM}" --home "$HOME_DIR"

# ---------------------------
# === CREATE GENTX
# ---------------------------

echo "📝 Generating gentx..."
"$BINARY" genesis gentx "$KEY_NAME" "${VALIDATOR_STAKE}${DENOM}" \
  --keyring-backend "$KEYRING" \
  --chain-id "$CHAIN_ID" \
  --home "$HOME_DIR" \
  --gas-prices "1000000000${DENOM}"

"$BINARY" genesis collect-gentxs --home "$HOME_DIR"
"$BINARY" genesis validate-genesis --home "$HOME_DIR"

# ---------------------------
# === GENESIS PARAM PATCHING
# ---------------------------
echo "🛠️ Updating genesis parameters..."

  update_test_genesis () {
    cat $HOME_DIR/config/genesis.json | jq "$1" > $HOME_DIR/config/tmp_genesis.json && mv $HOME_DIR/config/tmp_genesis.json $HOME_DIR/config/genesis.json
  }

  # === CORE MODULES ===

  # Block
  update_test_genesis '.consensus["params"]["block"]["max_gas"]="100000000"'
  update_test_genesis '.consensus["params"]["block"]["time_iota_ms"]="1000"'

  # Gov
  update_test_genesis `printf '.app_state["gov"]["params"]["min_deposit"]=[{"denom":"%s","amount":"1000000"}]' $DENOM`
  update_test_genesis '.app_state["gov"]["params"]["max_deposit_period"]="300s"'
  update_test_genesis '.app_state["gov"]["params"]["voting_period"]="300s"'
  update_test_genesis '.app_state["gov"]["params"]["expedited_voting_period"]="150s"'

  # EVM
  update_test_genesis `printf '.app_state["evm"]["params"]["evm_denom"]="%s"' $DENOM` # This seems duplicated since chain config already has this
  update_test_genesis '.app_state["evm"]["params"]["active_static_precompiles"]=["0x00000000000000000000000000000000000000ca","0x0000000000000000000000000000000000000100","0x0000000000000000000000000000000000000400","0x0000000000000000000000000000000000000800","0x0000000000000000000000000000000000000801","0x0000000000000000000000000000000000000802","0x0000000000000000000000000000000000000803","0x0000000000000000000000000000000000000804","0x0000000000000000000000000000000000000805","0x0000000000000000000000000000000000000901"]'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["homestead_block"]="0"'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["dao_fork_block"]="0"'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["dao_fork_support"]=true'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["eip150_block"]="0"'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["eip150_hash"]="0x0000000000000000000000000000000000000000000000000000000000000000"'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["eip155_block"]="0"'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["eip158_block"]="0"'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["byzantium_block"]="0"'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["constantinople_block"]="0"'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["petersburg_block"]="0"'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["istanbul_block"]="0"'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["muir_glacier_block"]="0"'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["berlin_block"]="0"'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["london_block"]="0"'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["arrow_glacier_block"]="0"'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["gray_glacier_block"]="0"'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["merge_netsplit_block"]="0"'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["shanghai_block"]="0"'
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["cancun_block"]="0"'
  update_test_genesis `printf '.app_state["evm"]["params"]["chain_config"]["chain_id"]=%s' $EVM_CHAIN_ID`
  update_test_genesis `printf '.app_state["evm"]["params"]["chain_config"]["denom"]="%s"' $DENOM`
  update_test_genesis '.app_state["evm"]["params"]["chain_config"]["decimals"]="18"'

  update_test_genesis '.app_state["erc20"]["params"]["native_precompiles"]=["0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"]' # https://eips.ethereum.org/EIPS/eip-7528
  update_test_genesis `printf '.app_state["erc20"]["token_pairs"]=[{contract_owner:1,erc20_address:"0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",denom:"%s",enabled:true}]' $DENOM`

  
  # feemarket
  update_test_genesis '.app_state["feemarket"]["params"]["no_base_fee"]=false'
  update_test_genesis '.app_state["feemarket"]["params"]["base_fee"]="1000000000.000000000000000000"'
  update_test_genesis '.app_state["feemarket"]["params"]["min_gas_price"]="1000000000.000000000000000000"'

  # staking
  update_test_genesis `printf '.app_state["staking"]["params"]["bond_denom"]="%s"' $DENOM`
  update_test_genesis '.app_state["staking"]["params"]["min_commission_rate"]="0.050000000000000000"'

  # mint
  update_test_genesis `printf '.app_state["mint"]["params"]["mint_denom"]="%s"' $DENOM`

  # crisis
  update_test_genesis `printf '.app_state["crisis"]["constant_fee"]={"denom":"%s","amount":"1000"}' $DENOM`

  ## distribution
  update_test_genesis '.app_state["distribution"]["params"]["community_tax"]="0.000000000000000000"'

  ## abci
  update_test_genesis '.consensus["params"]["abci"]["vote_extensions_enable_height"]="1"'

  # === CUSTOM MODULES ===
  # tokenfactory
  update_test_genesis '.app_state["tokenfactory"]["params"]["denom_creation_fee"]=[]'
  update_test_genesis '.app_state["tokenfactory"]["params"]["denom_creation_gas_consume"]=100000'

# ---------------------------
# === CONFIG PATCHING
# ---------------------------

# Opens the RPC endpoint to outside connections
sed -i -e 's/laddr = "tcp:\/\/127.0.0.1:26657"/c\laddr = "tcp:\/\/0.0.0.0:'$RPC'"/g' $HOME_DIR/config/config.toml
sed -i -e 's/cors_allowed_origins = \[\]/cors_allowed_origins = \["\*"\]/g' $HOME_DIR/config/config.toml

# REST endpoint
sed -i -e 's/address = "tcp:\/\/localhost:1317"/address = "tcp:\/\/0.0.0.0:'$REST'"/g' $HOME_DIR/config/app.toml
sed -i -e 's/enable = false/enable = true/g' $HOME_DIR/config/app.toml
sed -i -e 's/enabled-unsafe-cors = false/enabled-unsafe-cors = true/g' $HOME_DIR/config/app.toml

# peer exchange
sed -i -e 's/pprof_laddr = "localhost:6060"/pprof_laddr = "localhost:'$PROFF'"/g' $HOME_DIR/config/config.toml
sed -i -e 's/laddr = "tcp:\/\/0.0.0.0:26656"/laddr = "tcp:\/\/0.0.0.0:'$P2P'"/g' $HOME_DIR/config/config.toml

# GRPC
sed -i -e 's/address = "localhost:9090"/address = "0.0.0.0:'$GRPC'"/g' $HOME_DIR/config/app.toml
sed -i -e 's/address = "localhost:9091"/address = "0.0.0.0:'$GRPC_WEB'"/g' $HOME_DIR/config/app.toml

# Rosetta Api
sed -i -e 's/address = ":8080"/address = "0.0.0.0:'$ROSETTA'"/g' $HOME_DIR/config/app.toml

# Faster blocks
sed -i -e 's/timeout_commit = "5s"/timeout_commit = "'$BLOCK_TIME'"/g' $HOME_DIR/config/config.toml

# ---------------------------
# ✅ DONE
# ---------------------------

echo ""
echo "✅ Genesis validator setup complete!"
echo "➡️ Start the chain with:"
echo "   bash $APP_DIR/scripts/start.sh"