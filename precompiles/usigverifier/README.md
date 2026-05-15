# `usigverifier` — Universal Signature Verifier Precompile

The only EVM precompile Push Chain ships on top of the cosmos-evm baseline. Verifies Ed25519 signatures inside the EVM so Solidity contracts can authenticate Solana-style signatures (or any other Ed25519 input) without re-implementing the curve in EVM bytecode.

## Addresses

| Address | Why it exists |
|---|---|
| `0x00000000000000000000000000000000000000ca` | Original "legacy" address. Hardcoded into contracts deployed before the address-range cleanup. |
| `0xEC00000000000000000000000000000000000001` | New address in the reserved Push precompile range (`0xEC...`). |

Both addresses are registered simultaneously and point at the **same** implementation. Backward compatibility for previously-deployed contracts is the only reason the legacy address still exists. New code should target `0xEC00000000000000000000000000000000000001`.

Wired into `app/app.go:781-795`:

```go
usigverifierPrecompile, _   := usigverifierprecompile.NewPrecompile()
usigverifierPrecompileV2, _ := usigverifierprecompile.NewPrecompileV2()
corePrecompiles[usigverifierPrecompile.Address()]   = usigverifierPrecompile
corePrecompiles[usigverifierPrecompileV2.Address()] = usigverifierPrecompileV2
```

## Solidity Interface

```solidity
// SPDX-License-Identifier: MIT
pragma solidity >=0.8.18;

address constant USigVerifier_PRECOMPILE_ADDRESS    = 0x00000000000000000000000000000000000000ca;
address constant USigVerifier_PRECOMPILE_ADDRESS_V2 = 0xEC00000000000000000000000000000000000001;

interface IUSigVerifier {
    /// Verifies signature over `"0x" + hex(msgDigest)` (66 ASCII bytes).
    /// Used by UEA_SVM. Solana wallets render the hex string in their sign-message UI.
    function verifyEd25519(bytes calldata pubKey, bytes32 msgDigest, bytes calldata signature)
        external view returns (bool);

    /// Verifies signature over the raw message bytes (standard Ed25519 semantics).
    /// Use this if your signer uses the conventional `ed25519.Sign(privKey, rawBytes)` API.
    function verifyEd25519RawMessage(bytes calldata pubKey, bytes calldata message, bytes calldata signature)
        external view returns (bool);
}
```

| Method | Signed bytes | Gas | Use when |
|---|---|---|---|
| `verifyEd25519(bytes,bytes32,bytes)` | `"0x" + hex(msgDigest)` (66 ASCII bytes) | 4000 | UEA_SVM / Solana-wallet flows where the user signs a hex string in Phantom/Solflare |
| `verifyEd25519RawMessage(bytes,bytes,bytes)` | Raw `message` bytes | 4000 | New integrations / relayers using standard `ed25519.Sign(privKey, rawBytes)` |

Both methods are `view` and touch no chain state.

## Verification Semantics

Two methods, two distinct signing conventions. **A signature produced for one method will not verify under the other** — the test vectors in `query_test.go` lock this in.

### `verifyEd25519` — hex-ASCII convention (legacy / wallet-friendly)

Internally (`query.go:VerifyEd25519`), the `bytes32` `msgDigest` is rendered as a 0x-prefixed hex string before being passed to `ed25519.Verify`:

```go
msgStr := "0x" + hex.EncodeToString(msg)  // 66 ASCII bytes
msgBytes := []byte(msgStr)
ok = ed25519.Verify(pubKeyBytes, msgBytes, signature)
```

The off-chain signer must sign the **66-byte ASCII string** `"0x"+hex(digest)`, not the raw 32 bytes. This is what UEA_SVM uses so that a Solana wallet (Phantom, Solflare) shows the user a copy-pasteable hex string in its sign-message prompt rather than opaque bytes.

### `verifyEd25519RawMessage` — raw-bytes convention (standard)

Standard Ed25519 verification — signature is checked against the raw `message` bytes:

```go
ok = ed25519.Verify(pubKeyBytes, message, signature)
```

Use this when your signer uses `ed25519.Sign(privKey, rawBytes)` (default in every Solana SDK / nacl library). `message` may be any length, not just 32 bytes.

### Common rules

- `pubKey` must be exactly 32 bytes; `signature` must be exactly 64 bytes — otherwise the precompile reverts with `invalid params`.
- Unknown method IDs revert with the standard `unknown method` error.
- Both methods cost `4000` gas.

## Generating the ABI

If `USigVerifier.sol` is changed, regenerate `abi.json` with:

```bash
cd precompiles/usigverifier
solcjs USigVerifier.sol --abi
mv *.abi abi.json
jq --argjson abi "$(cat abi.json)" \
   '{"_format": "hh-sol-artifact-1", "contractName": "USigVerifier",
     "sourceName": "precompiles/USigVerifier.sol",
     "bytecode": "0x", "deployedBytecode": "0x",
     "linkReferences": {}, "deployedLinkReferences": {},
     "abi": $abi}' <<< '{}' > abi.json
```

The Go binary embeds `abi.json` via `//go:embed`, so a fresh `make build` will pick up the change.

## Testing from the Command Line

```bash
# Make sure the precompile is enabled in the EVM params:
# app_state["evm"]["params"]["active_static_precompiles"] must include
# 0x00000000000000000000000000000000000000ca and/or 0xEC00000000000000000000000000000000000001
# (test_node.sh installs the legacy address by default).

cast call 0xEC00000000000000000000000000000000000001 \
    "verifyEd25519(bytes,bytes32,bytes)" \
    "<32-byte pubKey hex>" \
    "<bytes32 digest>" \
    "<64-byte signature hex>"

# Decode the boolean response
cast abi-decode "verifyEd25519(bytes,bytes32,bytes)(bool)" <returndata>
```

If the call returns `0x` (empty), the precompile is not in `active_static_precompiles` for the current chain — that's a configuration issue, not a verification failure.

## Layout

```
precompiles/usigverifier/
|-- USigVerifier.sol     Solidity interface (the source of truth for the ABI)
|-- abi.json             Embedded into the binary via go:embed
|-- usigverifier.go      Precompile struct, NewPrecompile / NewPrecompileV2, RequiredGas, Run
|-- query.go             VerifyEd25519 method handler
+-- README.md            (this file)
```
