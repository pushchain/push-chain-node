#!/bin/bash
shopt -s expand_aliases
set -eu

export KEY1="acc1"
export KEY2="acc2"
export KEY3="acc3"
export KEY4="acc4"
export KEY5="acc5"

export CHAIN_ID="push_42101-1"
export MONIKER="donut-node1"
export KEYALGO="eth_secp256k1"
export KEYRING=${KEYRING:-"test"}
export HOME_DIR=$(eval echo "~/.pchain")
export BINARY=${BINARY:-pchaind}
export DENOM=${DENOM:-upc}

export CLEAN="true"
export RPC=${RPC:-"26657"}
export REST=${REST:-"1317"}
export PROFF=${PROFF:-"6060"}
export P2P=${P2P:-"26656"}
export GRPC=${GRPC:-"9090"}
export GRPC_WEB=${GRPC_WEB:-"9091"}
export ROSETTA=${ROSETTA:-"8080"}
export BLOCK_TIME="1000ms"

# if which binary does not exist, install it
if [ -z `which $BINARY` ]; then
  make install

  if [ -z `which $BINARY` ]; then
    echo "Ensure $BINARY is installed and in your PATH"
    exit 1
  fi
fi

alias BINARY="$BINARY --home=$HOME_DIR"

command -v $BINARY > /dev/null 2>&1 || { echo >&2 "$BINARY command not found. Ensure this is setup / properly installed in your GOPATH (make install)."; exit 1; }
command -v jq > /dev/null 2>&1 || { echo >&2 "jq not installed. More info: https://stedolan.github.io/jq/download/"; exit 1; }

set_config() {
  $BINARY config set client chain-id $CHAIN_ID
  $BINARY config set client keyring-backend $KEYRING
}
set_config


from_scratch () {
  # Fresh install on current branch
#  make install

  # remove existing daemon files.
  if [ ${#HOME_DIR} -le 2 ]; then
      echo "HOME_DIR must be more than 2 characters long"
      return
  fi
  rm -rf $HOME_DIR && echo "Removed $HOME_DIR"

  # reset values if not set already after whipe
  set_config

  # Instead of adding private keys, use pre-computed addresses
  # These addresses are provided for genesis account allocation
  export ADDR1="push1jtdw9kjc2yptl6yjyad69q73v2gcl29xfmmq5a"
  export ADDR2="push1fz5mkhr23kypp5ejq8u2ucltvm4gw2ln5vnpcz"
  export ADDR3="push1cxrtcjgrxzup0s548467cqhtytmfh0cf4lzy2y"
  export ADDR4="push1jm42e22eqxzcknhkhf3rrhpevpk0zcyv3c68xa"
  export ADDR5="push1v9vw2m3jrrxeyql07ec63n8amrkhpl94ttsfsv"

  # NOTE: If you need different addresses, you can generate them using:
  # echo "your-mnemonic-here" | pchaind keys add temp-key --keyring-backend test --algo eth_secp256k1 --recover --dry-run
  # Then delete the temp key: pchaind keys delete temp-key --keyring-backend test

  BINARY init $MONIKER --chain-id $CHAIN_ID --default-denom $DENOM

  update_test_genesis () {
    cat $HOME_DIR/config/genesis.json | jq "$1" > $HOME_DIR/config/tmp_genesis.json && mv $HOME_DIR/config/tmp_genesis.json $HOME_DIR/config/genesis.json
  }

  # === CORE MODULES ===

  # Block
  update_test_genesis '.consensus_params["block"]["max_gas"]="100000000"'
  update_test_genesis '.consensus_params["block"]["time_iota_ms"]="1000"'

  # Gov
  update_test_genesis `printf '.app_state["gov"]["params"]["min_deposit"]=[{"denom":"%s","amount":"1000000"}]' $DENOM`
  update_test_genesis '.app_state["gov"]["params"]["voting_period"]="172800s"'
  update_test_genesis '.app_state["gov"]["params"]["expedited_voting_period"]="86400s"'

  update_test_genesis `printf '.app_state["evm"]["params"]["evm_denom"]="%s"' $DENOM`
  update_test_genesis '.app_state["evm"]["params"]["active_static_precompiles"]=["0x00000000000000000000000000000000000000ca","0x0000000000000000000000000000000000000100","0x0000000000000000000000000000000000000400","0x0000000000000000000000000000000000000800","0x0000000000000000000000000000000000000801","0x0000000000000000000000000000000000000802","0x0000000000000000000000000000000000000803","0x0000000000000000000000000000000000000804","0x0000000000000000000000000000000000000805"]'
  # Extract numeric part from CHAIN_ID for EVM chain_id (e.g., push_42101-1 -> 42101)
  EVM_CHAIN_ID=$(echo $CHAIN_ID | sed 's/.*_\([0-9]*\).*/\1/')
  #update_test_genesis `printf '.app_state["evm"]["params"]["chain_config"]["chain_id"]="%s"' $EVM_CHAIN_ID`
  #update_test_genesis `printf '.app_state["evm"]["params"]["chain_config"]["denom"]="%s"' $DENOM`
  #update_test_genesis '.app_state["evm"]["params"]["chain_config"]["decimals"]="18"'
  update_test_genesis '.app_state["erc20"]["params"]["native_precompiles"]=["0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"]' # https://eips.ethereum.org/EIPS/eip-7528
  update_test_genesis `printf '.app_state["erc20"]["token_pairs"]=[{contract_owner:1,erc20_address:"0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",denom:"%s",enabled:true}]' $DENOM`
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

  ## abci
  update_test_genesis '.consensus["params"]["abci"]["vote_extensions_enable_height"]="1"'

  # === CUSTOM MODULES ===
  # tokenfactory
  update_test_genesis '.app_state["tokenfactory"]["params"]["denom_creation_fee"]=[]'
  update_test_genesis '.app_state["tokenfactory"]["params"]["denom_creation_gas_consume"]=100000'


  # Allocate genesis accounts using addresses directly (no private keys needed)
  # Total Supply: 10 Billion PUSH (10 * 10^9 PUSH)
  # Each account gets 2 Billion PUSH (2 * 10^9 PUSH)
  # Amount format for PUSH token (upc): amount * 10^18
  # So, 2 Billion PUSH = 2 * 10^9 * 10^18 = 2 * 10^27 upc -> we want to mint 2 billion PUSH
  # Represented as 2000000000000000000000000000
  local two_billion_npush="2000000000000000000000000000" # 2 followed by 27 zeros
  local addr5_amount="1999000000000000000000000000" # 1.999 billion PUSH (1 million less)
  local one_million_npush="1000000000000000000000000" # 1 million PUSH

  BINARY genesis add-genesis-account $ADDR1 ${two_billion_npush}$DENOM --keyring-backend $KEYRING --append
  BINARY genesis add-genesis-account $ADDR2 ${two_billion_npush}$DENOM --keyring-backend $KEYRING --append
  BINARY genesis add-genesis-account $ADDR3 ${two_billion_npush}$DENOM --keyring-backend $KEYRING --append
  BINARY genesis add-genesis-account $ADDR4 ${two_billion_npush}$DENOM --keyring-backend $KEYRING --append
  BINARY genesis add-genesis-account $ADDR5 ${addr5_amount}$DENOM --keyring-backend $KEYRING --append

  # For gentx, you'll need to add at least one key for the validator
  # This can be done separately or with a different approach
  echo "<mnemonic>" | BINARY keys add $KEY1 --keyring-backend $KEYRING --algo $KEYALGO --recover

  # Get the validator address and fund it with 1 million PUSH
  VALIDATOR_ADDR=$(BINARY keys show $KEY1 -a --keyring-backend $KEYRING)
  BINARY genesis add-genesis-account $VALIDATOR_ADDR ${one_million_npush}$DENOM --keyring-backend $KEYRING --append

  # Sign genesis transaction
  # 10 000 . 000000000 000000000
  BINARY genesis gentx $KEY1 10000000000000000000000$DENOM --gas-prices 1000000000${DENOM} --keyring-backend $KEYRING --chain-id $CHAIN_ID

  BINARY genesis collect-gentxs

  BINARY genesis validate-genesis
  err=$?
  if [ $err -ne 0 ]; then
    echo "Failed to validate genesis"
    return
  fi
}

# check if CLEAN is not set to false
if [ "$CLEAN" != "false" ]; then
  echo "Starting from a clean state"
  from_scratch
fi

echo "Starting node..."

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

# EVM
python3 "$HOME/app/toml_edit.py" "$HOME_DIR/config/app.toml" "json-rpc.address" "0.0.0.0:8545"
python3 "$HOME/app/toml_edit.py" "$HOME_DIR/config/app.toml" "json-rpc.ws-address" "0.0.0.0:8546"

# Rosetta Api
sed -i -e 's/address = ":8080"/address = "0.0.0.0:'$ROSETTA'"/g' $HOME_DIR/config/app.toml

# Faster blocks
sed -i -e 's/timeout_commit = "5s"/timeout_commit = "'$BLOCK_TIME'"/g' $HOME_DIR/config/config.toml

BINARY start --pruning=nothing  --minimum-gas-prices=1000000000$DENOM --rpc.laddr="tcp://0.0.0.0:$RPC" --json-rpc.api=eth,txpool,personal,net,debug,web3 --chain-id="$CHAIN_ID"
