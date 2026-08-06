# `x/ucallback` — core implementation guide

**Companion to** `UCALLBACK_MODULE_PLAN.md` (architecture, rationale, open questions).
This file is the build order. Section numbers below match the **actual commits** on
`feat/read-state`, which diverged from the original plan — scaffolding landed before protos, state was
split out of the skeleton, and the protocgen fix was unplanned:

| # | commit | state |
|---|---|---|
| C0 | `d2e033ce` fix(proto): stop protocgen deleting compat/orm-api | done (unplanned prerequisite) |
| C1 | `b9c966f3` feat(ucallback): scaffold module | done |
| C2 | `d0ad5720` feat(ucallback): add read-state types | done |
| C3 | keeper state + indexes | **staged, in review** |
| C4 | queries | next — unblocks the UV team |
| C5–C9 | ingestion → vote → hook → sweeper → upgrade | planned |

**Design decisions already locked** (see plan §2 for evidence):

| decision | why |
|---|---|
| new module `x/ucallback` | clean separation from uexecutor |
| EVM calls made **as the uexecutor module account** | `UniversalCallback.sol:25` hardcodes `0x14191Ea5…` immutable; a `ucallback` module account is rejected by `onlyUEModule` |
| aggregate named `UniversalRead` | read-side sibling of `UniversalTx` |
| `error_msg` absent from the ballot proto entirely | convention-only exclusion will be forgotten once generated |
| ballot key computed over the *identical-mode subset* | v2 `MEDIAN` must be excluded or it's a consensus-breaking change later |
| ingest filters on `log.Address` **and** topic0 | we listen to one trusted contract; the filter is what makes that true |

---

## OPEN — expiry semantics, to confirm with the team

**Not resolved. Do not treat the C8 sweeper design as settled until this is answered.**

There are **two independent expiry clocks**, and the interaction between them is unspecified:

| | clock | set by | enforced at |
|---|---|---|---|
| **A** | `ReadSpec.expiryPushChainHeight` → our `ReadRequest.expiry_block_height` | the app, per request | `UniversalCallback.sol:121` on request, `:207` in `expireExternalRead` |
| **B** | `Ballot.block_height_expiry` | us, as arg 8 to `VoteOnBallot` | `x/uvalidator/keeper/ballot.go:344` |

Questions, in the order they change the design:

1. **Is late fulfilment intended?** `fulfillExternalCallback` has **no expiry check** — the only guard
   is `fulfilledRequests`. A quorum reached long after `expiryHeight` still fulfils and still calls the
   app's callback. So A is not a deadline on fulfilment; it is one side of a fulfil-vs-expire race.
2. **Does expiry need to be prompt at all?** `expireExternalRead` **refunds nothing**. Prompt sweeping
   frees contract storage and moves our record off `PENDING` — nothing a user feels. If the answer is
   "no", the sweeper does not need per-block cadence, and the case for keeping `PendingByExpiry` rests
   only on "don't scan an unboundedly-growing map", not on frequency.
3. **Should B be disabled?** uexecutor passes `DefaultExpiryAfterBlocks = 100_000_000` (~19 yrs) with
   *"Ballots should not expire without an escape hatch for stuck pending items."* If we copy that, A is
   the only real deadline. If we don't, a ballot can die while its read is still live — leaving a record
   that can neither fulfil nor expire until A fires. Two clocks on one lifecycle is how records get stuck.

**Consequences that are parked on this**: sweeper cadence (every block vs every N) and the
inclusive/exclusive boundary at exactly `expiryHeight`.

**No longer parked on it: `PendingByExpiry` itself.** C3 dropped the `ballotKey → requestId` index and
made the ballot terminal hook scan `PendingByExpiry` instead, so that set now has two consumers. It
stays whichever way the cadence question is answered.

---

## Reference points in existing code

Copy these, don't invent:

```
x/uexecutor/keeper/evm_hooks.go:21      NewEVMHooks / PostTxProcessing shape
x/uexecutor/keeper/create_outbound.go:27-42   log scan: address filter → topic filter → decode
x/uexecutor/keeper/voting.go:73-125     VoteOnOutboundBallot — the exact voting template
x/uexecutor/keeper/ballot_hooks.go:56   AfterBallotTerminal dispatch
x/uexecutor/keeper/chain_meta.go        median-without-ballots (relevant only for v2)
x/uexecutor/types/types.proto:186       UniversalTx shape · :123 PCTx (reuse)
proto/uexecutor/v1/tx.proto:121         MsgVoteOutbound shape
proto/uvalidator/v1/ballot.proto:25     BallotObservationType enum
app/app.go:794                          EVMKeeper.SetHooks — currently single-hook
```

**The ballot model, stated plainly** — this shapes everything downstream:
`Ballot.votes` is a parallel array of binary `VoteResult{SUCCESS|FAILURE}`. The **ballot ID encodes the
observation**. Distinct observations produce distinct ballots; the one that reaches quorum wins.
Validators do not vote *values*. This is why `VoteChainMeta` bypasses ballots entirely to compute gas
medians, and why v2 `MEDIAN` cannot ride the ballot path.

---

# C2 — protos  ·  DONE `d0ad5720`

**Files**
```
proto/ucallback/v1/types.proto
proto/ucallback/v1/genesis.proto
proto/ucallback/v1/params.proto
```

```protobuf
// types.proto
message UniversalRead {
  option (amino.name) = "ucallback/universal_read";
  option (gogoproto.equal) = true;

  string id                  = 1;   // requestId, 0x-hex uint256
  ReadRequest request        = 2;
  ReadResult  result         = 3;   // set when the ballot finalises
  repeated uexecutor.v1.PCTx pc_tx = 4;   // fulfil / expire attempts — REUSE
  UniversalReadStatus status = 5;
  string ballot_key          = 6;
}

message ReadRequest {
  string request_id               = 1;
  string destination_chain        = 2;  // CAIP-2, composed by us
  bytes  owner                    = 3;
  bytes  query                    = 4;
  uint32 min_confirmations        = 5;
  uint64 destination_block_height = 6;
  uint64 expiry_block_height      = 7;
  uint64 created_at_height        = 8;  // derived from the log's block — NOT in the event

  // core-only, never read by the UV
  string callback_target          = 9;
  string original_funder          = 10;
  string fees_deposited           = 11;
  string max_fee                  = 12;
  string requested_tx_hash        = 13;
  uint64 requested_log_index      = 14;
}

// The ballot payload. NO error_msg field — deliberately.
message ReadResult {
  ReadStatus status                = 1;
  bytes      result_data           = 2;
  uint64     observed_block_height = 3;
  bytes      observed_block_hash   = 4;
  // reserved for v2 MEDIAN — excluded from the ballot key when populated
  repeated AggregateValue aggregates = 5;
}

message AggregateValue {
  uint32 extract_index = 1;
  uint32 mode          = 2;
  bytes  value         = 3;
}

enum ReadStatus {
  READ_STATUS_UNSPECIFIED = 0;
  READ_STATUS_SUCCESS     = 1;
  READ_STATUS_ERROR       = 2;
}

enum UniversalReadStatus {
  UNIVERSAL_READ_STATUS_UNSPECIFIED = 0;
  UNIVERSAL_READ_STATUS_PENDING     = 1;
  UNIVERSAL_READ_STATUS_VOTING      = 2;
  UNIVERSAL_READ_STATUS_FULFILLED   = 3;
  UNIVERSAL_READ_STATUS_EXPIRED     = 4;
  UNIVERSAL_READ_STATUS_FAILED      = 5;  // quorum reached, callback reverted
}
```

**`ReadRequest` must satisfy `universalClient/uread/types.go:9` field-for-field** — that struct exists
only to be deleted once these types generate.

**Generate:** `make proto-gen` (Docker required — not the script directly).

**Verify:** `go build ./...`; generated types exist; `uread.ReadRequest` maps 1:1.

---

# C1 + C3 — module skeleton `b9c966f3`, keeper state (staged)

**Files**
```
x/ucallback/module.go            AppModule, depinject
x/ucallback/keeper/keeper.go     collections wiring
x/ucallback/keeper/genesis.go    InitGenesis / ExportGenesis
x/ucallback/types/{keys,codec,errors,constants}.go
app/app.go                       module registration + maccPerms
```

```go
type Keeper struct {
    UniversalReads  collections.Map[string, types.UniversalRead]
    PendingByExpiry collections.KeySet[collections.Pair[uint64, string]] // in-flight set
    ReadsByTxHash   collections.KeySet[collections.Pair[string, string]] // (txHash, requestId)
    Params          collections.Item[types.Params]

    evmKeeper       types.EVMKeeper
    uexecutorKeeper types.UexecutorKeeper   // for module addr + DerivedEVMCall
    uvalidatorKeeper types.UvalidatorKeeper // for eligible voters + VoteOnBallot
}
```

`ReadsByTxHash` exists because a single Push transaction can emit several `ReadRequested` logs —
a batching app, or a contract that fires more than one `_requestRead` in one call. Each becomes its
own `UniversalRead` keyed by `requestId` (they share no lifecycle: one can be FULFILLED while its
sibling EXPIRES), so this index is what reassembles the batch for `reads-by-tx`. Ordered composite
key, prefix-scanned by `txHash`.

`PendingByExpiry` **must** be an ordered composite key so the sweeper can range-scan
`[0, currentHeight]` rather than iterating the whole set.

> Adding `ucallback` to `maccPerms` puts its address into `BlockedAddresses()`, and since cosmos/evm
> v0.7 that list also gates `SetBalance` — so the module address can't receive native EVM value.
> That is almost certainly correct here; note it if not.

**Verify:** chain starts, genesis round-trips, `q ucallback params` responds.

---

# C4 — queries ← **ship this early, it unblocks the UV team**

**Files**
```
proto/ucallback/v1/query.proto
x/ucallback/keeper/grpc_query.go
x/ucallback/client/cli/query.go
```

```protobuf
rpc AllPendingReadRequests(QueryAllPendingReadRequestsRequest)
      returns (QueryAllPendingReadRequestsResponse);   // paginated
rpc GetUniversalRead(QueryGetUniversalReadRequest)
      returns (QueryGetUniversalReadResponse);
rpc ReadsByTxHash(QueryReadsByTxHashRequest)
      returns (QueryReadsByTxHashResponse);            // all reads from one Push tx
```

`AllPendingReadRequests` returns `[]ReadRequest` where status is `PENDING`. Mirror
`AllPendingOutbounds` for pagination shape.

**Why third and not last:** it deletes `ErrReadQueriesNotAvailable` in
`universalClient/pushcore/pushCore.go:382` and lets the UV team integrate against a real endpoint —
even while it returns an empty list. Their TODO names this exact query.

`ReadsByTxHash` prefix-scans `ReadsByTxHash` and returns the full `UniversalRead` for each hit —
`q ucallback reads-by-tx <hash>`. This is how a batched request is inspected as a unit, and it is why
we can key records by `requestId` without losing the grouping.

`GetUniversalRead` is the operator tool. Model the CLI on `q uexecutor v2 get-universal-tx`; that query
is what made a stranded production tx diagnosable in minutes this week.

**Verify:** UV team's `GetAllPendingReadRequests` returns `[]` instead of an error.

---

# C5 — ingestion

**Files**
```
x/ucallback/keeper/evm_hooks.go
x/ucallback/keeper/ingest.go
x/ucallback/types/event_decode.go
app/app.go                        ← rewire to MultiEvmHooks
```

```go
func (h EVMHooks) PostTxProcessing(ctx sdk.Context, sender common.Address,
                                   msg core.Message, receipt *ethtypes.Receipt) error {
    if err := h.k.IngestReadRequests(ctx, receipt); err != nil {
        h.k.Logger().Error("ucallback ingest failed", "tx", receipt.TxHash, "err", err)
    }
    return nil   // NEVER non-nil — see below
}
```

> 🔴 `MultiEvmHooks.PostTxProcessing` aborts the whole hook chain on the first error
> (`x/vm/keeper/hooks.go:40-42`), which **fails the EVM transaction**. A bug in our hook would break
> unrelated user txs. Log and continue; never return an error for anything short of a consensus fault.

```go
// ingest.go — mirrors create_outbound.go:27-42
for _, lg := range receipt.Logs {
    if lg.Removed                                          { continue }
    if !strings.EqualFold(lg.Address, ucAddr)              { continue }
    if len(lg.Topics) == 0                                 { continue }
    if !strings.EqualFold(lg.Topics[0], ReadRequestedSig)  { continue }

    ev, err := types.DecodeReadRequestedFromLog(lg)
    if err != nil { k.Logger().Error(...); continue }

    if has, _ := k.UniversalReads.Has(ctx, ev.RequestID); has { continue }  // idempotent

    ur := types.UniversalRead{
        Id: ev.RequestID,
        Request: &types.ReadRequest{
            RequestId:              ev.RequestID,
            DestinationChain:       ev.ChainNamespace + ":" + ev.ChainId,
            Owner:                  ev.Owner,
            Query:                  ev.Query,
            MinConfirmations:       uint32(ev.MinConfirmations),
            DestinationBlockHeight: ev.BlockNumber,
            ExpiryBlockHeight:      ev.ExpiryPushChainHeight,
            CreatedAtHeight:        uint64(ctx.BlockHeight()),
            CallbackTarget:         ev.CallbackTarget,
            OriginalFunder:         ev.OriginalFunder,
            FeesDeposited:          ev.FeesDeposited.String(),
            MaxFee:                 ev.MaxFee.String(),
            RequestedTxHash:        receipt.TxHash.Hex(),
            RequestedLogIndex:      uint64(lg.Index),
        },
        Status: types.UNIVERSAL_READ_STATUS_PENDING,
    }
    k.UniversalReads.Set(ctx, ev.RequestID, ur)
    k.PendingByExpiry.Set(ctx, collections.Join(ev.ExpiryPushChainHeight, ev.RequestID))
}
```

**app.go — `SetHooks` panics if called twice** (`x/vm/keeper/keeper.go:255`), and line 794 already
registers uexecutor's:

```go
app.EVMKeeper.SetHooks(evmkeeper.NewMultiEvmHooks(
    uexecutorkeeper.NewEVMHooks(app.UexecutorKeeper),
    ucallbackkeeper.NewEVMHooks(app.UcallbackKeeper),
))
```

**⚠️ Put `ReadRequestedSig` and the `UniversalCallback` address in chain config, not Go constants.**
uexecutor hardcodes both, and that has already failed in production: `constants.go:56` still declares
`RescueFundsOnSourceChain(...)` for an event the contract renamed to `FundsRescued`, so it silently
matches nothing. Register these like `gateway_methods` / `vault_methods`.

**Tests**
- a log from a **non-UniversalCallback** address is ignored (regression test — this is the filter that makes "we only listen to one trusted contract" true)
- wrong topic0 ignored
- duplicate log → single record
- `created_at_height` equals the block height, not anything from the event

---

# C6 — vote message and ballot

**Files**
```
proto/uvalidator/v1/ballot.proto      + BALLOT_OBSERVATION_TYPE_READ_RESULT = 5
proto/ucallback/v1/tx.proto           MsgVoteReadResult
x/ucallback/keeper/msg_vote_read_result.go
x/ucallback/keeper/voting.go          VoteOnReadBallot
x/ucallback/types/keys.go             GetReadBallotKey
```

```protobuf
rpc VoteReadResult(MsgVoteReadResult) returns (MsgVoteReadResultResponse);

message MsgVoteReadResult {
  option (amino.name) = "ucallback/MsgVoteReadResult";
  option (cosmos.msg.v1.signer) = "signer";
  string signer     = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string request_id = 2;
  ReadResult result = 3;
}
```

```go
// keys.go — mode-aware from day one.
// v1: every field is IDENTICAL, so this equals H(all of result_data).
// v2: aggregates are EXCLUDED here and medianed separately at quorum.
func GetReadBallotKey(requestId string, r *ReadResult) (string, error) {
    // hash: requestId ‖ status ‖ result_data ‖ observed_block_height ‖ observed_block_hash
    // NOT hashed: aggregates, and error_msg does not exist in the proto
}
```

```go
// voting.go — copy x/uexecutor/keeper/voting.go:73-125 verbatim, swapping the observation type
func (k Keeper) VoteOnReadBallot(ctx, universalValidator sdk.ValAddress,
                                 requestId string, res *types.ReadResult) (isFinalized, isNew bool, err error) {
    ballotKey, err := types.GetReadBallotKey(requestId, res)
    voters, _ := k.uvalidatorKeeper.GetEligibleVoters(ctx)
    votesNeeded := (types.VotesThresholdNumerator*len(voters))/types.VotesThresholdDenominator + 1

    _, isFinalized, isNew, err = k.uvalidatorKeeper.VoteOnBallot(
        ctx, ballotKey,
        uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_READ_RESULT,
        universalValidator.String(),
        uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
        voterAddrStrs, int64(votesNeeded), int64(types.DefaultExpiryAfterBlocks),
    )
    // ballotKey is stored on the UniversalRead itself; there is no reverse index.
    // AfterBallotTerminal resolves it by scanning PendingByExpiry — see
    // GetUniversalReadByBallot.
    return
}
```

**Msg server guards:** request exists · status is `PENDING` or `VOTING` · signer is an eligible
universal validator · not already `FULFILLED`/`EXPIRED`.

**Open decision (plan Q2):** whether to reject `READ_STATUS_ERROR` ballots that carry no revert
evidence. PR #296's EVM executor votes ERROR on *any* `CallContract` failure — including pruned nodes
and 429s — and because ERROR ballots are byte-identical by design, infra faults converge into a
confident quorum. Decide before this commit lands.

**Tests**
- two validators, identical results → one ballot, converges
- two validators, different `result_data` → two ballots, neither converges
- non-validator signer rejected
- vote on a `FULFILLED` request rejected

---

# C7 — ballot terminal hook and fulfilment

**Files**
```
x/ucallback/keeper/ballot_hooks.go
x/ucallback/keeper/fulfill.go
x/ucallback/types/abi.go            UniversalCallback ABI
```

```go
func (h BallotHooks) AfterBallotTerminal(ctx, ballotKey string,
                                          ballotType uvalidatortypes.BallotObservationType, ...) error {
    switch ballotType {
    case uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_READ_RESULT:
        return h.afterReadBallotTerminal(ctx, ballotKey)
    }
    return nil
}
```

```go
// fulfill.go
func (k Keeper) FulfillRead(ctx sdk.Context, ur types.UniversalRead) error {
    abi, _ := types.ParseUniversalCallbackABI()
    ueModuleAcc, _   := k.uexecutorKeeper.GetUeModuleAddress(ctx)
    isModuleSender, nonce, _ := k.uexecutorKeeper.ModuleSenderNonce(ctx, ueModuleAcc)

    resp, err := k.evmKeeper.DerivedEVMCall(
        ctx, abi,
        ueModuleAcc,             // MUST be uexecutor — contract hardcodes 0x14191Ea5…
        universalCallbackAddr,
        big.NewInt(0), nil,      // gasLimit nil — estimate, per house convention
        true,  /*commit*/ false, /*gasless*/
        isModuleSender, nonce,
        "fulfillExternalCallback",
        requestIdBig, ur.Result.ResultData,
        ur.Result.ObservedBlockHeight, ur.Result.ObservedBlockHash,
    )

    ur.PcTx = append(ur.PcTx, &uexecutortypes.PCTx{
        BlockHeight: uint64(ctx.BlockHeight()),
        Status:      err == nil && resp != nil && resp.VmError == "",
        ErrorMsg:    errMsgOf(err, resp),      // ← the field that saves you at 2am
    })
    if success { ur.Status = FULFILLED } else { ur.Status = FAILED }
    k.PendingByExpiry.Remove(ctx, collections.Join(ur.Request.ExpiryBlockHeight, ur.Id))
    k.UniversalReads.Set(ctx, ur.Id, ur)
}
```

> **Decision: `gasLimit` is `nil`.** Ten of the eleven existing `DerivedEVMCall` sites pass `nil`
> and let `EstimateGasInternal` size it; only `CallUEAExecutePayload` passes a value, and only
> because the user's signed payload supplies one. We follow the convention.
>
> Consequence: we never need `callbackGasLimit`, so there is no `getPendingRead` call in the fulfil
> path and no dependency on contracts emitting it in `ReadRequested`.
>
> Residual risk, recorded not mitigated: the estimator's own doc says it *"may underpredict"*, and a
> `nil` site is how `CallPRC20Deposit` produced `intrinsic gas too low` on donut this week. If a read
> ever underpredicts, that request fails terminally (F4). The fix would be passing an explicit limit
> here — localised to this one call.

> 🔴 **Never set `FULFILLED` optimistically.** If the submit itself fails (nonce drift, gas), the
> ballot is terminal but the callback never landed — you must be able to retry until expiry.

**Note the uexecutor module-nonce drift bug**: module-sender calls skip `ModuleAccountNonce`
increment. Fix it before adding a fourth caller, or `FulfillRead` inherits it.

**Tests**
- quorum → `fulfillExternalCallback` called with exactly the voted values
- callback reverts → `FAILED` + `error_msg` recorded, no retry
- submit fails → status unchanged, still retryable
- gas: `gasleft()` at the call ≥ `callbackGasLimit`

---

# C8 — expiry sweeper

**Files**
```
x/ucallback/keeper/expire.go
x/ucallback/module.go              EndBlock
app/app.go                         SetOrderEndBlockers
```

```go
func (k Keeper) SweepExpired(ctx sdk.Context) error {
    h := uint64(ctx.BlockHeight())
    rng := collections.NewPrefixUntilPairRange[uint64, string](h)
    n := 0
    for iter, _ := k.PendingByExpiry.Iterate(ctx, rng); iter.Valid() && n < maxExpiriesPerBlock; iter.Next() {
        // DerivedEVMCall → expireExternalRead(requestId), same sender rules as C7
        // record PCTx, status = EXPIRED, de-index
        n++
    }
}
```

Bound it per block — unbounded makes a fat EndBlocker, too low and a backlog never drains.

> **Blocked on the open expiry question at the top of this file.** Cadence, whether `PendingByExpiry`
> exists at all, and the boundary at exactly `expiryHeight` are all downstream of that answer. The
> sketch above assumes per-block; that assumption is the thing under review.

> The contract's `expireExternalRead` **transfers nothing** (verified: zero value-transfer statements),
> so the funder's fee is trapped. That is a contracts bug, not ours — but our sweeper is what makes it
> visible, so record it clearly in the `PCTx` and surface it in `GetUniversalRead`.

**Tests**
- request past expiry swept exactly once
- request at exactly `expiryHeight` — decide inclusive/exclusive and pin it
- fulfil/expire race: contract's `fulfilledRequests` guard means first-wins; core must swallow the
  loser's revert without corrupting status

---

# C9 — upgrade handler

**Files**
```
app/upgrades/<name>/upgrade.go
app/upgrades.go
```

New store key → `StoreUpgrades.Added: []string{"ucallback"}`.

**Not a no-op `RunMigrations` — the handler must also deploy UniversalCallback at 0xC2.**
Verified against donut (chain 42101, height 20,791,174): `0x…C2`, `0xF2…C2` and `0xF1…C2` are all
empty — `code: 0x`, balance 0, nonce 0. Only the explicitly-named `SYSTEM_CONTRACTS` entries
(`0xAA 0xB0 0xB1 0xB2 0xBC 0xC0 0xC1`) are live; every `RESERVED_*` slot (`0xA0 0xA5 0xB3 0xC2 0xCF`)
is empty, because the deploy loop in `x/uregistry/keeper/genesis.go` runs at `InitGenesis` only and
donut's genesis predates the `init()` that added those reservations.

So promoting `RESERVED_C2` → `UNIVERSAL_CALLBACK` is free on donut (nothing there either way), but the
real contract will not appear on its own — the handler has to deploy the admin + impl + proxy triple
explicitly, the way genesis would have.

> Related, and worth raising with the team separately: the F-2026-17025 squatting defence is **not in
> effect on donut**. The A/B/C reserved slots are empty and claimable there. Pre-existing, but we are
> about to place a contract in that range.

**Verify with a real upgrade simulation** from the current donut release to this branch — the
established flow: start the old binary, submit `MsgSoftwareUpgrade` at a height well past the end of
the voting period, let cosmovisor swap, confirm `q upgrade applied`, then run a tx.

> Set the proposal height generously past the **end of the voting period**, not just the submit
> height — two simulation proposals failed with `upgrade cannot be scheduled in the past` learning
> this.

---

## Ordering rationale

C1→C3 are prerequisites. **C4 (queries) is deliberately early**: it is cheap, it unblocks the UV team,
and it can ship returning an empty list. C5 (ingestion) makes records real. C6–C7 are the consensus
core and should land together in review even if committed separately. C8 (sweeper) is safe to add last
because until it exists, expired requests simply accumulate — no corruption. C9 gates deployment.

## Cross-team dependencies

Nothing here is blocked on contracts. But six contract defects change behaviour at the edges, and all
are cheap while `feat-read-state` is unmerged — F6 (**no status channel in
`fulfillExternalCallback`**) is the only one with no core-side workaround. Full list in the plan §10.
