HDIR_CONFIG=~/.push/config
HDIR_SCRIPTS=~/.push/scripts

read -p "This will re-download genesis.json(yes/no): " answer
if [ "$answer" != "yes" ]; then
    echo "Exiting"
    exit 1
fi

FIRST_NODE=pn1.dev.push.org
NOW=$(date '+%Y%m%d%H%M%S')
echo "Downloading genesis.json from $FIRST_NODE"
cp "$HDIR_CONFIG/genesis.json" "$HDIR_CONFIG/genesis.json.$NOW"
curl http://$FIRST_NODE:26657/genesis | jq '.result.genesis' > ~/.app/config/genesis.json
cat $HOME/.push/config/genesis.json
echo 'genesis.json generated '

exit 0


#### prepare configs
# EDIT THIS FILE PER NODE or use nano to edit specific configs manually

# edit app.toml : set min gas price
cp $HDIR_CONFIG/app.toml.sample $HDIR_CONFIG/app.toml
python3 $HDIR_SCRIPTS/toml_edit.py $HDIR_CONFIG/app.toml "minimum-gas-prices" "0.25npush"

# edit config.toml : set human readable node name
cp $HDIR_CONFIG/config.toml.sample $HDIR_CONFIG/config.toml
python3 $HDIR_SCRIPTS/toml_edit.py $HDIR_CONFIG/config.toml "moniker" "$VALIDATOR_NAME"

# edit config.toml: set persistent peers
# !! this is id of the validator1,
# check by "pushchaind tendermint show-node-id --home test-push-chain-0"  or "pushchaind tendermint show-node-id" on pn1
export pn1_id=a1ba93b69fb0ff339909fcd502d404d6e4b9c422
export pn1_url="$pn1_id@pn1.dev.push.org:26656"
python3 $HDIR_SCRIPTS/toml_edit.py $HDIR_CONFIG/config.toml "p2p.persistent_peers" "$pn1_url"
