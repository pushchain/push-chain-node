export RPC=${RPC:-"26657"}
export REST=${REST:-"1317"}
export PROFF=${PROFF:-"6060"}
export P2P=${P2P:-"26656"}
export GRPC=${GRPC:-"9090"}
export GRPC_WEB=${GRPC_WEB:-"9091"}
export ROSETTA=${ROSETTA:-"8080"}
export BLOCK_TIME=${BLOCK_TIME:-"1s"}
export CHAIN_HOME="$HOME/.pchain"

# your chain id
export CHAIN_ID="push_42101-1"
# your node name
export MONIKER="val-2"
# keyring type
export KEYRING="test"
# token name
export DENOM="npc"

read -p "-> This will remove all config .toml files. Type DELETEALL to continue " response
if [[ "$response" == "DELETEALL" || "$response" == "deleteall" ]]; then
  rm "$CHAIN_HOME/config/app.toml"
  rm "$CHAIN_HOME/config/client.toml"
  rm "$CHAIN_HOME/config/config.toml"
  rm "$CHAIN_HOME/node_key.json"
  rm "$CHAIN_HOME/priv_validator_key.json"
  rm -rf "$CHAIN_HOME/data"
  # todo rm -rf $CHAIN_HOME/
else
  exit 1
fi

~/app/pchaind config set client chain-id "$CHAIN_ID"
~/app/pchaind config set client keyring-backend $KEYRING

echo "generating initial config in $CHAIN_HOME/config"
~/app/pchaind init $MONIKER --chain-id $CHAIN_ID --default-denom $DENOM



echo "editing generated configs"
# RPC endpoint (in config.toml, under [rpc])
python3 "$HOME/app/toml_edit.py" "$CHAIN_HOME/config/config.toml" "rpc.laddr" "tcp://0.0.0.0:$RPC"

# Set CORS allowed origins (in config.toml, under [rpc])
python3 "$HOME/app/toml_edit.py" "$CHAIN_HOME/config/config.toml" "rpc.cors_allowed_origins" "[\"*\"]"

# REST endpoint (in app.toml, under [api])
python3 "$HOME/app/toml_edit.py" "$CHAIN_HOME/config/app.toml" "api.address" "tcp://0.0.0.0:$REST"

# Enable the REST API and unsafe CORS (in app.toml, under [api])
python3 "$HOME/app/toml_edit.py" "$CHAIN_HOME/config/app.toml" "api.enable" true
python3 "$HOME/app/toml_edit.py" "$CHAIN_HOME/config/app.toml" "api.enabled-unsafe-cors" true

# Peer exchange settings in config.toml:
# Update pprof_laddr (global key) to use the new port
python3 "$HOME/app/toml_edit.py" "$CHAIN_HOME/config/config.toml" "pprof_laddr" "localhost:$PROFF"

# Update the P2P listen address (in config.toml, under [p2p])
python3 "$HOME/app/toml_edit.py" "$CHAIN_HOME/config/config.toml" "p2p.laddr" "tcp://0.0.0.0:$P2P"

# GRPC endpoints in app.toml:
# Set the gRPC server address (in [grpc])
python3 "$HOME/app/toml_edit.py" "$CHAIN_HOME/config/app.toml" "grpc.address" "0.0.0.0:$GRPC"

# Set the gRPC-web address (in [grpc-web]) – if your file uses an address key here
python3 "$HOME/app/toml_edit.py" "$CHAIN_HOME/config/app.toml" "grpc-web.address" "0.0.0.0:$GRPC_WEB"

# EVM endpoings
python3 "$HOME/app/toml_edit.py" "$CHAIN_HOME/config/app.toml" "json-rpc.address" "0.0.0.0:8545"
python3 "$HOME/app/toml_edit.py" "$CHAIN_HOME/config/app.toml" "json-rpc.ws-address" "0.0.0.0:8546"

# Rosetta API endpoint (in app.toml)
python3 "$HOME/app/toml_edit.py" "$CHAIN_HOME/config/app.toml" "rosetta.address" "0.0.0.0:$ROSETTA"

# Faster blocks: update consensus timeout_commit (in config.toml, under [consensus])
python3 "$HOME/app/toml_edit.py" "$CHAIN_HOME/config/config.toml" "consensus.timeout_commit" "$BLOCK_TIME"

# Min gas price
python3 "$HOME/app/toml_edit.py" "$CHAIN_HOME/config/app.toml" "minimum-gas-prices" "0npush"