
# Deploy validator1
```sh
# allow 'alias' binding
shopt -s expand_aliases

export VALIDATOR_NAME="pn1"
export HDIR="test-push-chain-0"
export HDIR_CONFIG="$HDIR/config"
export CHAIN_ID="test-push-chain"

# bind cmd name to specific binary path
alias pushchaind="~/go/bin/pushchaind"

# build binary
./build-code.sh

# create keys (write down the memo words)(you need this only once per environment)
pushchaind keys add user1
export user1addr=$(pushchaind keys show user1 -a)
pushchaind keys add user2
export user2addr=$(pushchaind keys show user2 -a)

# a testnetwork of 1 node
pushchaind init $VALIDATOR_NAME --home $HDIR --chain-id $CHAIN_ID

# build config/genesis.json
### register 2 genesis accounts with 500k push each
pushchaind genesis add-genesis-account $user1addr 500000000000npush --home $HDIR
pushchaind genesis add-genesis-account $user2addr 500000000000npush --home $HDIR
# replace all tokens with npush; npush is nano push; it is 1/1 000 000 of push
sed -i '' 's/stake/npush/g' $HDIR_CONFIG/genesis.json


# I cannot register more no matter how hord I try
# this is the command we have in 0.50:
#   pushchaind genesis gentx <key_name> <amount> [flags]
# this is the command which I need in 0.52:
#    pushchaind genesis gentx <key_name> 10000000000stake  --chain-id push-test-chain --moniker="pn1" --commission-rate="0.10" --commission-max-rate="0.20" --commission-max-change-rate="0.01" --min-self-delegation="1" --ip "192.168.88.114" --node-id <your_node_id> --home ~/.tn/pn2
# register 1 founder validator in genesis.json ;
pushchaind genesis gentx user1 10000000000npush --chain-id $CHAIN_ID --home $HDIR
# put all txs into genesis.json
pushchaind genesis collect-gentxs --home $HDIR

## configs

# edit app.toml
python3 toml_edit.py $HDIR_CONFIG/app.toml "minimum-gas-prices" "0.25npush"

```
# Deploy validator2+
```sh
./build-code.sh
./deploy-code.sh

# (! CAREFULLY CHECK HOSTNAME)
export VALIDATOR_NAME=pn3
export CONFIG_HOME_DIR="test-push-chain"
export REMOTE_HOST="$VALIDATOR_NAME.dev.push.org"

# edit config.toml
(cd $CONFIG_HOME_DIR && cp config.toml.sample config.toml)
(cd $CONFIG_HOME_DIR && python3 toml_edit.py config.toml "moniker" "$VALIDATOR_NAME")

# edit app.toml
(cd $CONFIG_HOME_DIR && cp app.toml.sample app.toml)
(cd $CONFIG_HOME_DIR && python3 toml_edit.py app.toml "minimum-gas-prices" "0.25npush")


# edit config.toml

# !! this is id of the validator1, 
# check by "pushchaind tendermint show-node-id --home test-push-chain-0" 
# or "pushchaind tendermint show-node-id" on pn1
export pn1_id=a1ba93b69fb0ff339909fcd502d404d6e4b9c422
export pn1_url="$pn1_id@pn1.dev.push.org:26656"
 
(cd $CONFIG_HOME_DIR && python3 toml_edit.py config.toml "persistent_peers" "$pn1_url")

# upload configs 
./deploy-config.sh
```