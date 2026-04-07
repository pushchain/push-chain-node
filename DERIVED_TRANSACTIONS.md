# Derived Transactions

A primitive added in Push Chain's EVM fork ([`github.com/pushchain/evm`](https://github.com/pushchain/evm), pinned via `replace` in `go.mod`) that lets a Cosmos SDK module produce a **real EVM transaction** — one that has a real receipt, real logs, and is fully observable through the JSON-RPC layer — instead of an internal "module call" that exists only inside the SDK.

The new EVM keeper method is `DerivedEVMCall`. Everywhere in the Push Chain codebase that needs to act on the EVM as a Cosmos module (mint PRC20s, write chain-meta, deploy a UEA, refund gas, ...) goes through this single entry point.

## Why It Exists

Stock cosmos-evm exposes `EVMKeeper.CallEVM`:

```go
func (k Keeper) CallEVM(
    ctx sdk.Context,
    abi abi.ABI,
    from, contract common.Address,
    commit bool,
    method string,
    args ...interface{},
) (*types.MsgEthereumTxResponse, error)
```

`CallEVM` is built for **internal queries**: a Cosmos module wants to read state from a contract or trigger a side effect, and the EVM layer treats it as a synthetic call. It's enough for read paths and lightweight writes, but it has hard limitations the moment a module needs to behave like a first-class EVM sender:

| Need | `CallEVM` |
|---|---|
| Send native value (`msg.value`) | not supported (always 0) |
| Set an explicit `gasLimit` | not supported |
| Bypass gas accounting for module-initiated work | not supported |
| Act as a module account (no private key) sending a real EVM tx | not supported |
| Issue multiple calls in the same block from the same sender without nonce collisions | not supported (nonce is read from state on every call) |
| Produce a JSON-RPC-visible receipt with hash, gas used, and logs | partial — the call exists, but doesn't surface as a normal EVM tx |

`DerivedEVMCall` is the fork's answer to all six.

## The API

```go
DerivedEVMCall(
    ctx sdk.Context,
    abi abi.ABI,
    from, contract common.Address,
    value, gasLimit *big.Int,
    commit, gasless, isModuleSender bool,
    manualNonce *uint64,
    method string,
    args ...interface{},
) (*types.MsgEthereumTxResponse, error)
```

Defined on the Push Chain `EVMKeeper` interface in [`x/uexecutor/types/expected_keepers.go`](./x/uexecutor/types/expected_keepers.go).

| Parameter | Purpose |
|---|---|
| `ctx` | SDK context — provides block, gas meter, store access |
| `abi` | Parsed contract ABI for encoding the call |
| `from` | The EVM address that will appear as the tx sender. Can be a derived user address or a module account address. |
| `contract` | Destination contract |
| `value` | Native value to attach (`*big.Int`, may be `nil` or `big.NewInt(0)`) |
| `gasLimit` | Explicit gas limit (`nil` -> use a sensible default). Critical for predictable receipts. |
| `commit` | `true` = real state-changing tx; `false` = simulation / static call |
| `gasless` | `true` = skip gas accounting entirely. Used when the call is initiated by the protocol itself and shouldn't bill any user. |
| `isModuleSender` | `true` = `from` is a Cosmos module account (no private key). The fork's signer logic uses a deterministic synthetic signature instead of requiring a real ECDSA signature. |
| `manualNonce` | If non-`nil`, the caller supplies the nonce explicitly. This is what makes "many EVM calls in one block from the same module" deterministic — see [Manual Nonce Management](#manual-nonce-management). |
| `method` + `args` | Standard ABI-encoded call data |

The return type is `*evmtypes.MsgEthereumTxResponse`, the same type a normal `MsgEthereumTx` produces. Concretely:

```go
receipt, err := k.evmKeeper.DerivedEVMCall(...)
// receipt.Hash    -- 0x... tx hash, queryable via eth_getTransactionByHash
// receipt.GasUsed -- real gas used, observable in receipts
// receipt.Logs    -- real EVM logs, indexable by event subscribers
// receipt.Ret     -- ABI-encoded return data (for view-style commits)
```

## When to Use Each Mode

The Push Chain codebase uses two distinct call patterns. Both are visible in [`x/uexecutor/keeper/evm.go`](./x/uexecutor/keeper/evm.go).

### 1. User-derived sender (UEA-routed user actions)

When a user submits a `MsgExecutePayload`, the Cosmos signer is converted to its derived EVM address and the EVM call is issued from that address. The UEA contract is what authenticates the request via `verificationData`. UEA migration takes the same path — there is no separate migration message; an upgrade is just an `executePayload` whose payload calls the UEA's migration entry point.

```go
return k.evmKeeper.DerivedEVMCall(
    ctx,
    abi,
    evmFromAddress,    // user's derived EVM address
    ueaAddr,
    big.NewInt(0),
    gasLimit,
    true,              // commit
    false,             // gasless = false (real user tx, gas should appear in receipt)
    false,             // isModuleSender = false
    nil,               // manualNonce = nil (read from state like a normal user)
    "executeUniversalTx",
    abiUniversalPayload,
    verificationData,
)
```

Why not `CallEVM`? Two reasons:
- Real receipts. Universal Validators, indexers, and the JSON-RPC layer all need to see the tx as a normal Ethereum tx so they can observe gas used, status, and emitted events.
- Explicit `gasLimit`. The payload's gas budget must be enforceable; `CallEVM` doesn't accept one.

### 2. Module-as-sender (protocol-initiated EVM work)

When `x/uexecutor` itself needs to issue an EVM call (deposit PRC20s, push chain-meta, refund unused gas, ...) the sender is the `uexecutor` module account. Module accounts don't have private keys, so this would be impossible via a normal `MsgEthereumTx` — you can't sign one. `DerivedEVMCall` with `isModuleSender=true` solves it:

```go
ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)
nonce, _ := k.GetModuleAccountNonce(ctx)
_, _ = k.IncrementModuleAccountNonce(ctx)

return k.evmKeeper.DerivedEVMCall(
    ctx,
    abi,
    ueModuleAccAddress, // module account as sender
    handlerAddr,
    big.NewInt(0),
    nil,
    true,               // commit
    false,              // gasless = false (we still want gas in the receipt)
    true,               // isModuleSender = true
    &nonce,             // manualNonce = explicit
    "depositPRC20Token",
    prc20Address, amount, to,
)
```

The fork is responsible for synthesising a deterministic "signature" for the module account so the tx can be properly receipted and indexed without ever needing a real key to exist.

## Manual Nonce Management

Stock cosmos-evm reads the sender's nonce from EVM state on every call. That's fine for users (one user = one tx in flight at a time, the mempool serializes the rest), but it breaks for module accounts that may need to issue **several** EVM calls within the same block:

```
BeginBlock
  uexecutor.handleInbound1
    -> CallPRC20Deposit         (nonce = ?)
    -> CallUniversalCoreRefundUnusedGas (nonce = ?)
  uexecutor.handleInbound2
    -> CallPRC20DepositAutoSwap (nonce = ?)
EndBlock
```

If the keeper read the nonce from state for each of these, every call within the same block would see the same starting nonce — and they'd all collide. The fork's solution is the `manualNonce *uint64` argument: the caller passes its own counter, the fork honours it, and is responsible for incrementing it before the next call.

`x/uexecutor` keeps that counter in its own KV store as the `ModuleAccountNonce` collection ([`x/uexecutor/keeper/keeper.go`](./x/uexecutor/keeper/keeper.go)):

```go
nonce, err := k.GetModuleAccountNonce(ctx)   // read
if _, err := k.IncrementModuleAccountNonce(ctx); err != nil {
    return nil, err
}
// pass &nonce to DerivedEVMCall
```

The increment happens **before** the call, intentionally — if the EVM call fails, the nonce gap is benign (skipped nonces are fine in EVM), but a post-call increment would risk reusing a nonce on retry. This pre-increment is the canonical way to issue derived txs from a module.

> ⚠️ **Single source of truth.** Only one collection in the whole codebase should ever increment `ModuleAccountNonce`. If two modules need to send derived txs as the same module account, they must coordinate through a single keeper helper. The current design has only `x/uexecutor` doing this, so the invariant holds trivially.

## The `gasless` Flag

`gasless=true` tells the fork: "this call is part of internal protocol bookkeeping, don't bill any account for the gas." Right now, every Push Chain call site passes `gasless=false`, with the inline comment:

> `// gasless = false (@dev: we need gas to be emitted in the tx receipt)`

The reason: even though the protocol pays the gas, the tx receipt still needs `gas_used` populated so off-chain services (Universal Validators, explorers, the gas-fee accounting in `x/uexecutor`) can read it back. Setting `gasless=true` would suppress the gas field and break that read path.

The flag exists for future use — protocol housekeeping calls that don't need to be observable via receipts (e.g. genesis-time bytecode patches). For day-to-day inbound/outbound execution, `gasless` stays `false`.

## Where It's Used

Every derived call in Push Chain is in [`x/uexecutor/keeper/evm.go`](./x/uexecutor/keeper/evm.go). Quick map:

| Helper | Sender | Why derived? |
|---|---|---|
| `CallFactoryToDeployUEA` | user-derived | Real tx receipt is required for the deploy; the deployer address is the source-chain user's derived EVM address. |
| `CallUEAExecutePayload` | user-derived | Carries `gasLimit` from the payload; receipt is consumed by the Universal Validator vote-back path. UEA migration also flows through this path now (the migration is just a payload that calls the UEA's migrate entry point). |
| `CallPRC20Deposit` | module | Mints PRC20 to recipient. Module account has no key. |
| `CallPRC20DepositAutoSwap` | module | Same, but with the auto-swap leg. |
| `CallUniversalCoreSetGasPrice` | module | Writes a single chain's gas price to the on-chain oracle. |
| `CallUniversalCoreSetChainMeta` | module | Writes gas price + block height for a chain. |
| `CallUniversalCoreRefundUnusedGas` | module | Refunds unused gas (with optional swap back to PC). |
| `CallExecuteUniversalTx` | module | Calls `executeUniversalTx` on a recipient smart contract for `isCEA` inbounds. |

The pure read paths in the same file (`CallFactoryToGetUEAAddressForOrigin`, `CallFactoryGetOriginForUEA`, `CallUEADomainSeparator`, `GetGasPriceByChain`, `GetUniversalCoreQuoterAddress`, `GetUniversalCoreWPCAddress`, `GetDefaultFeeTierForToken`, `GetSwapQuote`) all use plain `CallEVM` with `commit=false` — they don't need a receipt because they're static.

## Quick Reference: `CallEVM` vs `DerivedEVMCall`

```
                          CallEVM                  DerivedEVMCall
                          -------                  ---------------
value                     0 (implicit)             explicit *big.Int
gasLimit                  default                  explicit *big.Int (or nil)
commit                    yes                       yes
gasless                   no (always charges)      flag (default: false in PC)
isModuleSender            no                        flag (true = synthetic signer)
manualNonce               no (read from state)     optional override
JSON-RPC visible receipt  partial                  yes — same as a user MsgEthereumTx
typical use               internal queries,        protocol-as-sender writes,
                          lightweight side effects user-derived EVM-routed actions
```

## Caveats

- **`isModuleSender=true` requires the synthetic signer logic in the fork.** If the upstream cosmos-evm version is bumped, that signer path must remain intact, otherwise module-originated derived calls will fail validation.
- **`manualNonce` is the caller's responsibility.** The fork trusts the supplied value verbatim. Two callers stomping each other's nonce will cause receipt collisions and confusing replays.
- **Pre-increment, never post-increment.** If you increment after the call and the call panics or errors mid-execution, you've now reused a nonce. Always increment first; treat skipped nonces as a non-issue (EVM allows nonce gaps for module accounts since no transaction sequencing depends on them).
- **`gasless=true` suppresses the gas field in the receipt.** Until there's a clear reason to drop receipts on the floor for a particular call site, leave it `false`.
