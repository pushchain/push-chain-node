# `x/uvalidator` — Universal Validator Set, Ballot Voting & Reward Boost

The consensus coordination layer for Push Chain's crosschain protocol. Three responsibilities live here:

1. **Maintain the Universal Validator (UV) set** — the subset of standard Cosmos validators that have been approved to additionally run a `puniversald` worker and participate in crosschain consensus.
2. **Run the generic ballot machine** that every other Push module votes through (inbound, outbound, chain meta, TSS events, fund migrations all use it).
3. **Boost UV rewards** — in `BeginBlocker`, intercept the FeeCollector balance and allocate an extra `0.148x` portion to active UVs so running a Universal Validator is economically attractive.

## What It Does

### Universal Validator Lifecycle

A standard Cosmos validator becomes a UV by being added by the admin. Lifecycle:

```
       AddUniversalValidator              RemoveUniversalValidator
PENDING_JOIN ---------------> ACTIVE ---------------------------> PENDING_LEAVE -----> LEFT
              (admin)                       (admin)                              (gradual)
                                                                         
Slashing-driven side states: TOMBSTONED (terminal — can never return)
```

The status is stored as `LifecycleInfo` on the `UniversalValidator` record. `UpdateUniversalValidator` lets the validator self-update its crosschain identity (network info, public keys for external chains) without needing admin approval. `UpdateUniversalValidatorStatus` is admin-gated for everything else.

Bonded check (`IsBondedUniversalValidator`) requires:
1. The validator is in `UniversalValidatorSet`
2. The validator exists in the staking module
3. The validator's status is `BONDED`

Tombstone check (`IsTombstonedUniversalValidator`) consults the slashing keeper directly, so any double-sign by the underlying core validator immediately removes their UV from the eligible voter set.

### Generic Ballot Machine

Every crosschain observation (in `x/uexecutor` and `x/utss`) is voted through this single mechanism:

```go
ballot, finalized, isNew, err := k.VoteOnBallot(
    ctx,
    ballotId,           // canonical hash of the observation
    ballotType,         // INBOUND | OUTBOUND | CHAIN_META | TSS_EVENT | FUND_MIGRATION
    voter,              // signer's bech32 address
    voteResult,         // SUCCESS | FAILURE
    eligibleVoters,     // snapshot of UVs at ballot creation
    votesNeeded,        // threshold (caller decides 2/3, 100%, simple majority, ...)
    expiryAfterBlocks,  // ballot auto-expires after this many blocks
)
```

A ballot is created lazily on the first vote, indexed in `ActiveBallotIDs`, and finalizes the moment either:
- `yesVotes >= votingThreshold` -> `BALLOT_STATUS_PASSED`
- `eligibleVoters - noVotes < votingThreshold` (the threshold is now mathematically unreachable) -> `BALLOT_STATUS_REJECTED`

On finalization, the ballot is moved from `ActiveBallotIDs` -> `FinalizedBallotIDs`. Expired ballots that never reached threshold are moved to `ExpiredBallotIDs`.

The ballot type is opaque — `x/uvalidator` doesn't care what's being voted on. The ballot ID is a `sha256` of the canonical observation, so two validators voting on the same observation hit the same ballot deterministically.

### UV Reward Boost (BeginBlocker)

`x/uvalidator`'s `BeginBlocker` runs **before** the standard distribution module's `BeginBlocker` and reshapes the fee distribution:

```
                                    1.148x effective power
                                       for active UVs
fees collected     +-------------------------------------------+
in previous   ---->|  uvalidator BeginBlocker                  |
block         ---->|                                           |
                   |  1. Compute effective_total_power:        |
                   |       sum( vote.power * 1.148  if UV      |
                   |            vote.power           else )    |
                   |                                           |
                   |  2. For each UV vote, allocate            |
                   |       fees * (vote.power * 0.148)         |
                   |              / effective_total_power      |
                   |     to the validator via distribution     |
                   |     module's AllocateTokensToValidator    |
                   |                                           |
                   |  3. Forward the boost coins to the        |
                   |     distribution module account so        |
                   |     accounting matches                    |
                   |                                           |
                   |  4. Send the remaining coins back to      |
                   |     the FeeCollector                      |
                   +-------------------------------------------+
                                       |
                                       v
                          standard distribution BeginBlocker
                          runs as usual on the remaining fees
```

Constants in `abci.go`:

```go
const BoostMultiplier   = "1.148"  // applied to UV power when computing the denominator
const ExtraBoostPortion = "0.148"  // numerator for the UV-specific allocation
```

Net effect: a validator that runs a UV earns ~14.8% more block rewards than a non-UV with the same stake. This is the only economic incentive baked into the protocol for running a UV — it has to make sense as a business for permissioned operators.

> **Note on community tax** — The boost math is correct only when community tax is `0`. With a non-zero community tax, the UV boost is taken from the full fee amount before tax is applied to the remainder, so the community pool sees a slightly smaller share than configured. This is documented inline in `abci.go`.

## State (KV layout)

| Prefix | Collection | Type | Purpose |
|---|---|---|---|
| `0` | `Params` | `Item[Params]` | Module parameters (admin address) |
| `2` | `UniversalValidatorSet` | `Map[sdk.ValAddress, UniversalValidator]` | Registered UVs with lifecycle info and crosschain identity |
| `3` | `Ballots` | `Map[string, Ballot]` | All ballots ever created |
| `4` | `ActiveBallotIDs` | `KeySet[string]` | Ballots currently collecting votes |
| `5` | `ExpiredBallotIDs` | `KeySet[string]` | Expired (not yet pruned) ballots |
| `6` | `FinalizedBallotIDs` | `KeySet[string]` | `PASSED` or `REJECTED` ballots |

(Prefix `1` was historically used by an obsolete `core_to_universal` mapping and is left unused for migration compatibility.)

## Messages (`MsgServer`)

| Message | Authority | Purpose |
|---|---|---|
| `MsgAddUniversalValidator` | admin | Register a core validator as a UV (`PENDING_JOIN`) |
| `MsgRemoveUniversalValidator` | admin | Begin removing a UV (`PENDING_LEAVE`) |
| `MsgUpdateUniversalValidatorStatus` | admin | Force-set lifecycle status (escape hatch) |
| `MsgUpdateUniversalValidator` | self | The UV updates its own crosschain identity (network info / external pubkeys) |
| `MsgUpdateParams` | gov | Rotate admin or update other params |

## Queries

- `Params`
- `AllUniversalValidators`, `UniversalValidator`
- `Ballot`, `AllBallots`
- `AllActiveBallotIDs`, `AllActiveBallots`

## Hooks

`x/uvalidator` exports `UValidatorHooks`:

```go
type UValidatorHooks interface {
    AfterValidatorAdded(ctx, valAddr) error
    AfterValidatorRemoved(ctx, valAddr) error
    AfterValidatorStatusChanged(ctx, valAddr, oldStatus, newStatus) error
}
```

A `MultiUValidatorHooks` dispatcher (`keeper/hooks.go`) lets multiple consumers subscribe. As of today, no other module installs hooks, but the interface is present for future use.

## Inter-module Dependencies

The keeper holds:
- `StakingKeeper` — to look up validators by operator/consensus address and to gate `IsBondedUniversalValidator`
- `SlashingKeeper` — to check tombstone status (`IsTombstoned` by consensus address)
- `BankKeeper` — to move fees between FeeCollector / `uvalidator` / `distribution` module accounts during the boost
- `AuthKeeper` (`AccountKeeper`) — to resolve the FeeCollector module account
- `DistributionKeeper` — to call `AllocateTokensToValidator` for the UV boost
- `UtssKeeper` — used during validator lifecycle transitions when TSS quorum changes are needed

## Genesis

```protobuf
GenesisState {
  Params                          params
  repeated UniversalValidatorEntry universal_validators
  repeated Ballot                 ballots
  repeated string                 active_ballot_ids
  repeated string                 expired_ballot_ids
  repeated string                 finalized_ballot_ids
}
```

Default admin in `params.go`: `push1negskcfqu09j5zvpk7nhvacnwyy2mafffy7r6a`.

## Layout

```
x/uvalidator/
|-- abci.go                    BeginBlocker — UV reward boost (this is the interesting one)
|-- keeper/
|   |-- keeper.go              State + dependencies
|   |-- voting.go              IsBondedUV, IsTombstonedUV, AddVoteToBallot, VoteOnBallot, CheckIfFinalizingVote
|   |-- ballot.go              CreateBallot, GetOrCreateBallot, ExpireBallotsBeforeHeight
|   |-- validator.go           UV set CRUD and bonded/tombstone helpers
|   |-- hooks.go               MultiUValidatorHooks dispatcher
|   |-- msg_server.go          + msg_*.go for each message type
|   +-- query_server.go        gRPC queries
|-- types/
|   |-- ballot.go, ballot.pb.go  Ballot lifecycle (ShouldPass, ShouldReject, IsExpired, AddVote)
|   |-- universal_validator.go, types.pb.go  UV record + UVStatus enum
|   |-- identity_info.go, network_info.go    Per-chain identity
|   |-- lifecyle_info.go, lifecyle_event.go  Status tracking
|   |-- params.go, keys.go
|   +-- expected_keepers.go    Staking, Slashing, Bank, Distribution, Account, Utss interfaces
|-- migrations/                Consensus version 2 — one prior breaking change
|-- module.go
|-- autocli.go
+-- depinject.go
```
