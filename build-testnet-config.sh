# RUN THIS LINE BY LINE; COMMENTING OUT EVERYTHING ELSE !!!

# OPTIONAL STEP: create keys (write down the memo words)(you need this only once per environment)
pchaind keys add user1
pchaind keys add user2

## make 3 folders with all configs and genesis files and validator keys for
# a testnetwork (tn) of 3 nodes: pushnode1(.tn/pn1), .tn/pn2, .tn/pn3
pchaind init pn1 --home ~/.tn/pn1 --chain-id test-push-chain
pchaind init pn2 --home ~/.tn/pn2 --chain-id test-push-chain
pchaind init pn3 --home ~/.tn/pn3 --chain-id test-push-chain

# build config/genesis.json
### register 2 genesis accounts with 500k push each
# user1 would own nodes pn1,pn2,pn3
export user1=$(pchaind keys show user1 -a)
pchaind genesis add-genesis-account $user1 500000000000npush --home ~/.tn/pn1
# user2 would own nothing
export user2=$(pchaind keys show user2 -a)
pchaind genesis add-genesis-account $user2 500000000000npush --home ~/.tn/pn1

# replace all tokens with npush; npush is nano push; it is 1/1 000 000 of push
sed -i '' 's/stake/npush/g'  ~/.tn/pn1/config/genesis.json


# I cannot register more no matter how hord I try
# this is the command we have in 0.50:
#   pchaind genesis gentx <key_name> <amount> [flags]
# this is the command which I need in 0.52:
#    pchaind genesis gentx <key_name> 10000000000stake  --chain-id push-test-chain --moniker="pn1" --commission-rate="0.10" --commission-max-rate="0.20" --commission-max-change-rate="0.01" --min-self-delegation="1" --ip "192.168.88.114" --node-id <your_node_id> --home ~/.tn/pn2
# register 1 founder validator in genesis.json ;
pchaind genesis gentx user1 10000000000npush --chain-id test-push-chain --home ~/.tn/pn1
# put all txs into genesis.json
pchaind genesis collect-gentxs --home ~/.tn/pn1
# copy genesis to other nodes
cp ~/.tn/pn1/config/genesis.json ~/.tn/pn2/config/genesis.json
cp ~/.tn/pn1/config/genesis.json ~/.tn/pn3/config/genesis.json

## configs

# build config/app.toml
sed -i '' 's/minimum-gas-prices = ""/minimum-gas-prices = "0.25npush"/g' ~/.tn/pn1/config/app.toml
cp ~/.tn/pn1/config/app.toml ~/.tn/pn2/config/app.toml
cp ~/.tn/pn1/config/app.toml ~/.tn/pn3/config/app.toml


# build config/config.toml
export pn1_id=$(pchaind tendermint show-node-id --home ~/.tn/pn1)
export pn1_url="$pn1_id@pn1.dev.push.org:26656"

export pn2_id=$(pchaind tendermint show-node-id --home ~/.tn/pn2)
export pn2_url="$pn2_id@pn2.dev.push.org:26656"

export pn3_id=$(pchaind tendermint show-node-id --home ~/.tn/pn3)
export pn3_url="$pn3_id@pn3.dev.push.org:26656"

export pn1_peers="\"$pn2_url, $pn3_url\""
sed -i '' "s/persistent_peers = \"\"/persistent_peers = $pn1_peers/g" ~/.tn/pn1/config/config.toml
grep -i persistent_peers ~/.tn/pn1/config/config.toml

export pn2_peers="\"$pn1_url, $pn3_url\""
sed -i '' "s/persistent_peers = \"\"/persistent_peers = $pn2_peers/g" ~/.tn/pn2/config/config.toml
grep -i persistent_peers ~/.tn/pn2/config/config.toml

export pn3_peers="\"$pn1_url, $pn2_url\""
sed -i '' "s/persistent_peers = \"\"/persistent_peers = $pn3_peers/g" ~/.tn/pn3/config/config.toml
grep -i "persistent_peers =" ~/.tn/pn3/config/config.toml