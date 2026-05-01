# `x/utss` — Threshold Signature Scheme

The on-chain coordination layer for Push Chain's TSS key. The actual DKLS protocol runs off-chain inside the Universal Validator binary (`puniversald`); this module is the deterministic state machine that schedules processes, tallies validator votes about what happened off-chain, and serves as the canonical record of which TSS key is active.

## What It Does

- **Schedules TSS key processes** — admin-initiated keygen, refresh, and quorum-change events. Each process is given a deterministic `process_id` and tracked through history.
- **Stores the active TSS key** — `CurrentTssKey` is the single source of truth for which key signs outbound transactions. `TssKeyHistory` retains every key that has ever existed (never deleted, used by fund migration).
- **Tallies UV votes on TSS events** — every fine-grained step of an off-chain DKLS run (setup message produced, key derived, vote-to-finalize) is voted onto chain via `MsgVoteTssKeyProcess`. The module finalizes events through the generic ballot machine in `x/uvalidator`.
- **Coordinates fund migration** — when a `KEYGEN` produces a new public key, funds locked under the old key on every external chain need to move to the new key. Each (old_key, chain) pair becomes a `FundMigration` record; UVs broadcast the migration tx off-chain and vote success/failure on chain.

## State (KV layout)

| Prefix | Collection | Type | Purpose |
|---|---|---|---|
| `0` | `Params` | `Item[Params]` | Module parameters (admin address) |
| `1` | `NextProcessId` | `Sequence` | Auto-increment for process IDs |
| `2` | `CurrentTssProcess` | `Item[TssKeyProcess]` | Active in-flight process (may be empty) |
| `3` | `ProcessHistory` | `Map[uint64, TssKeyProcess]` | All past processes by ID |
| `4` | `CurrentTssKey` | `Item[TssKey]` | Currently active finalized key |
| `5` | `TssKeyHistory` | `Map[string, TssKey]` | All keys ever finalized, keyed by `key_id` |
| `6` | `TssEvents` | `Map[uint64, TssEvent]` | Per-event records produced during a process |
| `7` | `NextTssEventId` | `Sequence` | Auto-increment for event IDs |
| `8` | `PendingTssEvents` | `Map[uint64, uint64]` | `process_id -> event_id` index of in-flight events |
| `9` | `FundMigrations` | `Map[uint64, FundMigration]` | Migration records by ID |
| `10` | `NextMigrationId` | `Sequence` | Auto-increment for migration IDs |
| `11` | `PendingMigrations` | `Map[uint64, uint64]` | `migration_id -> migration_id` pending index |

`PendingTssEvents` and `PendingMigrations` are deliberately structured as `uint64 -> uint64` indexes so the keeper can iterate "everything currently in flight" without scanning the full history.

## Process Types

| Type | Public key | On-chain addresses | Triggers fund migration? |
|---|---|---|---|
| `KEYGEN` | new | new | yes — funds must move to the new addresses on every chain |
| `REFRESH` | unchanged | unchanged | no — only keyshares are redistributed |
| `QUORUM_CHANGE` | unchanged | unchanged | no — only the participant set changes |

`KEYGEN` is the heaviest operation: it lets the protocol periodically rotate the master key as a security uplift, but it forces a coordinated migration of every locked balance on every connected chain.

## Messages (`MsgServer`)

| Message | Authority | Gasless? | Purpose |
|---|---|---|---|
| `MsgInitiateTssKeyProcess` | admin | no | Start a new keygen / refresh / quorum-change |
| `MsgVoteTssKeyProcess` | bonded UV | yes | Vote on a TSS event during an active process |
| `MsgInitiateFundMigration` | admin | no | Open a migration record for an old key on a specific chain |
| `MsgVoteFundMigration` | bonded UV | yes | Vote success or failure on a fund migration tx |
| `MsgUpdateParams` | gov | no | Rotate admin or update other params |

Vote messages gate on `IsBondedUniversalValidator` and `IsTombstonedUniversalValidator` from `x/uvalidator`. The two vote messages are gasless so UVs can participate without holding gas tokens.

## Queries

- `Params`
- `CurrentProcess`, `ProcessById`, `AllProcesses`
- `CurrentKey`, `KeyById`
- Plus event and migration queries (see `keeper/query_server.go`)

## Inter-module Dependencies

The keeper holds:
- `uvalidatorKeeper` — bonded/tombstoned checks, generic ballot machine
- `uregistryKeeper` — chain lookups for fund migration
- `uexecutorKeeper` — to update UTX state when migration affects in-flight outbounds

It exports no hooks; other modules read `CurrentTssKey` to know what address signs outbounds.

## Genesis

```protobuf
GenesisState {
  Params           params
  TssKeyProcess?   current_tss_process
  repeated TssKeyProcessEntry process_history
  TssKey?          current_tss_key
  repeated TssKeyEntry        tss_key_history
  uint64           next_process_id
  repeated TssEvent           tss_events
  uint64           next_tss_event_id
  repeated FundMigrationEntry fund_migrations
  uint64           next_migration_id
}
```

`PendingMigrations` is reconstructed from `FundMigrations` during `InitGenesis` by re-indexing every entry whose status is `FUND_MIGRATION_STATUS_PENDING`.

Default admin in `params.go`: `push1negskcfqu09j5zvpk7nhvacnwyy2mafffy7r6a`.

## Layout

```
x/utss/
|-- keeper/
|   |-- keeper.go              State + lifecycle
|   |-- msg_server.go          InitiateTssKeyProcess, VoteTssKeyProcess, InitiateFundMigration, VoteFundMigration
|   +-- query_server.go        gRPC queries
|-- types/
|   |-- types.pb.go            TssKeyProcess, TssKey, TssEvent, FundMigration, enums
|   |-- params.go              Admin field
|   |-- keys.go                Store prefixes + ballot key generators (sha256 of canonical inputs)
|   |-- tss_key.go, tss_key_process.go, msg_tss_key_process.go
|   +-- expected_keepers.go    UValidatorKeeper, URegistryKeeper, UExecutorKeeper interfaces
|-- module.go
|-- autocli.go
+-- depinject.go
```
