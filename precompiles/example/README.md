## Generate ABI encoding

```bash
cd precompiles/example
solc IControlPanel.sol --abi
mv *.abi abi.json
jq --argjson abi "$(cat abi.json)" '{"_format": "hh-sol-artifact-1", "contractName": "IControlPanel", "sourceName": "x/controlpanel/precompiles/IControlPanel.sol", "bytecode": "0x", "deployedBytecode": "0x", "linkReferences": {}, "deployedLinkReferences": {}, "abi": $abi}' <<< '{}' > abi.json
cd ../../
# jq ".abi" abi.json | abigen --abi - --pkg controlpanel --type IControlPanel --out IControlPanel.go
```

## Verification

```bash
# if you just get 0x, make sure the address is in the app_state["evm"]["params"]["active_static_precompiles"]

# precompile directly
cast abi-decode "params()(address,(bool,string[]))" `cast call 0x0000000000000000000000000000000000000901 "getParams()"`


HEX_ADDR=$(pchaind keys parse `pchaind keys show acc1 -a` --output=json | jq -r '.bytes'); echo $HEX_ADDR
cast balance $HEX_ADDR
PRIV_KEY=`pchaind keys unsafe-export-eth-key acc1`; echo $PRIV_KEY


cast send --private-key ${PRIV_KEY} 0x0000000000000000000000000000000000000901 "updateParams(string authority, string admin, bool enabled, string[] validators)" "push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20" "push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20" true "[]"
```
