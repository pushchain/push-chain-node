
# Remote test net (1..N nodes)

## VM Preconditions
you need to run a vm with enough CPU/RAM/DISK, accessible via ssh using only private key auth

a default user will be used for deployment (currently: igx)

Here is a VM preparation example for pn3 host
```sh
export REMOTE_HOST="pn3.dev.push.org"
ssh $REMOTE_HOST

# app will hold chain binary
mkdir ~/app
# .push will hold home directory with /config and /data
mkdir ~/.push
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

## Make 1st node
```sh
# upload binary
scp "../build/pchaind" "pn1.dev.push.org:~/app/pchaind"
# upload scripts
scp -r test-push-chain/scripts/* "pn1.dev.push.org:~/app/"
# upload make_first_node.sh
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
CHAIN_ID="push_501-1" MONIKER=pn1 HOME_DIR="~/.pchain" BLOCK_TIME="1000ms" CLEAN=true ./make_first_node.sh
# CTRL-C to stop

# run in the backgr
./start.sh

# check logs
./showLogs.sh
```

## Make 2nd node
```sh
# ----------------- step1: upload files 
# upload binary
scp "../build/pchaind" "pn2.dev.push.org:~/app/pchaind"
# upload scripts
scp -r test-push-chain/scripts/* "pn2.dev.push.org:~/app/"
# ??? upload configs (genesis + toml-s)
scp -r test-push-chain/config/* "pn2.dev.push.org:~/app/config-tmp"

# remote ssh
# set executable
ssh pn2.dev.push.org
chmod u+x ~/app/pchaind
chmod u+x ~/app/*.sh

# ----------------- step2: generate configs
export HOME_DIR="$HOME/.pchain"
# get python3
sudo apt install python3-pip
pip install tomlkit
# setup initial configs & edit toml files with proper values (check the script manually)
~/app/updateConfigs.sh
# set moniker (this is the node name)
python3 $HOME/app/toml_edit.py "$HOME_DIR/config/config.toml" "moniker" "pn2"
# wallet @ url for the initial (seed) nodes to connect to the rest of the network
# note : execute this cmd on the remote node to get it's id : pchaind tendermint show-node-id 
export pn1_url="bc7105d5927a44638ac2ad7f6986ec98dacc5ac6@pn1.dev.push.org:26656"
python3 toml_edit.py $HOME_DIR/config/config.toml "p2p.persistent_peers" "$pn1_url"
# copy genesis.json from the 1st node
cp ~/app/config-tmp/genesis.json "$HOME_DIR/config/"

# ----------------- step3: run (non-validator) blockchain node an sync it
# stop if running
~/app/stop.sh
# start
~/app/start.sh
# check that node is syncing
tail -n 100 ~/app/chain.log
# wait for full node sync (manually or via this script)
~/app/waitFullSync.sh
```

Faucet - Step4
```shell
# show new node id
pchaind tendermint show-node-id

# mint some tokens via Faucet


```
