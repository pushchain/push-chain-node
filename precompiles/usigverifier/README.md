# `usigverifier` — Universal Signature Verifier Precompile

The only EVM precompile Push Chain ships on top of the cosmos-evm baseline. Verifies Ed25519 signatures inside the EVM so Solidity contracts can authenticate Solana-style signatures (or any other Ed25519 input) without re-implementing the curve in EVM bytecode.

## Addresses

| Address | Why it exists |
|---|---|
| `0xEC00000000000000000000000000000000000001` | Reserved Push precompile range (`0xEC...`). |

Registered at `0xEC00000000000000000000000000000000000001`.

Wired into `app/app.go`:

```go
usigverifierPrecompileV2, _ := usigverifierprecompile.NewPrecompile()
corePrecompiles[usigverifierPrecompileV2.Address()] = usigverifierPrecompileV2
```

## Solidity Interface

```solidity
// SPDX-License-Identifier: MIT
pragma solidity >=0.8.18;

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
| `verifyEd25519(bytes,bytes32,bytes)` | `"0x" + hex(msgDigest)` (66 ASCII bytes) | 4000 (flat) | UEA_SVM / Solana-wallet flows where the user signs a hex string in Phantom/Solflare |
| `verifyEd25519RawMessage(bytes,bytes,bytes)` | Raw `message` bytes | `4000 + 12` per 32-byte word of `message` | New integrations / relayers using standard `ed25519.Sign(privKey, rawBytes)` |

Both methods are `view` and touch no chain state.

### Why only the raw method scales with size

`ed25519.Verify` hashes the whole message, so its CPU cost grows with the message
(~58 µs at 32 B, ~146 µs at 128 KiB, ~922 µs at 1 MB). `verifyEd25519` always verifies
the same 66-byte ASCII string no matter what the caller sends, so its cost is constant
and its price stays flat. `verifyEd25519RawMessage` verifies caller-supplied bytes, so
it is priced per 32-byte word — the same per-word rate the EVM `SHA-256` precompile
charges for comparable hashing work.

Because both methods are `view`, a contract can park one large message in memory and
loop `STATICCALL`s over it, paying the calldata only once. Pricing alone is therefore
not the whole defence: `message` is also **hard-capped at 128 KiB**
(`MaxEd25519MessageBytes`), and anything larger reverts with `message too large`
instead of being verified.

| `len(message)` | Gas |
|---|---|
| 0 | 4,000 |
| 32 B | 4,012 |
| 1 KiB | 4,384 |
| 8 KiB | 7,072 |
| 64 KiB | 28,576 |
| 128 KiB (cap) | 53,152 |
| > 128 KiB | reverts |

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

Use this when your signer uses `ed25519.Sign(privKey, rawBytes)` (default in every Solana SDK / nacl library). `message` may be any length up to `MaxEd25519MessageBytes` (128 KiB), not just 32 bytes.

### Common rules

- `pubKey` must be exactly 32 bytes; `signature` must be exactly 64 bytes — otherwise the precompile reverts with `invalid params`.
- `verifyEd25519RawMessage` reverts with `message too large` past `MaxEd25519MessageBytes` (128 KiB).
- Unknown method IDs revert with the standard `unknown method` error.
- `verifyEd25519` costs a flat `4000` gas; `verifyEd25519RawMessage` costs `4000` plus `12` per 32-byte word of `message`.

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
# 0xEC00000000000000000000000000000000000001

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
|-- usigverifier.go      Precompile struct, NewPrecompile / NewPrecompileV2, RequiredGas (gas schedule), Run
|-- query.go             VerifyEd25519 / VerifyEd25519RawMessage method handlers
|-- gas_test.go          Gas-schedule + size-cap regression tests and benchmarks
+-- README.md            (this file)
```
