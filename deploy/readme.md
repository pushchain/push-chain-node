# Local test net (1 node)

TBD, mostly the same steps as in [Deploy validator 1] except the final [deploy] steps

# Remote test net (1..N nodes)

## VM Preconditions (WIP)
you need to run a vm with enough CPU/RAM/DISK, accessible via ssh using only private key auth

a special user 'chain' will be used for deployment

Here is a VM preparation example for pn3 host
```sh
export REMOTE_HOST="pn3.dev.push.org"
ssh $REMOTE_HOST

# app will hold chain binary
mkdir ~/app
# .push will hold home directory with /config and /data
mkdir ~/.push
# add to path
echo 'export PATH="$HOME/app:$HOME/.push/config/scripts:$PATH"' >> ~/.bashrc

# open firewall ports (MANUAL STEP) : 
#   26656 for p2p, 
#   26657 for rpc (optional)
# to check availability
# run on the VM
nc -l 26656
# run on DEV machine
telnet $REMOTE_HOST 26656
```


## Build validator1 configs (!required only once) (WIP)
- Generates validator1 config data to /test-push-chain-0: genesis.json, config.toml, app.toml, client.toml 
- Generates validator1 private keys to /test-push-chain-0: node_key.json, priv_validator_key.json
- Generates binary build to /release: push_linux_amd64.tar.gz
- Generates template config data for Validator2+ to /test-push-chain: genesis.json
  (these configs are stored in GIT)


```sh
export VALIDATOR_NAME="pn1"
export CHAIN_ID="test-push-chain"
export TOKEN_NAME=npush
export HDIR="test-push-chain-0"
export HDIR_CONFIG="$HDIR/config"
# 1 push = 1 mil npush
export ONE_PUSH=000000npush
export MIL_PUSH=000000$ONEPUSH
# assume we're in /deploy dir
cd push-chain/deploy

# allow 'alias' binding
shopt -s expand_aliases

# build binary on dev machine
(cd .. && ignite chain build)
# bind cmd name to specific binary path
alias pushchaind="~/go/bin/pushchaind"
# check that binary works
pushchaind version

# create keys (write down the memo words)(you need this only once per environment)
pushchaind keys add user1
export user1addr=$(pushchaind keys show user1 -a)
pushchaind keys add user2
export user2addr=$(pushchaind keys show user2 -a)

# a testnetwork of 1 node 
# generates all configs + node_key.json, priv_validator_key.json
pushchaind init $VALIDATOR_NAME --home $HDIR --chain-id $CHAIN_ID

# build genesis.json
### register 2 genesis accounts with 500k push each
pushchaind genesis add-genesis-account $user1addr 5000$MIL_PUSH --home $HDIR
pushchaind genesis add-genesis-account $user2addr 5000$MIL_PUSH --home $HDIR
# replace all tokens with npush; npush is nano push; it is 1/1 000 000 of push
# to replace with jq
sed -i '' 's/stake/npush/g' $HDIR_CONFIG/genesis.json
# register 1 founder validator in genesis.json ;
pushchaind genesis gentx user1 10000$ONE_PUSH --chain-id $CHAIN_ID --home $HDIR
# put all txs into genesis.json
pushchaind genesis collect-gentxs --home $HDIR

## configs

# edit app.toml
python3 toml_edit.py $HDIR_CONFIG/app.toml "minimum-gas-prices" "0.25npush"

# make bundle
# copy to templates dir for further deployments
# no modifications
cp $HDIR/config/client.toml $CHAIN_ID/config/client.toml
# no modifications (at this stage)
cp $HDIR/config/app.toml $CHAIN_ID/config/app.toml.sample
# no modifications (at this stage)
cp $HDIR/config/config.toml $CHAIN_ID/config/config.toml.sample
# edited
cp $HDIR/config/genesis.json $CHAIN_ID/config/genesis.json
echo "generated network wide config: genesis.json"
cat $CHAIN_ID/config/genesis.json
echo "-----"

```

## Deploy validator1 configs & binary (!required only once)  (WIP)
```shell
# HDIR contains private keys, since we generated them at prev steps
export HDIR="test-push-chain-0"
test -f $HDIR/config/node_key.json || echo 'error, no private key file to upload'
test -f $HDIR/config/priv_validator_key.json || echo 'error, no private key file to upload'

export REMOTE_HOST="pn1.dev.push.org"

# upload configs (no processing is needed, since for 1st validator everything is generated locally )
./deploy-config-unpack.sh $HDIR $REMOTE_HOST "/home/chain/.push" "UNPACK"
# upload binary
./deploy-code.sh "../release/push_linux_amd64.tar.gz" $REMOTE_HOST "/home/chain/app"
```


## Deploy validator2+ (uses template data from validator1)
Fills in configs from template data: genesis.json, config.toml, app.toml, client.toml for pn2+
also deploys pn2+
```sh
# (! CAREFULLY CHECK REMOTE_HOST = This is your deployment target)
export VALIDATOR_NAME=pn3
export REMOTE_HOST="$VALIDATOR_NAME.dev.push.org"
export HDIR="test-push-chain"
export HDIR_CONFIG="$HDIR/config"
export CHAIN_ID="test-push-chain"

# assume we're in /deploy dir
cd push-chain/deploy

#### prepare binary
# build linux-specific binary
./build-code.sh

#### prepare configs
# edit app.toml : set min gas price
cp $HDIR_CONFIG/app.toml.sample $HDIR_CONFIG/app.toml
python3 toml_edit.py $HDIR_CONFIG/app.toml "minimum-gas-prices" "0.25npush"

# edit config.toml : set human readable node name
cp $HDIR_CONFIG/config.toml.sample $HDIR_CONFIG/config.toml
python3 toml_edit.py $HDIR_CONFIG/config.toml "moniker" "$VALIDATOR_NAME"

# edit config.toml: set persistent peers 
# !! this is id of the validator1, 
# check by "pushchaind tendermint show-node-id --home test-push-chain-0"  or "pushchaind tendermint show-node-id" on pn1
export pn1_id=a1ba93b69fb0ff339909fcd502d404d6e4b9c422
export pn1_url="$pn1_id@pn1.dev.push.org:26656"
python3 toml_edit.py $HDIR_CONFIG/config.toml "p2p.persistent_peers" "$pn1_url"

#### upload binary  
scp "../release/push_linux_amd64.tar.gz" "$REMOTE_HOST:~/push_linux_amd64.tar.gz"
#### zip configs
COPYFILE_DISABLE=1 tar -czvf "push-home.tar.gz" -C "$HDIR" .
#### upload configs
scp ".push.tar.gz" "$REMOTE_HOST:~/push-home.tar.gz"
```

now ssh into the REMOTE HOST! to generate private key & apply configs on REMOTE HOST!!!

New node
```sh
export VALIDATOR_NAME=pn3
export REMOTE_HOST="$VALIDATOR_NAME.dev.push.org"
ssh pn3.dev.push.org

# REMOTE CMDS GO BELOW
export VALIDATOR_NAME=pn3
export CHAIN_ID="test-push-chain"

# unpack binary
tar -xzvf "$HOME/push_linux_amd64.tar.gz" -C "$HOME/app" 
chmod u+x "$HOME/app/pushchaind"

# create your private keys
~/app/pushchaind init $VALIDATOR_NAME --chain-id $CHAIN_ID

# overwrite some configs from .tar.gz configs
# (configs are already with the correct variables!!)
tar --no-same-owner --no-same-permissions -xzvf "$HOME/push-home.tar.gz" -C "$HOME/.push"
chmod u+x ~/.push/scripts/*.sh 





# stop if running
~/.push/scripts/stop.sh
# start
~/.push/scripts/start.sh
# check that node is syncing
tail -n 100 ~/app/chain.log
# wait for full node sync (manually or via this script)
~/.push/scripts/waitFullSync.sh

# TODO 
# upgrade node to validator

# 0 create register-validator.json (edit required vars)
export VALIDATOR_PUBKEY=$(pushchaind comet show-validator)
export ONE_PUSH=000000npush
export VALIDATOR_NAME=\"pn3\"
cat <<EOF > register-validator.json
{
	"pubkey": $VALIDATOR_PUBKEY,
	"amount": "10000$ONE_PUSH",
	"moniker": $VALIDATOR_NAME,
	"website": "example.com",
	"security": "swein2@gmail.com",
	"details": "a test validator",
	"commission-rate": "0.1",
	"commission-max-rate": "0.2",
	"commission-max-change-rate": "0.01",
	"min-self-delegation": "1"
}
EOF
echo "validator_pubkey is $validator_pubkey"
echo "json cmd is "
cat register-validator.json
```

Faucet machine
```sh
# do it from DEV machine with faucet key (or import user2 key to the validator host)

# 1 create a wallet, sends tokens to the wallet if needed 
# user3 = validator owner wallet
export NODE_OWNER_WALLET_NAME=user3
export CHAIN_NAME=test-push-chain
pushchaind keys add $NODE_OWNER_WALLET_NAME --keyring-backend test

# here push1tjxdmycqua5j8f8y3j9ac5hn6cjhx2pgsaj6vs is the node wallet (from command above)
export NODE_OWNER_WALLET=push1tjxdmycqua5j8f8y3j9ac5hn6cjhx2pgsaj6vs
# here push1j55s4vpvmncruakqhj2k2fywnc9mvsuhcap28q is the faucet wallet (you need it's priv key)
export FAUCET_WALLET=push1j55s4vpvmncruakqhj2k2fywnc9mvsuhcap28q
export ONE_PUSH=000000npush
# we transfer 20k PUSH
pushchaind tx bank send $FAUCET_WALLET $NODE_OWNER_WALLET   20000$ONE_PUSH --fees 500000npush --chain-id $CHAIN_NAME  --keyring-backend test
# check to have 20k PUSH
pushchaind query bank balances $NODE_OWNER_WALLET --chain-id $CHAIN_NAME  --keyring-backend test
# 2 register validator with stake
# pn1.dev.push.org - is the existing public node with api
pushchaind tx staking create-validator register-validator.json --chain-id $CHAIN_NAME --fees 500000npush --from $NODE_OWNER_WALLET_NAME --node=tcp://pn1.dev.push.org:26657
```
New node
```sh
# 3 restart the chain process
ssh $REMOTE_HOST
# stop if running
~/.push/scripts/stop.sh
# start
~/.push/scripts/start.sh
```

