## Generate ABI encoding

```bash
cd precompiles/verifier
solc ISolVerifier.sol --abi
mv *.abi abi.json
jq --argjson abi "$(cat abi.json)" '{"_format": "hh-sol-artifact-1", "contractName": "ISolVerifier", "sourceName": "x/solverifier/precompiles/ISolVerifier.sol", "bytecode": "0x", "deployedBytecode": "0x", "linkReferences": {}, "deployedLinkReferences": {}, "abi": $abi}' <<< '{}' > abi.json
cd ../../
# jq ".abi" abi.json | abigen --abi - --pkg solverifier --type ISolVerifier --out ISolVerifier.go
```

## Verification

```bash
# if you just get 0x, make sure the address is in the app_state["evm"]["params"]["active_static_precompiles"]

# precompile directly
cast abi-decode "verifyEd25519(bytes,bytes32,bytes)(bool)" `cast call 0x0000000000000000000000000000000000000901 "verifyEd25519(bytes,bytes32,bytes)" \
  "5DgQvTf6BvVs5Y4vNFnB5iXvTQvZah7y2JbT1dFxN6T2" \
  0x68656c6c6f776f726...bytes32_message_here \
  0x6f7c...your_signature_here
`
```
