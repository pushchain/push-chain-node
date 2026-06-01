# `x/uexecutor` — Universal Transaction Executor

The execution layer for Push Chain's crosschain protocol. Owns the lifecycle of every `UniversalTx` (UTX) — from inbound observation through Push Chain execution to outbound completion — and is the only module that drives the EVM-side universal contracts (UEA factory, gateway PC, chain meta oracle).

## What It Does

- **Tally inbound votes** from Universal Validators (UVs). Once 2/3+ vote the same observation, finalize the inbound and execute it on Push Chain (deposit funds, run the user's payload through their UEA).
- **Track pending outbounds** created as a side-effect of Push Chain execution, and tally UV votes on whether they were successfully broadcast on the destination chain (or have permanently failed and need a refund).
- **Maintain the chain meta oracle** (gas price + block height per external chain) by tallying votes from UVs and writing the result back to the EVM so contracts can read it.
- **Issue derived EVM calls** as the `uexecutor` module account, with a manually managed nonce, so the module can deploy and call universal contracts on behalf of itself.

## State (KV layout)

| Prefix | Collection | Type | Purpose |
|---|---|---|---|
| `0` | `Params` | `Item[Params]` | Module parameters |
| `2` | `PendingInbounds` | `Map[string, PendingInboundEntry]` | In-flight inbounds with full per-variant audit trail. Key = `sha256(sourceChain:txHash:logIndex)` |
| `3` | `UniversalTx` | `Map[string, UniversalTx]` | Canonical UTX record. Key = `sha256(sourceChain:txHash:logIndex)` |
| `4` | `ModuleAccountNonce` | `Item[uint64]` | Manual nonce for `DerivedEVMCall` from the module account |
| `5` | `GasPrices` | `Map[string, GasPrice]` | **Deprecated** — replaced by `ChainMetas`, kept only for genesis import |
| `6` | `ChainMetas` | `Map[string, ChainMeta]` | Aggregated gas price + block height per CAIP-2 chain |
| `7` | `PendingOutbounds` | `Map[string, PendingOutboundEntry]` | Outbounds in `PENDING` status, with per-variant audit trail of validator votes |
| `8` | `ExpiredInbounds` | `Map[string, ExpiredInboundEntry]` | Per-variant audit trail of inbounds whose ballots all reached EXPIRED/REJECTED without producing a UTX. Consumed by future escape-hatch refund flow. |

## The `UniversalTx` Record

`UniversalTx` (UTX) is the canonical, end-to-end record of a single crosschain transaction as it travels through Push Chain. One UTX is created per observed inbound and lives forever (it is never deleted, only mutated as new pieces of evidence arrive). It is the only object in the module that the rest of the protocol — Universal Validators, the JSON-RPC layer, indexers, the explorer — needs to read in order to know what's happening with a given crosschain action.

```protobuf
message UniversalTx {
  string              id           = 1;  // sha256(sourceChain:txHash:logIndex)
  Inbound             inbound_tx   = 2;  // the source-chain observation that opened this UTX
  repeated PCTx       pc_tx        = 3;  // every Push Chain execution this UTX produced
  repeated OutboundTx outbound_tx  = 4;  // every outbound this UTX spawned (and their results)
  string              revert_error = 6;  // non-empty if revert-outbound attachment failed
}
```

The UTX is intentionally append-mostly. Components are filled in over time as the protocol progresses; nothing is overwritten. Field `5` is reserved (a removed `UniversalTxStatus` enum field — see below for why status is computed instead of stored).

### The Three Components

#### 1. `Inbound` — the source-chain observation

Filled in once, when the inbound vote is finalized. After that, it is read-only.

```protobuf
message Inbound {
  string source_chain        = 1;  // CAIP-2, e.g. "eip155:11155111"
  string tx_hash             = 2;  // unique source-chain tx hash
  string sender              = 3;  // source-chain sender address
  string recipient           = 4;  // destination address on Push Chain (UEA or contract)
  string amount              = 5;  // bridged amount (synthetic token, uint256 as string)
  string asset_addr          = 6;  // source-chain ERC20 / native token address
  string log_index           = 7;  // log index that emitted this inbound (uniqueness within tx)
  TxType tx_type             = 8;  // see TxType table below
  UniversalPayload universal_payload = 9;  // the user's intent (decoded from raw_payload)
  string verification_data   = 10; // bytes the UEA uses to authenticate the payload
  RevertInstructions revert_instructions = 11;  // where funds go on revert
  bool   isCEA               = 12; // recipient is a contract (CEA) instead of a UEA
  string raw_payload         = 13; // hex-encoded raw event bytes (decoded by core validator)
}
```

#### 2. `PCTx` — Push Chain execution

A list, because a single inbound can spawn multiple Push Chain executions (the deposit tx, the payload-execution tx, and possibly a revert tx all live as separate `PCTx` entries on the same UTX).

```protobuf
message PCTx {
  string tx_hash      = 1;  // hash of the EVM tx the core validator produced (DerivedEVMCall)
  string sender       = 2;  // who initiated it (user-derived address, or uexecutor module)
  uint64 gas_used     = 3;  // populated from the tx receipt
  uint64 block_height = 4;  // Push Chain block this was committed in
  string status       = 6;  // "SUCCESS" or "FAILED"
  string error_msg    = 7;  // populated when status == "FAILED"
}
```

These hashes correspond to real EVM transactions you can fetch from `eth_getTransactionByHash` — see [`DERIVED_TRANSACTIONS.md`](../../DERIVED_TRANSACTIONS.md) for why module-originated calls produce real receipts.

#### 3. `OutboundTx` — outbounds spawned by Push Chain execution

A list, because one inbound's payload can fan out into multiple destination-chain transactions (e.g. a multi-hop cross-chain swap or a batched refund).

```protobuf
message OutboundTx {
  string destination_chain    = 1;  // CAIP-2 of the destination
  string recipient            = 2;
  string amount               = 3;
  string external_asset_addr  = 4;
  string prc20_asset_addr     = 5;
  string sender               = 6;
  string payload              = 7;
  string gas_limit            = 8;
  TxType tx_type              = 9;
  OriginatingPcTx pc_tx       = 10; // which PCTx (and log) created this outbound
  OutboundObservation observed_tx = 11; // populated once UVs vote the destination-chain result
  string id                   = 12; // deterministic outbound ID
  Status outbound_status      = 13; // PENDING -> OBSERVED | REVERTED | ABORTED
  RevertInstructions revert_instructions = 14;
  PCTx   pc_revert_execution  = 15; // PC tx that ran the revert path (nil if not reverted)
  string gas_price            = 16; // destination-chain gas price snapshot
  string gas_fee              = 17; // amount paid to relayer
  PCTx   pc_refund_execution  = 18; // PC tx that ran the unused-gas refund (nil if no refund)
  string refund_swap_error    = 19; // non-empty if the swap-refund leg failed
  string gas_token            = 20; // PRC20 used to pay relayer
  string abort_reason         = 21; // human-readable reason if outbound was aborted
}
```

`OutboundObservation` is what UVs vote in via `MsgVoteOutbound`:

```protobuf
message OutboundObservation {
  bool   success      = 1;
  uint64 block_height = 2;
  string tx_hash      = 3;
  string error_msg    = 4;
  string gas_fee_used = 5;  // actual gas spent on destination — used to compute refund
}
```

### `TxType` — what flavour of crosschain action

The same enum is used on both `Inbound` and `OutboundTx` to describe what the message is for.

| `TxType` | Inbound semantics | Outbound semantics |
|---|---|---|
| `GAS` | User pre-paid gas on the source chain. Mints PC to the recipient as a gas top-up. | Refund of unused gas back to a source chain. |
| `GAS_AND_PAYLOAD` | Gas top-up + executes a payload through the recipient's UEA in the same Push Chain tx. | Same combo on the destination side. |
| `FUNDS` | Pure synthetic transfer — mints PRC20 representation of an external token. | Pure transfer of a PRC20 back out of Push Chain. |
| `FUNDS_AND_PAYLOAD` | Mints funds + runs a payload (e.g. deposit + DEX swap atomically). | Funds delivery with a destination-side call. |
| `PAYLOAD` | Pure payload execution, no value movement. | Pure call on the destination chain. |
| `INBOUND_REVERT` | Reverts a previously-executed inbound (returns funds to the source-chain sender). | — |
| `RESCUE_FUNDS` | Admin-driven rescue path for stuck funds. | Outbound that delivers the rescue. |

### Status is derived from component state, not stored

The current `UniversalTx` record has **no status field at all**. Field `5` is reserved precisely because the old `UniversalTxStatus` enum field was removed in favour of computing status on the fly from the underlying components. This avoids the staleness class of bugs where a stored status gets out of sync with the actual outbounds/PC txs after a partial update.

Instead, callers ask "what's the state of this UTX?" by inspecting:

- whether `OutboundTx[]` is non-empty, and the per-entry `outbound_status` (`PENDING` / `OBSERVED` / `REVERTED` / `ABORTED`)
- whether `PcTx[]` is non-empty, and each entry's `status` string (`"SUCCESS"` / `"FAILED"`)
- whether `InboundTx` is set

The priority for any rollup view is **outbounds > PC txs > inbound presence**: as soon as an outbound exists, the UTX is "in the outbound phase" regardless of how the PC txs went; before that, PC tx state dominates; before that, the UTX is just a recorded inbound waiting to be executed.

> **Note on `UniversalTxStatus` (legacy enum).** The `UniversalTxStatus` proto enum (`PENDING_INBOUND_EXECUTION`, `PC_EXECUTED_SUCCESS`, `OUTBOUND_PENDING`, ...) is **only** used by the legacy query response shape `UniversalTxLegacy`. The v1 `GetUniversalTx` query converts the current record into `UniversalTxLegacy` and synthesises the status field via `computeUniversalStatus` in `keeper/query_server.go` purely for client backward compatibility. Anything new built against `x/uexecutor` should consume the live components on `UniversalTx` directly and compute the status it cares about, instead of depending on the legacy enum.

### `Status` — per-outbound status

`OutboundTx.outbound_status` uses a separate, narrower enum:

| `Status` | Meaning |
|---|---|
| `PENDING` | Outbound created on Push Chain, waiting for UVs to broadcast and vote |
| `OBSERVED` | UVs voted the outbound was successfully broadcast on the destination chain |
| `REVERTED` | UVs voted the outbound permanently failed; revert path triggered |
| `ABORTED` | Finalization or revert attachment failed and requires manual intervention |

### Lifecycle Walkthrough

A typical `FUNDS_AND_PAYLOAD` inbound, end to end:

```
1. UV observes a source-chain gateway event.
2. UV submits MsgVoteInbound. The UTX is created the moment the first vote
   arrives, with id = sha256(sourceChain:txHash:logIndex). Only the
   InboundTx field is populated; PcTx and OutboundTx are empty.
   (UTX id is also added to PendingInbounds.)

3. Threshold of UV votes reached. The keeper executes the inbound:
   a. Mints the PRC20 to the recipient's UEA address.
      A new PCTx (deposit) is appended to UTX.PcTx.
   b. Runs the universal payload through the UEA.
      A second PCTx (executeUniversalTx) is appended.
   (UTX id removed from PendingInbounds.)

4. The payload triggered a destination-chain call (e.g. release funds on
   another chain). An OutboundTx is created with Status_PENDING and
   appended to UTX.OutboundTx. It is also indexed in PendingOutbounds.

5. UVs sign the outbound via TSS, broadcast it, and vote the result back
   via MsgVoteOutbound. The OutboundTx.observed_tx is filled in and
   outbound_status flips to OBSERVED. The PendingOutbounds entry is
   removed.

6. If the destination chain refunds excess gas, a refund PCTx runs on
   Push Chain. PCTx.pc_refund_execution is set on the OutboundTx. The
   refund is just additional evidence attached to the existing OutboundTx.
```

At every step the UTX is mutated **append-only**: new entries are added to `pc_tx` and `outbound_tx`, existing entries are updated in place, and the live state of those slices is the only source of truth for "what's happening" with this UTX.

## Messages (`MsgServer`)

| Message | Authority | Gasless? | Purpose |
|---|---|---|---|
| `MsgVoteInbound` | bonded UV | yes | Vote an observed source-chain inbound |
| `MsgVoteOutbound` | bonded UV | yes | Vote that an outbound was broadcast (or failed) on the destination chain |
| `MsgVoteChainMeta` | bonded UV | yes | Vote on observed gas price + block height for a chain |
| `MsgExecutePayload` | any | yes | Execute a payload on a UEA (the UEA itself authenticates via `verificationData`) |
| `MsgUpdateParams` | gov | no | Update module params |

> **UEA migration is now part of payload execution.** There used to be a separate `MsgMigrateUEA` message; that path has been removed. UEAs are upgraded by submitting a normal `MsgExecutePayload` whose payload calls the UEA's migration entry point on the EVM side. The Cosmos layer no longer has a dedicated migration message — the UEA contract is the source of truth for who is allowed to migrate it and to what implementation.

Vote messages check `IsBondedUniversalValidator` and `IsTombstonedUniversalValidator` on `x/uvalidator` before accepting the vote. Tombstoned validators are silently rejected.

### Authorization model for `MsgExecutePayload` (contract-only binding)

`MsgExecutePayload` follows a **contract-only binding** authorization model. The Cosmos signer of the message and the owner of the target Universal Account are intentionally distinct roles:

- **`Signer`** identifies the Cosmos transaction signer — the party that delivers the owner's pre-authorized payload to Push Chain. `MsgExecutePayload` is a gasless message type (see `app/txpolicy/gasless.go`), so the signer pays no Cosmos transaction fee. Any account may submit the message.
- **`UniversalAccountId.Owner`** identifies the UEA whose pre-authorized payload is being executed. The actual EVM execution gas is deducted from this UEA;s balance (`DeductGasFeesFromReceipt`), not from the signer.

**The chain module deliberately does not enforce `Signer == EVM(Owner)`.** If it did, third-party delivery of owner-signed payloads would be impossible — every owner would have to submit their own Cosmos transactions even though the chain charges them no Cosmos fee for doing so, defeating the cross-chain UX promise of letting an external account act on Push Chain through delivered payloads.

#### Where authorization actually lives

The cryptographic binding is enforced inside the UEA contract's `executeUniversalTx` (see [`UEA_EVM.sol`](https://github.com/pushchain/push-chain-core-contracts/blob/86e20e2d26819e7cc885549f08c66895221dfab0/src/uea/UEA_EVM.sol#L145) and [`UEA_SVM.sol`](https://github.com/pushchain/push-chain-core-contracts/blob/86e20e2d26819e7cc885549f08c66895221dfab0/src/uea/UEA_SVM.sol)):

1. The contract holds the owner's public key as **immutable bytes** set at UEA deployment via `initialize(_id, _factory)`. There is no code path that mutates this after init.
2. `executeUniversalTx(payload, signature)` verifies the `signature` (passed in as `MsgExecutePayload.VerificationData`) against this stored owner — ECDSA recovery for EVM-origin owners, the Ed25519 precompile (`0x00…00ca`) for SVM-origin owners.
3. The signed payload hash includes a contract-tracked `nonce` (monotonic per UEA) and optional `deadline`, providing replay and freshness protection.
4. If signature verification fails, the contract reverts. The revert propagates as `execErr` from `CallUEAExecutePayload`; the keeper returns the error from `ExecutePayload`; the entire Cosmos transaction (including any partial gas-fee deduction) rolls back atomically. **No state changes survive a failed signature check.**

#### Why this is safe under `Signer ≠ Owner`

An attacker submitting `MsgExecutePayload` with their own `Signer` and a victim's `UniversalAccountId` produces no exploitable outcome:

- The factory resolves the victim's UEA address from the embedded `UniversalAccountId` — correct.
- `evmFrom` (derived from `Signer`) becomes the EVM-level `msg.sender` of the call to the UEA. Since `evmFrom != UNIVERSAL_EXECUTOR_MODULE` (`0x14191Ea54B4c176fCf86f51b0FAc7CB1E71Df7d7`), the contract enforces the signature check.
- The attacker cannot forge `VerificationData` that recovers to the victim's owner key.
- The contract reverts → the keeper returns an error → the Cosmos transaction reverts in full.
- Net effect: zero state change. No EVM gas is charged to the victim UEA (the deduction is rolled back with the rest of the transaction). The submission costs the attacker nothing on chain (gasless), but also achieves nothing.

## Pending-inbound and pending-outbound lifecycle

`PendingInbounds` and `PendingOutbounds` are intentionally asymmetric — they
represent two different things and have different lifecycle invariants.

### `PendingInbounds`

- **Created** by the FIRST validator vote on a given inbound (`RecordInboundVote`
  inside `VoteInbound`). The chain learns about the source-chain event from
  validator observations.
- **Keyed** by `utx_key = sha256(source_chain:tx_hash:log_index)`.
- **Variant-aware:** when validators marshal slightly different `Inbound` bytes
  for the same logical event (different decoded fields, formatting, etc.), each
  unique payload becomes its own `InboundVariant` inside the entry, with its
  own `ballot_id`, `voters[]`, and `terminal_status`.
- **Removed** when ALL related ballot variants reach a terminal state. If any
  variant ended `PASSED`, the existing post-finalization path in `VoteInbound`
  produced a `UniversalTx`. If ALL variants ended `EXPIRED`/`REJECTED`, the
  full per-variant audit trail is moved to `ExpiredInbounds` for the future
  escape-hatch refund flow.
- The cleanup-on-terminal logic lives in `keeper/ballot_hooks.go` (the
  `BallotHooks` impl wired into `x/uvalidator`).

### `PendingOutbounds`

- **Created** by chain code at outbound creation in `create_outbound.go` —
  BEFORE any validator vote. The chain knows the outbound exists because it
  generated the destination-chain transaction itself; validators are tasked
  with observing whether/how it landed.
- **Keyed** by deterministic chain-derived `outbound_id`.
- **Variant-aware:** validator votes append `OutboundObservationVariant`s as
  they arrive (`RecordOutboundVote` inside `VoteOutbound`). Multiple variants
  per outbound indicate validator divergence on the destination-chain
  observation (different `success`/`tx_hash`/`error_msg`/`gas_fee_used`).
- **Removed ONLY when validators reach consensus** (existing inline
  `PendingOutbounds.Remove` in `msg_vote_outbound.go` on `PASSED`).
- **Ballot expiry does NOT remove the entry** — this is intentional. The
  destination chain already received (or did not receive) the outbound; the
  user's funds are already in flight. Auto-refund risks double-pay (if the
  outbound actually landed), auto-retry risks double-delivery, and there is
  no safe automatic resolution. Operators investigate stuck outbounds via
  the per-variant audit trail (which validators voted what observation) plus
  separate `x/uvalidator` ballot status queries; resolution is governance-
  driven, not chain-driven.

## Queries

- `Params`
- `GetUniversalTx` — fetch a single UTX by ID. The v1 endpoint returns the legacy `UniversalTxLegacy` shape (with a synthesised `UniversalTxStatus` for backward compatibility); the v2 endpoint returns the live `UniversalTx` directly.
- v2 query server (`query_server_v2.go`) provides additional iterators over UTX state

See `keeper/query_server.go` and `keeper/query_server_v2.go` for the full surface.

## Inter-module Dependencies

The keeper holds references to:
- `evmKeeper` — for `DerivedEVMCall` (deploy contracts, mint, refund, push chain meta)
- `feemarketKeeper` — for current Push Chain gas price
- `bankKeeper` — for native transfers
- `accountKeeper` — for the `uexecutor` module account
- `uregistryKeeper` — to look up chain configs and token configs
- `uvalidatorKeeper` — to gate votes on bonded/tombstoned status, and to drive the generic ballot machine

It does not export any hooks; other modules call into it (not the other way around).

## EVM Integration

`x/uexecutor` is unusual in that it issues EVM calls as a Cosmos module. On fresh genesis (`Exported=false`) it deploys the **UEA factory** contract. Thereafter, every inbound execution, refund, swap quote, and chain-meta update flows through `DerivedEVMCall` with the manually tracked `ModuleAccountNonce` so successive calls in the same block don't collide.

Re-deploying the factory on genesis import is explicitly skipped — see `keeper.go:155-159` — because that would overwrite live EVM state and shift the deterministic addresses of every UEA on chain.

## Genesis

```protobuf
GenesisState {
  Params              params
  repeated string     pending_inbounds
  repeated UTXEntry   universal_txs
  uint64              module_account_nonce
  repeated GasPrice   gas_prices       // legacy
  repeated ChainMeta  chain_metas
  repeated Outbound   pending_outbounds
  bool                exported          // skip factory deploy if true
}
```

## Block Lifecycle

`x/uexecutor` does not implement a `BeginBlocker` or `EndBlocker` — the module is listed in the manager's order arrays as a placeholder, but all real work happens synchronously in the message handlers. Vote tallying, inbound execution, outbound creation, and chain-meta updates are all triggered by incoming `Msg*` calls.

## Layout

```
x/uexecutor/
|-- keeper/
|   |-- keeper.go              State + dependencies
|   |-- msg_server.go          MsgVoteInbound, MsgVoteOutbound, MsgVoteChainMeta, ExecutePayload
|   |-- query_server.go        v1 queries
|   |-- query_server_v2.go     v2 queries
|   +-- ...                    inbound execution, outbound creation, chain meta, derived EVM calls
|-- types/
|   |-- types.pb.go            UniversalTx, Inbound, ChainMeta, PendingOutboundEntry, enums
|   |-- params.go              Params (currently a single placeholder field)
|   |-- keys.go                Store prefixes + ID generators
|   |-- abi.go, decode_payload.go, gateway_pc_event_decode.go, caip2.go
|   +-- expected_keepers.go    Interfaces for evm/feemarket/bank/account/uregistry/uvalidator
|-- migrations/                v2, v4, v5 — params shape, UTX restructure, GasPrices -> ChainMetas
|-- module.go                  AppModule wiring
|-- autocli.go                 CLI auto-registration
+-- depinject.go               Dependency injection
```
