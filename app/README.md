# Core Validator

Push Chain's L1 node binary (`pchaind`). Four custom Cosmos SDK modules and one custom EVM precompile turn the chain into the universal-execution layer that coordinates inbounds, outbounds, and TSS-signed crosschain transactions.

- **Produces** blocks via CometBFT consensus and runs the EVM execution engine for both standard and universal traffic
- **Coordinates** the crosschain protocol — collects votes from Universal Validators on inbounds/outbounds/chain meta, finalizes ballots, drives TSS keygen and fund migration, and rewards UV operators with a boosted fee share
- **Hosts** Universal Executor Accounts (UEAs) and the chain-meta oracle on its EVM, giving any source-chain user a deterministic Push Chain identity and predictable gas pricing across networks

## Architecture

```
app/
|-- app.go               ChainApp wiring (4 custom modules + usigverifier)
|-- precompiles.go       Baseline EVM precompile registration (bech32, p256, staking, ...)
|-- ante/                Custom AnteHandler chain (gasless support)
|   |-- ante.go          Routes Ethereum vs Cosmos txs by extension option
|   |-- ante_cosmos.go   Cosmos decorator chain
|   |-- ante_evm.go      EVM mono-decorator wrapper
|   |-- fee.go           Custom DeductFeeDecorator (skips fee for gasless txs)
|   +-- account_init_decorator.go   Creates accounts mid-pipeline for first-time gasless signers
|-- cosmos/
|   +-- min_gas_price.go MinGasPriceDecorator (skips min-fee check for gasless txs)
|-- decorators/          Generic message-filter decorator template
|-- txpolicy/
|   +-- gasless.go       IsGaslessTx — single source of truth for the gasless message whitelist
|-- params/              Test encoding configuration
+-- config.go, encoding.go, genesis.go, token_pair.go, wasm.go

x/                       Custom Cosmos SDK modules (only what Push adds)
|-- uexecutor/           Universal transaction execution layer
|-- uregistry/           Chain & token registry
|-- uvalidator/          Universal validator set + ballot voting + UV reward boost
+-- utss/                TSS keygen / refresh / quorum-change / fund migration

precompiles/
+-- usigverifier/        Ed25519 signature verification precompile (Solana sig verification on EVM)

cmd/pchaind/             Binary entry point, root command, key/EVM CLI wiring
proto/                   Protobuf definitions for the four custom modules
config/                  Per-chain JSON registry configs (mainnet/, testnet-donut/)
```

## What It Does

### The Hub-and-Spoke Picture

Push Chain is the coordination layer in a hub-and-spoke crosschain model. Universal Validators (the off-chain `puniversald` worker — see [`universalClient/README.md`](../universalClient/README.md)) watch external chains, observe events, run TSS, and vote those observations onto Push Chain. The core validator is the hub: it tallies those votes, executes the resulting Push Chain logic, and emits the next round of work.

```
    Ethereum ----\                            /---- Ethereum
    Arbitrum -----\    +------------------+  /---- Arbitrum
    Base ---------->---|   Push Chain     |--<---- Base
    BSC ----------/    | (core validator) |  \---- BSC
    Solana ------/     +------------------+   \--- Solana

         Inbound           Tally + Execute        Outbound
    (UV votes inbound)    (PC executes UTX)   (UV signs + relays)
```

Two primitives drive this:

- **Inbound** — A gateway event observed on an external chain. Universal Validators wait for finality, then vote it via `MsgVoteInbound` on `x/uexecutor`. Once 2/3 vote the same observation, the core validator executes it on Push Chain (mints PRC20s, runs the user's payload through their UEA).
- **Outbound** — A transaction the core validator needs broadcast to an external chain (e.g. funds being unlocked from a vault). The pending outbound is picked up by Universal Validators, signed via TSS, broadcast, and the result is voted back via `MsgVoteOutbound`.

A single inbound's payload can spawn multiple outbounds; each outbound's destination event can become a new inbound. The core validator is the consistency point that keeps the whole graph deterministic.

### Custom Modules

Push Chain registers four custom Cosmos SDK modules.

#### `x/uexecutor` — Universal Transaction Executor

Lifecycle owner of every crosschain transaction (`UniversalTx`). Tallies inbound/outbound/chain-meta votes from Universal Validators, executes inbound payloads through the UEA factory, tracks pending outbounds, and writes chain-meta back to the EVM oracle.

**Messages**
- `MsgVoteInbound`, `MsgVoteOutbound`, `MsgVoteChainMeta` — bonded UV-only, gasless
- `MsgExecutePayload`, `MsgMigrateUEA` — any user, gasless (the UEA itself authenticates the request)
- `MsgUpdateParams` — gov-only

**State**
- `UniversalTx` — the canonical UTX record (inbound, PC tx, outbounds, status)
- `PendingInbounds` — secondary index of inbounds awaiting tally/execution
- `PendingOutbounds` — secondary index of outbounds in `PENDING` status
- `ChainMetas` — aggregated gas price + block height per CAIP-2 chain
- `ModuleAccountNonce` — manually managed nonce so the module can issue `DerivedEVMCall`s
- `GasPrices` — legacy, kept only for genesis import compatibility

**EVM integration** — Deploys the UEA factory on fresh genesis, then drives all on-chain crosschain logic (mint PRC20, swap quotes, refund gas, push chain meta) through `DerivedEVMCall` with manual nonce tracking. See [`x/uexecutor/README.md`](../x/uexecutor/README.md).

#### `x/uregistry` — Chain & Token Registry

Source of truth for which external chains and tokens Push Chain talks to. Admin-curated.

**Messages** (admin-only, where admin is `params.Admin`)
- `MsgAddChainConfig`, `MsgUpdateChainConfig`
- `MsgAddTokenConfig`, `MsgUpdateTokenConfig`, `MsgRemoveTokenConfig`
- `MsgUpdateParams` — gov-only

**State**
- `ChainConfigs` — per-CAIP-2 chain config (RPC URL, gateway, vault methods, block confirmations, inbound/outbound enabled flags, gas oracle interval)
- `TokenConfigs` — token whitelist by `chain:address`, with native representation, decimals, and liquidity cap

Deploys the universal system contracts (UniversalGatewayPC and reserved proxy slots) on fresh genesis. See [`x/uregistry/README.md`](../x/uregistry/README.md).

#### `x/uvalidator` — Universal Validator Management & Ballot Voting

The consensus layer for crosschain observations. Maintains the Universal Validator set, runs the generic ballot machine that all four modules vote through, and distributes a boosted reward share to active UVs.

**Messages**
- `MsgAddUniversalValidator`, `MsgRemoveUniversalValidator`, `MsgUpdateUniversalValidatorStatus` — admin-only
- `MsgUpdateUniversalValidator` — self (the validator updates its own crosschain identity)
- `MsgUpdateParams` — gov-only

**State**
- `UniversalValidatorSet` — registered UVs, keyed by `sdk.ValAddress`, with lifecycle status (`PENDING_JOIN` -> `ACTIVE` -> `PENDING_LEAVE`)
- `Ballots` — every ballot ever created (vote results, status, expiry)
- `ActiveBallotIDs`, `ExpiredBallotIDs`, `FinalizedBallotIDs` — index sets for fast lookup

**Generic ballot machine** — used by `x/uexecutor` (inbound/outbound/chain-meta) and `x/utss` (TSS events, fund migrations). A ballot is created on the first vote, finalizes as `PASSED` once `votingThreshold` matching votes are in, or `REJECTED` once enough opposite votes make the threshold unreachable.

**UV Reward Boost (BeginBlocker)** — Before the standard distribution module runs, `x/uvalidator` intercepts the FeeCollector balance and inflates effective voting power for active UVs by a `1.148x` multiplier. The extra `0.148x` portion is allocated proportionally to UVs and forwarded to the distribution module; the remaining fees flow back to the FeeCollector for normal proposer + community-pool + delegator distribution. Net effect: validators that also run a Universal Validator earn ~14.8% more block rewards. See [`x/uvalidator/README.md`](../x/uvalidator/README.md).

#### `x/utss` — Threshold Signature Scheme

Coordinates the lifecycle of the TSS key that signs every outbound transaction.

**Messages**
- `MsgInitiateTssKeyProcess`, `MsgInitiateFundMigration` — admin-only
- `MsgVoteTssKeyProcess`, `MsgVoteFundMigration` — bonded UV-only, gasless
- `MsgUpdateParams` — gov-only

**State**
- `CurrentTssProcess` / `ProcessHistory` — active and historical keygen/refresh/quorum-change processes
- `CurrentTssKey` / `TssKeyHistory` — finalized active key + every key that has ever existed
- `TssEvents` / `PendingTssEvents` — fine-grained events emitted during a process (used for vote routing)
- `FundMigrations` / `PendingMigrations` — old-key -> new-key fund moves on each external chain

**Process types**
- `KEYGEN` — produce a brand-new key with new on-chain addresses (triggers fund migration on every connected chain)
- `REFRESH` — redistribute fresh keyshares without changing the public key
- `QUORUM_CHANGE` — add/remove participants without changing the public key

See [`x/utss/README.md`](../x/utss/README.md).

### Custom EVM Precompile

Push Chain ships exactly one custom precompile:

| Address | Name | Purpose |
|---|---|---|
| `0x00000000000000000000000000000000000000ca` | `usigverifier` (legacy) | Ed25519 signature verification (Solana signatures over `bytes32` digests) |
| `0xEC00000000000000000000000000000000000001` | `usigverifier` (v2) | Same implementation, registered at the reserved Push range |

Both addresses are registered simultaneously for backward compatibility with deployed contracts that have the legacy address hardcoded. Gas cost: `4000` per `verifyEd25519` call. See [`precompiles/usigverifier/README.md`](../precompiles/usigverifier/README.md).

The baseline EVM precompiles (`bech32`, `p256`, `staking`, `distribution`, `ics20`, `bank`, `gov`, `slashing`, `evidence`) are wired in via `app/precompiles.go:NewAvailableStaticPrecompiles`.

### Transaction Pipeline — Gasless Support

Push Chain extends the Cosmos AnteHandler with three custom decorators that together enable **gasless transactions** for Universal Validators and UEA users. Without this, every Universal Validator would need to hold and manage gas tokens just to vote — defeating the point of having a permissioned UV set.

**The gasless whitelist** (`app/txpolicy/gasless.go`) — only these message types qualify:

```
/uexecutor.v1.MsgExecutePayload
/uexecutor.v1.MsgMigrateUEA
/uexecutor.v1.MsgVoteInbound
/uexecutor.v1.MsgVoteOutbound
/uexecutor.v1.MsgVoteChainMeta
/utss.v1.MsgVoteTssKeyProcess
/utss.v1.MsgVoteFundMigration
```

A tx is gasless only if **every** message (including those nested inside `authz.MsgExec`) is in the whitelist.

**Custom decorators**

| Decorator | File | Behavior on gasless tx |
|---|---|---|
| `MinGasPriceDecorator` | `app/cosmos/min_gas_price.go` | Skips the FeeMarket minimum-fee check entirely |
| `DeductFeeDecorator` | `app/ante/fee.go` | Skips fee deduction (no balance required) |
| `AccountInitDecorator` | `app/ante/account_init_decorator.go` | If signer has no on-chain account yet, creates it mid-pipeline with `account_number=0, sequence=0`, verifies the signature against those values, and short-circuits the rest of the ante chain |

The third decorator is what lets a freshly-keygen'd Universal Validator hot key vote on its very first tx, without anyone first having to fund it.

## Configuration

| | |
|---|---|
| Binary name | `pchaind` |
| Node home | `~/.pchain` |
| Bech32 prefixes | `push` (account) / `pushvaloper` (validator operator) / `pushvalcons` (consensus) |
| Coin type | `60` (Ethereum-compatible HD path) |
| Base denom | `upc` (18 decimals, EVM-aligned) |
| Default chain ID | `localchain_9000-1` (devnet); testnet uses `push_42101-1` |
| Exposed ports (Docker) | `1317` REST, `26656` P2P, `26657` Tendermint RPC, `8545` EVM JSON-RPC, `8546` EVM WS |

`app.toml` includes the standard `[evm]`, `[json-rpc]`, `[tls]`, and `[wasm]` sections required by the embedded EVM and JSON-RPC server. There are no Push-specific configuration knobs beyond those.

## Getting Started

**Prerequisites**

- [Go 1.23+](https://golang.org/dl/)
- [Docker](https://www.docker.com/) — required for `make proto-gen` and integration tests
- [Rust](https://www.rust-lang.org/tools/install) — required to build the DKLS23 native library that the Universal Validator binary links against (the core validator binary itself doesn't depend on it, but `make build` produces both)
- [jq](https://stedolan.github.io/jq/download/) — used by setup scripts

```bash
# One-time: build the DKLS23 native library
make build-dkls23

# Build pchaind (and puniversald) into ./build/
make build

# Or install both into $GOPATH/bin
make install

# Spin up a single-node local chain (uses scripts/test_node.sh + Cosmovisor)
make sh-testnet

# Run unit tests (sets LD_LIBRARY_PATH for the native TSS lib)
make test-unit

# Run with race detector
make test-race

# Regenerate protobuf bindings (must be inside Docker)
make proto-gen
```

### CLI

```bash
pchaind init <moniker> --chain-id push_42101-1   # initialize node home
pchaind start                                     # run validator/full node
pchaind status                                    # health check
pchaind export                                    # export app state to JSON

# Keys (cosmos-evm flavored — uses coin type 60)
pchaind keys add <name>
pchaind keys list
pchaind keys show <name>

# Custom module queries
pchaind q uexecutor params
pchaind q uregistry all-chain-configs
pchaind q uvalidator all-universal-validators
pchaind q uvalidator all-active-ballots
pchaind q utss current-key
pchaind q utss current-process
```

The full CLI surface is `pchaind <module> <tx|q> --help` — autocli definitions live in each module's `autocli.go`.
