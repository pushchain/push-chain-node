# `x/ucallback` — Read-from-Chains, core-side module plan

**Status:** Draft for review · **Written:** 2026-08-04
**Scope:** core chain only. Contracts (`push-chain-core-contracts@feat-read-state`) and universal
validators (`push-chain-node#296`) are owned by other teams; both are already written, which means
**our interface is pinned from both ends**.

Every claim below is cited to a file:line or a live query. Open questions are collected in §9.

---

## 1. What we own

```
┌─ contracts (done, branch)                  ┌─ UV (done, draft PR #296)
│  UniversalCallback.sol                     │  externalchains/{evm,svm,web2}/read_executor.go
│  emits ReadRequested                       │  pushwatcher/ → polls us
│  exposes fulfillExternalCallback           │  pushcore.GetAllPendingReadRequests() ← STUB
│          expireExternalRead                │
└────────────┬───────────────────────────────┴──────────┬─────────────────
             │                                          │
        ┌────▼──────────────────────────────────────────▼────┐
        │                   x/ucallback   ← US               │
        │  ingest ReadRequested → serve pending → tally       │
        │  ballot → call fulfill/expire → record outcome      │
        └─────────────────────────────────────────────────────┘
```

The UV's stub states our deliverable verbatim (`universalClient/pushcore/pushCore.go:377`):

> `TODO(core): blocked on x/uexecutor Query/PendingReadRequests … mirror GetAllPendingOutbounds`

Note it says `x/uexecutor`. We are choosing `x/ucallback` instead — see §2.1 for why that is fine
and §9 Q1 for the one thing it forces.

---

## 2. Hard constraints discovered

### 2.1 🔴 The EVM caller must be the **uexecutor** module account

`UniversalCallback.sol:25` hardcodes an immutable:

```solidity
address public immutable UNIVERSAL_EXECUTOR_MODULE = 0x14191Ea54B4c176fCf86f51b0FAc7CB1E71Df7d7;
modifier onlyUEModule() { if (msg.sender != UNIVERSAL_EXECUTOR_MODULE) revert CallerIsNotUEModule(); }
```

Verified against live donut:

```
uexecutor module  push1zsv3af2tfstklnux75dsltruk8n3ma7hnxp8ew
           → hex  0x14191ea54b4c176fcf86f51b0fac7cb1e71df7d7   ← identical
```

A new module gets `authtypes.NewModuleAddress("ucallback")`, a **different** address. Every
`fulfillExternalCallback` would revert.

**Decision taken:** `x/ucallback` owns state and lifecycle; the EVM call is executed *as uexecutor*
by calling into the uexecutor keeper. No contract change, no redeploy coupling.

> Consequence: we inherit uexecutor's module-nonce path, including the known drift bug where
> module-sender calls skip `ModuleAccountNonce` increment. Fix that before adding a fourth caller.

### 2.2 🔴 `SetHooks` panics if called twice

`x/vm/keeper/keeper.go:254`:

```go
func (k *Keeper) SetHooks(eh types.EvmHooks) *Keeper {
    if k.hooks != nil { panic("cannot set evm hooks twice") }
```

and `app/app.go:794` already does `app.EVMKeeper.SetHooks(uexecutorkeeper.NewEVMHooks(...))`.

**Solution:** `evmkeeper.NewMultiEvmHooks(uexecutorHooks, ucallbackHooks)` (`x/vm/keeper/hooks.go:27`).

> ⚠️ `MultiEvmHooks.PostTxProcessing` aborts the whole chain of hooks on the first error
> (`hooks.go:40-42`). A bug in our hook therefore **fails unrelated EVM transactions**. Our hook
> must never return a non-nil error for anything short of a genuine consensus fault — log and
> continue instead.

### 2.3 🟡 Known defects on the contract side we must design around

| defect | effect on us |
|---|---|
| `expireExternalRead` transfers nothing (verified: 0 value-transfer statements) | our sweeper "expires" a request but the funder is never repaid; fees accrue in the contract |
| refund uses `call{value}` + `revert` on failure | an app without `receive()` makes `fulfillExternalCallback` revert **forever**; our submit will never succeed |
| `fulfilledRequests[requestId] = true` set *before* dispatch | whatever we submit is final; a quorum on ERROR is permanent, no retry |
| `_localContext` leaks in the app on the failure/expiry paths | not ours, but it's what users will report to us |

These are raised with the contracts team (§10). Our design must not *depend* on them being fixed.

---

## 3. Data model

### 3.1 `UniversalRead` — the aggregate

Named as the read-side sibling of `UniversalTx` (`proto/uexecutor/v1/types.proto:186`). Deliberately
**not** a clone: a read is triggered by a Push-chain event, performs no external write, has no
external tx hash, and produces exactly one Push-chain fulfilment.

```protobuf
// proto/ucallback/v1/types.proto
message UniversalRead {
  option (amino.name) = "ucallback/universal_read";

  string id                    = 1;  // requestId, 0x-hex uint256
  ReadRequest request          = 2;
  ReadResult  result           = 3;  // set when the ballot finalises
  repeated PCTx pc_tx          = 4;  // fulfil / expire attempts (reuse uexecutor's PCTx)
  UniversalReadStatus status   = 5;
  string ballot_key            = 6;
}
```

`pc_tx` is repeated (fulfil, then possibly expire); `request`/`result` are singular. There is no
top-level `revert_error` — failures live in `PCTx.error_msg`, which is the field that made a stuck
`UniversalTx` debuggable in production.

### 3.2 `ReadRequest` — served verbatim to UVs

```protobuf
message ReadRequest {
  string request_id               = 1;
  string destination_chain        = 2;  // CAIP-2, composed by us
  bytes  owner                    = 3;
  bytes  query                    = 4;
  uint32 min_confirmations        = 5;  // uint16 on the wire
  uint64 destination_block_height = 6;
  uint64 expiry_block_height      = 7;
  uint64 created_at_height        = 8;  // derived — NOT in the event

  // core-only, never consumed by the UV
  string callback_target          = 9;
  string original_funder          = 10;
  string fees_deposited           = 11; // uint256 as string
  string max_fee                  = 12;
  string requested_tx_hash        = 13; // provenance / dedup / debugging
  uint64 requested_log_index      = 14;
}
```

Field-for-field this must satisfy `uread.ReadRequest` (`universalClient/uread/types.go:9`), the
temporary struct we are meant to delete.

### 3.3 `ReadResult` — the ballot payload

```protobuf
message ReadResult {
  ReadStatus status                = 1;
  bytes      result_data           = 2;
  uint64     observed_block_height = 3;
  bytes      observed_block_hash   = 4;
}
```

**`error_msg` is deliberately absent from the proto**, not merely unused. In `uread` it is excluded
by a comment — *"local diagnostic only — never part of the ballot"*. Once it is a generated type
that convention will be forgotten; make it structurally impossible. If error text ever enters the
ballot key, no two validators ever agree.

### 3.4 Enums

```protobuf
enum ReadStatus { READ_STATUS_UNSPECIFIED = 0; READ_STATUS_SUCCESS = 1; READ_STATUS_ERROR = 2; }

enum UniversalReadStatus {
  UNIVERSAL_READ_STATUS_UNSPECIFIED = 0;
  UNIVERSAL_READ_STATUS_PENDING     = 1;  // ingested, awaiting votes
  UNIVERSAL_READ_STATUS_VOTING      = 2;  // ≥1 vote, no quorum
  UNIVERSAL_READ_STATUS_FULFILLED   = 3;  // callback dispatched OK
  UNIVERSAL_READ_STATUS_EXPIRED     = 4;  // expireExternalRead submitted
  UNIVERSAL_READ_STATUS_FAILED      = 5;  // quorum reached, callback reverted
}
```

### 3.5 Contract → our fields

| our field | from `ReadRequested` |
|---|---|
| `request_id` | `requestId` |
| `destination_chain` | `account.chainNamespace + ":" + account.chainId` |
| `owner` / `query` / `min_confirmations` | `account.owner` / `readSpec.query` / `readSpec.minConfirmations` |
| `destination_block_height` | `readSpec.blockNumber` |
| `expiry_block_height` | `readSpec.expiryPushChainHeight` |
| `callback_target` / `original_funder` / `fees_deposited` / `max_fee` | same-named event args |
| **`created_at_height`** | **not emitted** — take from the log's Push block height |

---

## 4. Storage

```
UniversalReads          : requestId → UniversalRead          (collections.Map)
PendingByExpiry         : (expiryHeight, requestId) → ()     (KeySet, in-flight set)
ReadsByTxHash           : (pushTxHash, requestId) → ()       (KeySet, batch reassembly)
Params                  : module params
```

There is deliberately **no `ballotKey → requestId` index**. The ballot terminal hook resolves a ballot
by scanning `PendingByExpiry`, which holds only unsettled reads — the same trade uexecutor already
makes in `ballot_hooks.go:86` for the identical problem. That leaves `PendingByExpiry` with two
consumers, so it stays regardless of how the open expiry-cadence question is answered.

`PendingByExpiry` must be an ordered composite key so the sweeper can range-scan
`[0, currentHeight]` in `EndBlocker` rather than iterating everything.

---

## 5. Components

```
proto/ucallback/v1/{types,tx,query,genesis,params}.proto
x/ucallback/
  keeper/
    keeper.go               collections wiring, uexecutor + uvalidator keeper refs
    evm_hooks.go            PostTxProcessing → ingest ReadRequested
    ingest.go               log filter + decode → UniversalRead{PENDING}
    msg_vote_read_result.go MsgVoteReadResult → VoteOnReadBallot
    ballot_hooks.go         AfterBallotTerminal(READ_RESULT) → fulfil
    fulfill.go              call UniversalCallback.fulfillExternalCallback via uexecutor
    expire.go               EndBlocker sweeper → expireExternalRead
    grpc_query.go           AllPendingReadRequests, GetUniversalRead
  types/
    constants.go  keys.go  codec.go  errors.go  events.go
  module.go / depinject
```

### 5.1 Ingestion (`evm_hooks.go` + `ingest.go`)

Mirrors `x/uexecutor/keeper/create_outbound.go:27-42`:

```go
for _, lg := range receipt.Logs {
    if lg.Removed { continue }
    if !strings.EqualFold(lg.Address, universalCallbackAddr) { continue }   // ← MANDATORY
    if len(lg.Topics) == 0 || !strings.EqualFold(lg.Topics[0], ReadRequestedEventSig) { continue }
    ...
}
```

**The address filter is a security control, not a nicety.** Matching on topic0 alone lets anyone
deploy a contract emitting an identical `ReadRequested` and conscript the entire validator set into
performing free external reads — a cheap DoS. The eventual `fulfillExternalCallback` would be
rejected (`InvalidRequestId`), so no funds move, but validator work and module txs are burned.

### 5.2 Query (`grpc_query.go`)

```protobuf
rpc AllPendingReadRequests(QueryAllPendingReadRequestsRequest)
      returns (QueryAllPendingReadRequestsResponse);   // paginated, mirrors AllPendingOutbounds
rpc GetUniversalRead(QueryGetUniversalReadRequest)
      returns (QueryGetUniversalReadResponse);         // mirrors v2 GetUniversalTx
```

`AllPendingReadRequests` is the one the UV is blocked on. `GetUniversalRead` is the operator tool —
the `get-universal-tx` equivalent that made a stuck production tx diagnosable in minutes.

### 5.3 Voting

```protobuf
rpc VoteReadResult(MsgVoteReadResult) returns (MsgVoteReadResultResponse);

message MsgVoteReadResult {
  option (cosmos.msg.v1.signer) = "signer";
  string signer      = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string request_id  = 2;
  ReadResult result  = 3;
}
```

Mirrors `MsgVoteOutbound` (`proto/uexecutor/v1/tx.proto:121`). Requires a new enum value in
`proto/uvalidator/v1/ballot.proto:25`:

```protobuf
BALLOT_OBSERVATION_TYPE_READ_RESULT = 5;
```

`ballotKey = H(requestId ‖ status ‖ resultData ‖ observedBlockHeight ‖ observedBlockHash)` —
**excluding** error text.

### 5.4 Ballot terminal hook

Extend `BallotHooks.AfterBallotTerminal` (`x/uexecutor/keeper/ballot_hooks.go:56`) with a
`BALLOT_OBSERVATION_TYPE_READ_RESULT` case → `afterReadBallotTerminal` → §5.5.

### 5.5 Fulfilment

Call `UniversalCallback.fulfillExternalCallback(requestId, resultData, observedBlockHeight,
observedBlockHash)` through uexecutor's `DerivedEVMCall` so `msg.sender` is the uexecutor module
(§2.1). Record the outcome as a `PCTx` — **including `error_msg` on failure** — and set status
`FULFILLED` or `FAILED`.

> `fulfillExternalCallback` does `call{gas: callbackGasLimit}` with `callbackGasLimit` up to
> `MAX_CALLBACK_GAS_LIMIT = 1_000_000` and performs **no 63/64 check**. Since we are the caller, we
> must ensure `gasleft() ≥ callbackGasLimit × 64/63 + buffer` before dispatch, or the callback
> silently under-runs and fails permanently.

### 5.6 Expiry sweeper

`EndBlocker`: range-scan `PendingByExpiry` over `[0, ctx.BlockHeight()]`, submit
`expireExternalRead(requestId)`, mark `EXPIRED`. Bound the per-block count (§9 Q5).

---

## 6. Wiring (`app/app.go`)

```go
app.EVMKeeper.SetHooks(evmkeeper.NewMultiEvmHooks(
    uexecutorkeeper.NewEVMHooks(app.UexecutorKeeper),
    ucallbackkeeper.NewEVMHooks(app.UcallbackKeeper),
))
```

Replaces the single-hook call at `app/app.go:794`. Plus: module registration, `SetOrderEndBlockers`
entry for the sweeper, and a `ucallback` entry in `maccPerms`.

> `BlockedAddresses()` derives from `GetMaccPerms()`, and since cosmos/evm v0.7 the blocked list also
> gates `SetBalance`. Adding `ucallback` to `maccPerms` therefore makes its address unable to receive
> native EVM value. That is almost certainly what we want — flag it if not.

---

## 7. Delivery order

1. protos + generated types (`make proto-gen`, Docker)
2. storage + keeper skeleton + genesis
3. `AllPendingReadRequests` → **unblocks the UV team immediately**, even returning empty
4. ingestion hook + `MultiEvmHooks` rewire
5. `MsgVoteReadResult` + ballot type + tally
6. terminal hook → fulfilment
7. expiry sweeper
8. `GetUniversalRead` + CLI
9. upgrade handler (new store key → `StoreUpgrades.Added`)

Step 3 is deliberately early and cheap: it deletes `ErrReadQueriesNotAvailable` and lets the UV team
integrate against a real endpoint while the rest lands.

---

## 8. Testing

- ingestion: forged log from a non-`UniversalCallback` address is **ignored** (security regression test)
- ballot: two validators voting identical results converge; differing `error_msg` must not split them
- fulfilment: callback revert → `FAILED` + `error_msg` recorded, request not retried
- expiry: request past `expiry_block_height` is swept exactly once
- upgrade sim from the current donut release with the new store key

---

## 9. Open questions / decisions needed

**Q1 — module name vs the UV's expectation.**
The UV stub targets `x/uexecutor Query/PendingReadRequests`. If we ship `x/ucallback`, the UV team
must change the client path and proto import. Cheap, but it is a cross-team change that must be
agreed *before* they unblock. **Do we confirm `ucallback` with them now?**

**Q2 — should we ever submit an ERROR ballot?**
PR #296's EVM executor votes ERROR on *any* `CallContract` failure
(`externalchains/evm/read_executor.go:61`), including non-archive nodes, 429s and timeouts — while
the SVM and web2 executors correctly treat transport failures as transient. Because ERROR ballots are
byte-identical by design, validators failing for unrelated infrastructure reasons converge into a
confident quorum indistinguishable from a genuine revert — and the contract makes it permanent.
**Do we harden core-side (refuse ERROR ballots lacking revert evidence), or require the UV fix first?**

**Q3 — retry semantics.**
The contract marks `fulfilledRequests[requestId] = true` before dispatch, so a reverted callback is
terminal. Do we (a) accept that and record `FAILED`, or (b) ask contracts to mark fulfilled only on
success so a retry is possible? (b) is a contract change and must be requested while they are still
on a branch.

**Q4 — do we gate on Push-chain confirmations before serving a request?**
The UV sets `ConfirmationType: store.ConfirmationInstant` (`pushwatcher/event_parser.go:114`) — it
acts immediately on whatever we serve. If we serve from a block that later reorgs, validators do work
for a request that never existed. **Serve immediately, or hold N blocks?**

**Q5 — sweeper budget.**
Max expiries per block? Unbounded risks a fat EndBlocker; too low and a backlog never drains.

**Q6 — who pays for callback gas?**
There is **no validator/reader reward path anywhere in `UniversalCallback.sol`** (grep: 0 matches).
The `callbackGasLimit × tx.gasprice` component is collected then refunded in full on both success and
failure — it pays nobody. The module bears real execution cost for up to 1M gas per read, gasless.
**Is that intentional for v1?**

**Q7 — `ReadRequested` topic + `UniversalCallback` address: config or Go constants?**
uexecutor hardcodes both (`types/constants.go`, `uregistrytypes.SYSTEM_CONTRACTS`). That pattern has
already failed once in production: `RescueFundsOnSourceChainEventSig` still declares a signature the
contract renamed to `FundsRescued`, and it silently matches nothing. **Strong recommendation: put both
in chain config**, like `gateway_methods` / `vault_methods`.

**Q9 — 🔴 web2 reads cannot be expressed by the contract.**
The UV has a complete web2 path (`externalchains/web2/read_executor.go`, 418 lines, SSRF-hardened),
`uread` documents the CAIP form `web2:https` and marks `DestinationBlockHeight` *"not applicable for
web2"*. But the contracts contain **zero** web2 references, and `requestExternalReadSelf` rejects it:

```solidity
if (spec.blockNumber == 0
    || spec.blockNumber > _universalCore.chainHeightByChainNamespace(...)) revert InvalidBlockNumber();
if (spec.account.owner.length == 0) revert InvalidAccountId();
if (spec.minConfirmations < MIN_CONFIRMATIONS_FLOOR) revert InvalidMinConfirmations();
```

A web2 request would need a fabricated `blockNumber`, a fictional `web2` namespace height in
`UniversalCore` exceeding it, a dummy `owner` (the URL lives in `query`), and a meaningless
`minConfirmations ≥ 1`.

Ballots still converge — every web2 voter reports height `0` and an empty hash — so the tally needs no
special case. The costs are (a) we persist and serve a fake `destination_block_height`, and (b) any
future confirmation-gating on our side must exempt web2.

**Is web2 in scope for v1? If yes, the contract needs a namespace-aware validation branch. If no, the
UV's web2 executor is dead code and we should not model for it.**

**Q8 — one `ucallback` per read type, or reuse for future callbacks?**
The name implies a general callback module. If future non-read callbacks are planned, `UniversalRead`
should probably sit under a broader `UniversalCallbackRecord` umbrella now rather than later.

---

## 10. Cross-team asks

**Contracts** (`feat-read-state`, pre-merge — cheapest to fix now):
1. `expireExternalRead` refunds nothing — funder's fee is trapped
2. refund `call{value}` + `revert` lets an app with no `receive()` brick its own fulfilment forever
3. `UniversalReadClient` has no `receive()`; `CrossLendMock` adds one privately, so tests pass and the
   requirement is invisible to integrators
4. consider marking fulfilled only on callback success (Q3)
5. `chainHeightByChainNamespace` ignores `chainId`, so all `eip155` chains share one height
6. fee derived from requester-controlled `tx.gasprice`
7. **web2 reads are unrequestable** — validation assumes a blockchain (`blockNumber != 0`, non-empty
   `owner`, `minConfirmations ≥ 1`) while the UV has a complete web2 executor (Q9). Needs either a
   namespace-aware validation branch or an explicit "web2 is not in v1" decision

**UV team** (#296):
1. `eth_call` transport failures must not become ERROR ballots (Q2) — copy the web2 executor's own
   three-way transient/deterministic split
2. confirm the `ucallback` module path (Q1)
3. `uread` deletion once our generated types land
