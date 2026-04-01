# Universal Client

Specialized validator node that powers Push Chain's universal crosschain capabilities.

- **Reads** external chain state (EVM, Solana) and bridges it onto Push Chain via consensus-driven observation and voting
- **Writes** to external chains by collectively signing outbound transactions using threshold cryptography (DKLS TSS) - no single validator holds full signing authority

## Architecture

```
UniversalClient
|-- api/               HTTP health/query endpoints
|-- chains/            Multi-chain lifecycle (create/update/remove per-chain clients)
|   |-- common/        Shared interfaces (ChainClient, TxBuilder)
|   |-- evm/           Ethereum-compatible chains
|   |-- svm/           Solana
|   +-- push/          Push Chain internal events
|-- config/            Configuration loading and validation
|-- core/              Main entry point and lifecycle orchestrator
|-- db/                SQLite persistence (per-chain databases)
|-- logger/            Structured logging (zerolog)
|-- pushcore/          Push Chain gRPC client (queries, broadcast)
|-- pushsigner/        HotKey management, AuthZ-wrapped signing, voting
|-- store/             Data models and event constants
+-- tss/               Threshold Signature Scheme (DKLS protocol over libp2p)
    |-- coordinator/   Event routing, nonce assignment, setup messages
    |-- sessionmanager/ DKLS protocol sessions
    |-- txbroadcaster/ Signed tx broadcast to external chains
    |-- txresolver/    On-chain verification and terminal status
    |-- expirysweeper/ Expired event cleanup and failure voting
    |-- keyshare/      Encrypted keyshare storage
    |-- eventstore/    Event state queries
    |-- networking/    P2P communication (libp2p)
    +-- dkls/          DKLS protocol bindings
```

## What It Does

### Multi-Chain Execution

Push Chain operates as a **hub-and-spoke model** for crosschain execution. Every crosschain operation flows through Push Chain as the coordination layer, rather than chains talking to each other directly.

```
    Ethereum ----\                       /---- Ethereum
    Arbitrum -----\    +-----------+    /---- Arbitrum
    Base ---------->---|  Push     |---<----- Base
    BSC ----------/    |  Chain    |    \---- BSC
    Solana ------/     +-----------+     \--- Solana

        Inbound            Hub            Outbound
   (external -> Push)                (Push -> external)
```

Two primitives make this work:

- **Inbound** - an event observed on an external chain and bridged onto Push Chain. Universal Validators watch gateway contracts, confirm finality, and vote the observation into consensus. Once agreed upon, the inbound is executed on Push Chain (e.g., minting bridged tokens to the recipient).

- **Outbound** - a transaction that Push Chain needs to execute on an external chain. Created as a result of Push Chain logic (smart contract calls, payload execution). Validators collectively sign and broadcast it to the destination chain using TSS.

A single inbound can trigger Push Chain logic that produces multiple outbounds to different chains. Each of those outbounds, once executed, can emit events that become new inbounds - enabling composable crosschain workflows that fan out and converge across any connected network.

#### Inbound Flow

```
External Chain                    Universal Validators                Push Chain
      |                                   |                               |
      |  Gateway event emitted            |                               |
      |---------------------------------->|                               |
      |                                   |  Wait for confirmations       |
      |                                   |  (fast: 2, standard: 12)      |
      |                                   |                               |
      |                                   |  Vote observation (2/3+)      |
      |                                   |------------------------------>|
      |                                   |                               |
      |                                   |                     Execute inbound
      |                                   |                     (mint tokens,
      |                                   |                      run payload)
```

1. The EVM/SVM event listener detects a gateway event on the source chain
2. The event confirmer waits for sufficient block confirmations to ensure finality
3. The event processor votes the observation onto Push Chain via `VoteInbound`
4. Once 2/3+ validators agree, Push Chain executes the inbound (deposits funds, runs payload)
5. If the execution produces outbound events, those become pending outbounds

#### Outbound Flow

```
Push Chain                        Universal Validators               External Chain
      |                                   |                               |
      |  Pending outbound created         |                               |
      |---------------------------------->|                               |
      |                                   |  Coordinator assigns nonce    |
      |                                   |  Selects threshold subset     |
      |                                   |  DKLS signing session         |
      |                                   |                               |
      |                                   |  Broadcast signed tx          |
      |                                   |------------------------------>|
      |                                   |                               |
      |                                   |  Monitor for confirmation     |
      |                                   |<------------------------------|
      |                                   |                               |
      |                    Vote result    |                               |
      |<----------------------------------|                               |
```

1. The Push Chain listener picks up the pending outbound
2. A rotating coordinator assigns a nonce, selects a threshold subset of participants, and creates a DKLS signing session
3. Each participant independently verifies the signing request against their own RPC view of the destination chain, then collaborates in the distributed signing protocol
4. Every participating validator broadcasts the identical signed transaction; the first to land wins, the rest are idempotent (same nonce, same signature, same tx hash)
5. The resolver monitors the destination chain for confirmation
6. On success, the event is marked complete. On failure (reverted or not found after retries), validators vote failure on Push Chain, which triggers a refund to the user

### Chain Meta Oracle

One of the most powerful aspects of the Universal Client is its ability to bring external chain state onto Push Chain in a trust-minimized way. The Chain Meta Oracle is the first expression of this: a decentralized oracle where every Universal Validator independently reads chain metadata and votes it into consensus.

Today, each validator periodically fetches:

- **Gas price** (EVM) or **prioritization fee** (Solana) - so the protocol knows what it costs to transact on each chain
- **Block height** or **slot number** - so Push Chain tracks the latest state of every connected network

These readings are voted onto Push Chain through the same consensus mechanism used for crosschain transfers. No single validator's view is trusted; the protocol converges on what the majority observed.

### TSS Key Lifecycle Management

The TSS key is the collective signing authority behind every outbound transaction on every external chain. No single validator ever holds the full key; each holds an encrypted keyshare, and a threshold (2/3+) must collaborate to produce a valid signature. Keeping this key secure is foundational to the protocol's safety.

Three operations maintain the key, all admin-initiated on Push Chain. For each, the coordinator creates a DKLS setup message, participants run the distributed protocol, store their encrypted keyshare locally (AES-256-GCM with PBKDF2), and vote the result on Push Chain to finalize.

#### Keygen

Produces a completely new key with a new public key and new on-chain addresses. This is done as a periodic security rotation to limit the exposure window of any single key. Even if a keyshare were compromised, rotating the key renders it useless. All participants receive fresh keyshares.

Since keygen creates a new address, funds on external chains still sit under the old key. The protocol handles this automatically via fund migration, transferring funds from the old address to the new one.

#### Key Refresh

Redistributes new keyshares without changing the public key or on-chain addresses. Every validator receives a fresh share, instantly invalidating all previous shares. This provides a lightweight security uplift between full keygen rotations - no fund migration, no downtime.

#### Quorum Change

Triggered when a Universal Validator joins or leaves the network. Incoming validators receive a keyshare, and departing validators' shares are discarded. The public key and addresses remain the same, so no fund migration is needed. This ensures the signing set always reflects the current active validator membership.

## Getting Started

> **Note:** The Universal Validator set is currently permissioned. Only nodes approved by the Push team are eligible. Permissionless participation is on the roadmap as the network matures.

**Prerequisites:**

- Active Push Chain validator node approved as a Universal Validator
- AuthZ grants from the validator (granter) to a hot key (grantee) for all required message types:
  ```
  /uexecutor.v1.MsgVoteInbound
  /uexecutor.v1.MsgVoteChainMeta
  /uexecutor.v1.MsgVoteOutbound
  /utss.v1.MsgVoteTssKeyProcess
  /utss.v1.MsgVoteFundMigration
  ```
- External chain RPC endpoints (Ethereum, Arbitrum, Base, BSC, Solana)

```bash
# Build
go build -o puniversald ./cmd/puniversald

# Initialize config with defaults
puniversald init --home ~/.puniversal

# Edit ~/.puniversal/config/pushuv_config.json:
#   - Replace default RPCs with paid endpoints (QuickNode, Alchemy, etc.)
#   - Set strong passwords for keyring and TSS
#   - Configure chain-specific settings (gas markup, polling intervals)
#
# For Solana: place a relayer keypair at ~/.puniversal/relayer/solana.json
# (standard Solana keypair format: JSON array of 64 bytes).
# Fund this address with a small amount of SOL - the relayer pays for Solana
# tx fees, but each outbound tx reimburses the gas back, so funding is one-time.

# Start the universal validator
puniversald start --home ~/.puniversal

# Run tests
go test ./universalClient/...
```
