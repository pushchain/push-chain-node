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
    /// @notice Verifies an Ed25519 signature.
    /// @param pubKey    The 32-byte Ed25519 public key (Solana address bytes).
    /// @param msg       The message digest that was signed (bytes32).
    /// @param signature The 64-byte Ed25519 signature.
    /// @return isValid  True iff the signature is valid for (pubKey, msg).
    function verifyEd25519(
        bytes calldata pubKey,
        bytes32 msg,
        bytes calldata signature
    ) external view returns (bool);
}
```

| Property | Value |
|---|---|
| Method | `verifyEd25519(bytes,bytes32,bytes)` |
| State mutability | `view` (no on-chain state is touched) |
| Gas cost | `4000` per call (`VerifyEd25519Gas` in `usigverifier.go`) |

## Verification Semantics

The precompile is intentionally narrow. It accepts:

- `pubKey` — 32 raw Ed25519 public key bytes (a Solana address is exactly this)
- `msg` — a single `bytes32` digest
- `signature` — 64 raw Ed25519 signature bytes

Internally (`query.go:VerifyEd25519`), the `bytes32` digest is **rendered as a 0x-prefixed hex string** before being passed to `ed25519.Verify`:

```go
msgStr := "0x" + hex.EncodeToString(msg)  // 66 ASCII bytes
msgBytes := []byte(msgStr)
ok = ed25519.Verify(pubKeyBytes, msgBytes, signature)
```

In other words, the signed message that the off-chain signer must sign is the **66-byte ASCII string** `0x...` of the digest, not the raw 32 bytes. This matches the convention used by Solana wallets when signing arbitrary messages — they prefix-encode the payload — so a normal Solana wallet signature over a Push Chain message hash will verify here without any extra work on the wallet side.

If `pubKey` is not 32 bytes or `signature` is not 64 bytes, the precompile reverts with `invalid params`. Unknown method IDs revert with the standard `unknown method` error.

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
