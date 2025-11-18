# TSS Architecture

This document describes the structure of the `universalClient/tss` package.

## Package Structure

```
universalClient/tss
├── core/         # TSS service and protocol execution
├── transport/    # libp2p networking
├── coordinator/  # Database-driven event processing
├── keyshare/     # Encrypted keyshare storage
└── cmd/tss/      # Command-line tool
```

### `core/`

TSS service that handles keygen, keyrefresh, and signing operations. Uses `UniversalValidator` for participants and selects coordinators deterministically based on block numbers.

### `transport/`

libp2p transport for peer-to-peer communication. Handles peer discovery, connection management, and message routing.

### `keyshare/`

Encrypted storage for keyshares and signatures.

### `coordinator/`

Polls database for TSS events and triggers operations. Uses `PushChainDataProvider` to discover validators:

- `GetLatestBlockNum()` - Current block number
- `GetUniversalValidators()` - All validators
- `GetUniversalValidator()` - Specific validator by address

### `cmd/tss/`

Command-line tool for running nodes and triggering operations. Nodes register themselves in `/tmp/tss-nodes.json` for discovery.

## How It Works

1. Nodes register in registry file on startup
2. CLI creates `PENDING` events in node databases
3. Coordinator polls for events and discovers validators via `PushChainDataProvider`
4. Coordinator selected deterministically based on block number
5. All nodes register sessions and execute DKLS protocol
6. Results stored and status updated

## Component Flow

```
┌─────────────────────────────────────────────────────────────┐
│                      Coordinator                             │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Polls DB for PENDING events                         │   │
│  │  Uses PushChainDataProvider to get validators        │   │
│  │  Selects coordinator deterministically               │   │
│  │  Calls core.Service methods                          │   │
│  └──────────────────┬───────────────────────────────────┘   │
└─────────────────────┼───────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                      Core Service                           │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Manages TSS sessions                                │   │
│  │  Executes DKLS protocol (keygen/keyrefresh/sign)     │   │
│  │  Handles coordinator selection                       │   │
│  └──────┬───────────────────────┬───────────────────────┘   │
│         │                       │                           │
│         ▼                       ▼                           │
│  ┌──────────────┐      ┌──────────────┐                     │
│  │  Transport   │      │ KeyshareStore│                     │
│  │  (libp2p)    │      │  (encrypted) │                     │
│  └──────────────┘      └──────────────┘                     │
└─────────────────────────────────────────────────────────────┘
         │
         │ Sends/receives messages
         ▼
┌─────────────────────────────────────────────────────────────┐
│                    Transport (libp2p)                       │
│  - Peer discovery and connection management                 │
│  - Message routing via peer IDs                             │
│  - Handles network layer                                    │
└─────────────────────────────────────────────────────────────┘
```

**Flow:**

1. Coordinator polls database → finds PENDING events
2. Coordinator queries PushChainDataProvider → gets validators
3. Coordinator calls core.Service → triggers TSS operation
4. Core Service uses Transport → sends/receives messages
5. Core Service uses KeyshareStore → saves keyshares/signatures
6. Coordinator updates database → event status (SUCCESS/FAILED)

## Demo

For local demo setup and usage, see [cmd/tss/README.md](../../../cmd/tss/README.md).

## Production vs Demo

| Aspect                    | Demo                    | Production            |
| ------------------------- | ----------------------- | --------------------- |
| **Node Discovery**        | File registry           | On-chain registry     |
| **Event Source**          | CLI writes to databases | On-chain events       |
| **Block Numbers**         | Unix timestamp          | Chain block numbers   |
| **PushChainDataProvider** | Reads registry file     | Queries on-chain data |
