# Universal Signature Verifier (USV) Precompile

This is the USV (Universal Signature Verifier) precompile, responsible for verifying cryptographic signatures from supported source chains.

✅ Currently supported signature: **ed25519**

## Generate ABI encoding

```bash
cd precompiles/usv
solc USV.sol --abi
mv *.abi abi.json
jq --argjson abi "$(cat abi.json)" '{"_format": "hh-sol-artifact-1", "contractName": "USV", "sourceName": "precompiles/USV.sol", "bytecode": "0x", "deployedBytecode": "0x", "linkReferences": {}, "deployedLinkReferences": {}, "abi": $abi}' <<< '{}' > abi.json
cd ../../
# jq ".abi" abi.json | abigen --abi - --pkg usv --type USV --out USV.go
```

## Verification

```bash
# if you just get 0x, make sure the address is in the app_state["evm"]["params"]["active_static_precompiles"]

# precompile directly
cast abi-decode "verifyEd25519(bytes,bytes32,bytes)(bool)" `cast call 0x00000000000000000000000000000000000000ca "verifyEd25519(bytes,bytes32,bytes)" \
  "5DgQvTf6BvVs5Y4vNFnB5iXvTQvZah7y2JbT1dFxN6T2" \
  0x68656c6c6f776f726...bytes32_message_here \
  0x6f7c...your_signature_here
`
```
