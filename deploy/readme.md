
# Local test net (1 node)

```shell
cd push-chain
make install
cd push-chain/scripts
CHAIN_ID="42101" MONIKER=pn1 HOME_DIR="~/.pchain" BLOCK_TIME="1000ms" CLEAN=true ./test_node.sh
```

# dev.push.org test net (1..N nodes)

## VM Preconditions
you need to run a vm with enough CPU/RAM/DISK, accessible via ssh using only private key auth

a default user will be used for deployment (currently: igx)

Here is a VM preparation example for pn2 host
```sh
export REMOTE_HOST="pn2.dev.push.org"
ssh $REMOTE_HOST

# app will hold chain binary
mkdir ~/app
# .push will hold home directory with /config and /data
mkdir ~/.pchaind
# add to path
echo 'export PATH="$HOME/app:$PATH"' >> ~/.bashrc

# open firewall ports (MANUAL STEP) : 
#   26656 for p2p, 
#   26657 for rpc (optional)
# to check availability
# run on the VM
nc -l 26656
# run on DEV machine
telnet $REMOTE_HOST 26656
```
OK - VM is ready

## Build binary 
```shell
git clone https://github.com/push-protocol/push-chain.git
cd deploy
# for amd64 os
./build-code.sh  # OR ./build-code-on-arm-target-amd64.sh for macos m1
```

## Setup 1st node
```sh
# upload binary
scp "../build/pchaind" "pn1.dev.push.org:~/app/pchaind"
# upload scripts
scp -r test-push-chain/scripts/* "pn1.dev.push.org:~/app/"
# !!! upload make_first_node.sh (edit this file in-place on the remote and insert 3 genesis wallets - see '!!!PUT MNEMONIC1 HERE!!!')
scp make_first_node.sh "pn1.dev.push.org:~/app/"

#
#
# ssh into remote host !!
#
#
ssh pn1.dev.push.org
chmod u+x ~/app/pchaind
chmod u+x ~/app/*.sh

# start make_first_node.sh
#   ensure that on line 80:   
#   add_key $KEY1 2 3 has the correct wallet mnemonic which you own
#   after creation remove make_first_node.sh to hide keys (this is not not very safe)
cd ~/app
CHAIN_ID="42101" MONIKER=pn1 HOME_DIR="~/.pchain" BLOCK_TIME="1000ms" CLEAN=true ./make_first_node.sh
# CTRL-C to stop

# run in the backgr
~/app/start.sh

# check logs
~/app/showLogs.sh
```

## Setup 2nd node (or 3rd, 4th, etc - all steps are the same)
```sh
# ----------------- step1: upload files 
# upload binary
scp "../build/pchaind" "pn2.dev.push.org:~/app/pchaind"
# upload scripts from git
scp -r test-push-chain/scripts/* "pn2.dev.push.org:~/app/"
# upload configs from git (genesis, toml files are not being used, they are here only for reference)
scp -r test-push-chain/config/* "pn2.dev.push.org:~/app/config-tmp"

# remote ssh
# set executable
ssh pn2.dev.push.org
chmod u+x ~/app/pchaind
chmod u+x ~/app/*.sh

# ----------------- step2: generate configs
export CHAIN_DIR="$HOME/.pchain"
# get python3 + tomlkit lib
sudo apt install python3 python3-pip
pip install tomlkit
python3 --version # check python3.10+ is there
# setup initial configs & edit toml files with proper values (check the script manually before running)
~/app/resetConfigs.sh
# set moniker (this is the node name)
python3 "$HOME/app/toml_edit.py" "$CHAIN_DIR/config/config.toml" "moniker" "pn2"
# wallet @ url for the initial (seed) nodes to connect to the rest of the network
# note : execute this cmd on the remote node to get it's id : pchaind tendermint show-node-id 
export pn1_url="bc7105d5927a44638ac2ad7f6986ec98dacc5ac6@pn1.dev.push.org:26656"
python3 "$HOME/app/toml_edit.py" $CHAIN_DIR/config/config.toml "p2p.persistent_peers" "$pn1_url"
# copy genesis.json from the 1st node (currently all values arrive from git)
cp ~/app/config-tmp/genesis.json "$CHAIN_DIR/config/"

# ----------------- step3: run (non-validator) blockchain node an sync it
# stop if running
~/app/stop.sh
# start
~/app/start.sh
# check that node is syncing
tail -n 100 ~/app/chain.log
# wait for full node sync (manually or via this script)
~/app/waitFullSync.sh
# wait for: 'The Node has been fully synced '
```
Generate cmd for registering
```shell
# ----------------- Step4: Generate cmd for registering
# show new node id
pchaind tendermint show-node-id

# mint some tokens via Faucet
export VALIDATOR_PUBKEY=$(pchaind comet show-validator)
export ONE_PUSH=000000000000000000npush
export VALIDATOR_NAME=\"pn2\"
cat <<EOF > register-validator.json
{
	"pubkey": $VALIDATOR_PUBKEY,
	"amount": "20000$ONE_PUSH",
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
echo "validator name: $VALIDATOR_NAME, pubkey is $VALIDATOR_PUBKEY"
echo "json cmd is "
cat register-validator.json

```
Generate node wallet & Register & Check (from another node or Faucet wallet)
```sh
# ----------------- Step5: Generate node wallet
# this example does not use the keys pre-created in genesis.json, this allows to show a more generic case when a new node is being added
# to the already established network
export KEYRING="test"
export NODE_OWNER_WALLET_NAME=acc21
# write & save mnemonic for future use
pchaind keys add $NODE_OWNER_WALLET_NAME --keyring-backend "$KEYRING"


# # ----------------- Step 6: Transfer funds from Faucet
#  faucet wallet (you need it's priv key)
export FAUCET_WALLET=push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20
#  node wallet (from the cmd above: pchaind tendermint show-node-id)
export NODE_OWNER_WALLET=push1upvlrjlsvxpgk03nz327szz9wgcjh8afhk26k0
export ONE_PUSH=000000000000000000npush
export CHAIN_ID="42101"

# we transfer 20k PUSH
pchaind tx bank send "$FAUCET_WALLET" "$NODE_OWNER_WALLET"   "20000$ONE_PUSH" --fees 1000000000000000npush --chain-id "$CHAIN_ID"  --keyring-backend "$KEYRING"
# check to have 20k PUSH
pchaind query bank balances $NODE_OWNER_WALLET --chain-id $CHAIN_ID  --keyring-backend $KEYRING

# ----------------- Step7: Register new validator with new wallet in the network
# 2 register validator with stake
# pn1.dev.push.org - is the existing public node with api
pchaind tx staking create-validator register-validator.json --chain-id $CHAIN_ID --fees "1$ONE_PUSH" --gas "1000000" --from $NODE_OWNER_WALLET_NAME --node=tcp://pn1.dev.push.org:26657
# check that tx was successful
# check that code=0 and raw_log=""
export TX_ID= # from above
pchaind query tx $TX_ID --chain-id $CHAIN_ID --output json | jq '{code, raw_log}'
# check that validator got bonded tokens
# the output should contain
# moniker: YOUR VALIDATOR NAME
# status: BOND_STATUS_BONDED
# replace pn2 with validator name:
pchaind query staking validators --output json | jq '.validators[] | select(.description.moniker=="pn2")'
```
Restart the node, check logs
```sh
# ----------------- Step8: restart the new node process
# 3 restart the chain process
ssh pn2.dev.push.org
# stop if running
~/app/stop.sh
# start
~/app/start.sh
# check that node is syncing
tail -n 100 ~/app/chain.log
```