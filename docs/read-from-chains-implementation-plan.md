# Read from Chains — Core + universalClient Implementation Plan

## References
- Spec v1: `read_v1.pdf` (UniversalCallback + MetaCallbackSpec model — superseded)
- Spec v2: `read_v2.pdf` (UniversalReadClient model — **adopted**)
- Contracts: [pushchain/push-chain-core-contracts@51e5aeb](https://github.com/pushchain/push-chain-core-contracts/commit/51e5aeb2dd0cc0ecd23134f3b313a455ca52fdde) (`read-state-v1`)

## What the contracts already define (fixed surface we integrate against)

- `UniversalCallback.sol` (singleton, upgradeable):
  - `requestExternalReadSelf(ReadSpec spec, bytes4 callbackSelector, uint64 callbackGasLimit) payable → requestId`
    - validates: non-empty account/query, `minConfirmations >= 1`, `maxAgeSeconds/maxDelaySeconds != 0`, `supportedDomains[ns][id]`, `callbackGasLimit <= 1_000_000`, `fee <= msg.value <= spec.maxFee`
    - `requestId = keccak256(block.chainid, block.number, address(this), keccak256(spec), nonce++)`
    - emits **`ReadRequested(uint256 indexed requestId, ReadSpec spec, address indexed callbackTarget, address indexed originalFunder, uint256 feesDeposited)`**
  - `fulfillExternalCallback(uint256 requestId, bytes resultData, uint64 observedBlockHeight, bytes32 observedBlockHash)` — **`onlyUEModule`**
    - calls `callbackTarget.call{gas}(selector, requestId, resultData)`; handles fee split (protocol fee → VaultPC, refund → funder) internally
    - emits `ReadFulfilled` / `CallbackFailed`
  - `expireExternalRead(uint256 requestId)` — **`onlyUEModule`**, emits `RequestExpired`
  - `UNIVERSAL_EXECUTOR_MODULE = 0x14191Ea54B4c176fCf86f51b0FAc7CB1E71Df7d7` (hardcoded)
- `ReadTypes.sol`:
  - `ReadSpec { UniversalAccountId account; bytes query; uint16 minConfirmations; uint64 maxAgeSeconds; uint64 maxDelaySeconds; uint256 maxFee; }`
  - `BALLOT_OBSERVATION_TYPE_READ_REQUEST = 0x3dad9a0d…` — contracts expect a matching ballot type in core
- `UniversalReadClient.sol` — app-side base (`_requestRead` / `onUniversalData` / `_onReadResult`, `_localContext` storage); no core/uClient work needed
- `UniversalCore.sol` — new `readBaseFeeByChainNamespace[ns][id]` + `updateReadBaseFeeByChain` (admin)
- Query envelopes (spec v1, still applies): `EvmQueryEnvelope` (AccountBalance / ERC20Balance / ContractCall / StorageSlot + `EvmBlockRef`), `SolanaQueryEnvelope` (LamportBalance / SPLTokenAccount / RawAccountData + `minSlot`), `Web2QueryEnvelope` (GET/POST)

## End-to-end flow (target)

1. App inherits `UniversalReadClient`, calls `_requestRead` mid-execution → `ReadRequested` emitted on Push EVM
2. `x/uexecutor` `PostTxProcessing` hook decodes the event in-block → stores `PendingReadRequest`
3. universalClient (each validator) polls pending reads via gRPC → executes the query envelope against the external chain RPC → canonical-encodes result
4. Validator submits `MsgVoteReadResult` → uvalidator ballot; identical `(resultData, height, hash)` → same ballot key → >2/3 quorum
5. On finalization, uexecutor calls `fulfillExternalCallback` on `UniversalCallback` via module EVM call
6. Expiry: request not finalized within `maxDelaySeconds` → EndBlocker calls `expireExternalRead`

---

## Core (`push-chain-node`) changes

### 1. Proto (`proto/…` + buf regen)

- `proto/uvalidator/v1/ballot.proto`
  - add `BALLOT_OBSERVATION_TYPE_READ_REQUEST` to `BallotObservationType`
- `proto/uexecutor/v1/` (new `read_request.proto` + `tx.proto` + `query.proto`)
  - `ReadRequest` type: `request_id (bytes/hex)`, decoded `ReadSpec` fields (`chain_namespace`, `chain_id`, `owner`, `query`, `min_confirmations`, `max_age_seconds`, `max_delay_seconds`), `callback_target`, `pinned_block_height`, `created_at_height`, `expiry_timestamp`, `status (PENDING | FULFILLED | EXPIRED)`
  - `MsgVoteReadResult { signer, request_id, result_data, observed_block_height, observed_block_hash, status (SUCCESS | ERROR) }`
  - `Query/PendingReadRequests` (paginated) — mirror `GetAllPendingOutbounds`

### 2. Event detection (`x/uexecutor`)

- `types/events.go`: add `ReadRequestedEventSig` (topic0 of the event above)
- `types/gateway_pc_event_decode.go`: add `DecodeReadRequestedFromLog` (ABI-decode `ReadSpec` tuple from log data)
- `keeper/evm_hooks.go` `PostTxProcessing`: match logs from the `UniversalCallback` address → decode → `CreatePendingReadRequest`
  - follows the existing outbound-detection precedent (`BuildOutboundsFromReceipt`); no Push-EVM log polling needed in uClient
- `x/uregistry`: register `UNIVERSAL_CALLBACK` in `SYSTEM_CONTRACTS` (address source for hook filtering + callback calls)

### 3. Pending-read storage (`x/uexecutor/keeper`)

- new `Keeper.PendingReadRequests` collection + `keeper/pending_read_request.go` (CRUD, mirror `pending_outbound.go`)
- **Pin the query height at creation**: `pinned_block_height = ChainMeta[chain].LastAppliedChainHeight − spec.minConfirmations` (clamped)
  - all validators query the same height → identical bytes; satisfies the spec rule "block taken must be below gas-oracle minimum"
  - reject/park request if no ChainMeta exists for the chain
- set `expiry_timestamp = block.time + maxDelaySeconds`

### 4. Voting (`x/uexecutor`)

- `types/msg_vote_read_result.go` — ValidateBasic (mirror `msg_vote_inbound.go`)
- ballot key: `GetReadBallotKey = hash(request_id ‖ status ‖ result_data ‖ observed_block_height ‖ observed_block_hash)` — identical-bytes quorum
- `keeper/voting.go`: `VoteOnReadBallot` → `uvalidatorKeeper.VoteOnBallot` with the new ballot type, threshold `(2*validators)/3 + 1` (same as inbound)
- `keeper/msg_vote_read_result.go`:
  - reject if request unknown / not PENDING / past expiry
  - on finalizing vote (SUCCESS): `CallFulfillExternalCallback`, mark FULFILLED
  - on finalizing vote (ERROR quorum — e.g. query invalid, target chain reorged): mark EXPIRED + `CallExpireExternalRead` (refund path)
- `keeper/ballot_hooks.go`: handle terminal FAILED/EXPIRED ballots for the new type (cleanup)

### 5. EVM callback (`x/uexecutor`)

- `types/abi.go`: add `UniversalCallbackABI` const + `ParseUniversalCallbackABI` (only `fulfillExternalCallback`, `expireExternalRead`)
- `keeper/evm.go`: `CallFulfillExternalCallback(...)` + `CallExpireExternalRead(...)` via `DerivedEVMCall` (template: `CallExecuteUniversalTx` / `CallUniversalCoreSetChainMeta`; uses `ModuleAccountNonce`)
- gas: callback gas is bounded on the contract side (`callbackGasLimit ≤ 1M`); give the module call a fixed generous limit
- **verify** module account EVM address == `0x14191Ea54B4c176fCf86f51b0FAc7CB1E71Df7d7` (contract hardcodes it); mismatch = every fulfill reverts

### 6. Expiry sweep

- `x/uexecutor` EndBlocker (`abci.go`): iterate PENDING reads with `expiry_timestamp < block.time` → `CallExpireExternalRead` → mark EXPIRED
  - deterministic on-chain, no vote needed
  - bound per-block work (process N per block) to avoid unbounded EndBlocker gas

### 7. Queries / CLI

- `keeper/grpc_query.go` (or new file): `PendingReadRequests`, `ReadRequest(id)`
- autocli entries for inspection

---

## universalClient changes

> **Status: implemented** (UV side done; items marked `TODO(core)` are stubbed and unblock mechanically once core lands — grep `TODO(core)` in `universalClient/`).
>
> - `universalClient/uread/` — **temporary package**: only proto-mirror types (`ReadRequest`/`ReadStatus`/`ReadResult`); delete it once core proto lands by swapping every `uread.*` reference to `uexecutortypes.*`
> - `externalchains/common/read.go` — shared permanent bits: `ChainReader`/`ChainResolver` interfaces, `CAIP2`, canonical result encoders (`EncodeUint256Result`/`EncodeBytes32Result`)
> - `externalchains/evm/read_envelope.go` + `read_executor.go` + RPC additions (`GetBalanceAt`, `GetStorageAt`, `GetHeaderByNumber`) — all 4 query types at pinned height (**TODO(core): remove latest−minConfirmations fallback once core pins height**)
> - `externalchains/svm/read_envelope.go` + `read_executor.go` + RPC additions (`GetBalanceWithSlot`, `GetAccountInfoWithSlot`) — all 3 query types, minContextSlot semantics
> - `universalClient/pushwatcher/` (moved out of the chains manager — push is core-managed, not a registry chain) — `event_listener.go` + `event_parser.go` fetch pending reads via gRPC and **route each `READ_REQUEST` event into the target chain's DB** (`common.ReadStoreResolver`, implemented by `chains.Chains.GetStore`); requests for unserved chains are skipped and retried next poll (core re-serves pending requests; expiry is the backstop)
> - `externalchains/common/event_processor.go` — original type-switch shape kept; gained a `READ_REQUEST` branch (`processReadRequestEvent`: execute on the chain's own `ChainReader` → vote → COMPLETED; corrupt/expired → REVERTED; transient → retry), a `reader ChainReader` constructor param (evm/svm pass the client itself), and the consumer-side `VoteSigner` interface; push client has no processor at all
> - `pushcore/pushCore.go` `GetAllPendingReadRequests` (**TODO(core): wire to Query/PendingReadRequests**; returns sentinel until then, processor idles silently)
> - `pushsigner/pushsigner.go` `VoteReadResult` (**TODO(core): build MsgVoteReadResult in vote.go + add to AuthZ grant set**)
> - wiring: push client is owned by `core/client.go` (not the chains manager) — core opens the push DB once (shared with TSS), creates `push.NewClient(..., chainsManager)` and manages its lifecycle; `externalchains.Chains` only manages registry-driven external chains and implements `GetStore` for read routing

### 1. pushcore (`universalClient/pushcore/pushCore.go`)

- `GetAllPendingReadRequests()` — new gRPC query wrapper (mirror `GetAllPendingOutbounds`)

### 2. pushsigner (`universalClient/pushsigner/`)

- `VoteReadResult(ctx, msg)` — build `MsgVoteReadResult`, AuthZ-wrap, sign, broadcast (mirror `VoteInbound`)
- add msg type to AuthZ grant set (hot-key authorization for the new msg URL)

### 3. Read worker (`universalClient/chains/push/` — new component)

- new `read_request_processor.go` on the Push client (alongside `event_listener.go`):
  - poll `GetAllPendingReadRequests` on the existing polling interval
  - local store dedup (per-chain SQLite, reuse `common.ChainStore`) so a request is executed/voted once; retry until vote tx confirmed
  - skip requests already past `expiry_timestamp`
- **query executor** (new `universalClient/readexecutor/` or under `chains/common/`):
  - resolve target chain client: `chainNamespace + ":" + chainId` → CAIP-2 → existing `Chains` registry RPC client
  - decode envelope by namespace:
    - `eip155` → `EvmQueryEnvelope`: AccountBalance → `eth_getBalance`, ERC20Balance → `balanceOf` via `eth_call`, ContractCall → `eth_call`, StorageSlot → `eth_getStorageAt` — all at `pinned_block_height`; fetch `observedBlockHash` for that height
    - `solana` → `SolanaQueryEnvelope`: LamportBalance / SPLTokenAccount / RawAccountData via `getAccountInfo`/`getBalance` with `minContextSlot = pinned height`; observed slot + blockhash from response context
    - `web2` → **out of scope for v1** (non-deterministic responses; needs canonicalization design) — vote ERROR if received
  - canonical result encoding (must be byte-identical across validators):
    - AccountBalance/LamportBalance → `abi.encode(uint256)`
    - ERC20Balance/SPLTokenAccount → `abi.encode(uint256)`
    - ContractCall → raw returndata
    - StorageSlot → `abi.encode(bytes32)`; RawAccountData → raw account bytes
  - on RPC/decode failure after retries → `VoteReadResult(status = ERROR)`
- interaction with per-chain clients: reads target chains uClient already watches (registry-driven); if a supported domain has no chain client, vote ERROR

### 4. Config (`universalClient/config/`)

- optional: `read_polling_interval_seconds` per Push chain entry (else reuse `event_polling_interval_seconds`)
- no new per-external-chain config — reuse existing `rpc_urls`

---

## Cross-cutting decisions / open questions

- **Height pinning source**: plan uses ChainMeta (gas oracle) height at request time; confirm ChainMeta exists for all chains that will be `supportedDomains` on the contract (contract-side whitelist and core-side ChainMeta must stay in sync — no core check enforces this)
- **`maxAgeSeconds`**: with pinned-height reads, "freshness" = pinned height recency; ChainMeta staleness already bounds this — decide whether core must additionally reject requests when ChainMeta is older than `maxAgeSeconds`
- **Solana determinism**: account data can change between slots and `getAccountInfo` can't query an exact past slot; `minContextSlot` gives ≥ semantics, so identical-bytes quorum may need slot-tolerant ballot design (e.g. vote on value only, drop observed slot from ballot key) — flag for design review
- **`expireExternalRead` vs ERROR quorum**: both route to expiry on the contract; keep both (EndBlocker for timeout, ERROR ballot for definitively-failing queries) or simplify to timeout-only
- **Module address**: `UNIVERSAL_EXECUTOR_MODULE` is hardcoded in the contract — verify against `authtypes.NewModuleAddress(uexecutortypes.ModuleName)` EVM mapping before deploy
- **Fee flow**: fully contract-side (protocol fee → VaultPC, refunds); core only triggers callbacks — no bank/fee logic needed in module
- **Nomenclature**: PDFs say `x/UCallback` as a separate module; plan puts everything in `x/uexecutor` (reuses EVM hooks, module nonce, ballot plumbing, existing AuthZ grants) — confirm

## Suggested implementation order

1. Proto + ballot type + codegen
2. Event decode + PendingReadRequest storage + evm_hooks detection
3. ABI + `CallFulfillExternalCallback` / `CallExpireExternalRead`
4. `MsgVoteReadResult` handler + ballot wiring + EndBlocker expiry
5. Queries (gRPC) + autocli
6. uClient: pushcore query + pushsigner vote + read processor + query executor (EVM first, then SVM)
7. E2E test: local chain + mock external RPC (extend `scripts/test_universal.sh`)
