#!/bin/bash
(cd ~ && nohup ~/app/pchaind start --pruning=nothing  --minimum-gas-prices=1000000000upc --rpc.laddr="tcp://0.0.0.0:26657" --json-rpc.api=eth,txpool,personal,net,debug,web3,trace --json-rpc.address="0.0.0.0:8545" --json-rpc.ws-address="0.0.0.0:8546" --chain-id="push_42101-1" --trace --evm.tracer=json --home ~/.pchain >> ~/app/chain.log 2>&1 &)
