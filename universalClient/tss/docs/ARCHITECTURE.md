# TSS Architecture

## Package Structure

```
universalClient/tss/
├── dkls/          # Pure DKLS protocol execution (no networking)
├── networking/    # libp2p networking layer (protocol-agnostic)
├── node/          # Orchestration layer (coordinates dkls + networking)
├── keyshare/      # Encrypted keyshare storage
├── eventstore/    # Database access for TSS events
└── cmd/tss/       # Command-line tool
```

## Components

### `dkls/`

Pure DKLS protocol execution. Handles keygen, keyrefresh, and sign sessions. No networking or coordinator logic.

**Key responsibilities:**

- Manages DKLS sessions
- Executes protocol steps
- Produces/consumes protocol messages

### `networking/`

libp2p networking layer. Protocol-agnostic - only handles raw bytes.

**Key responsibilities:**

- Peer discovery and connection management
- Message routing via peer IDs
- Send/receive raw bytes

### `node/`

Orchestration layer that coordinates DKLS protocol and networking.

**Key responsibilities:**

- Event polling and processing
- Coordinator selection
- Coordinates dkls sessions + networking
- Updates event status

**Files:**

- `node.go` - Node initialization and lifecycle
- `coordinator.go` - Event polling and processing
- `keygen.go` - Keygen operation (coordinates dkls + networking)
- `types.go` - Types and interfaces
- `utils.go` - Helper functions

### `keyshare/`

Encrypted storage for keyshares and signatures. Uses password-based encryption.

### `eventstore/`

Database access layer for TSS events. Provides methods for getting pending events, updating status, and querying events.

### `cmd/tss/`

Command-line tool for running nodes and triggering operations.

## How It Works

1. Nodes register themselves in `/tmp/tss-nodes.json` on startup
2. Commands discover nodes from registry and update databases
3. Each node polls database for `PENDING` events
4. Coordinator is selected deterministically based on block number
5. Coordinator creates and broadcasts setup message to all participants
6. All nodes execute DKLS protocol via networking layer
7. Status updates: `PENDING` → `IN_PROGRESS` → `SUCCESS`/`FAILED`

## Coordinator Selection

Selected deterministically based on block number:

- **Formula**: `coordinator_index = (block_number / coordinator_range) % num_participants`
- **Default range**: 100 blocks per coordinator
- **Rotation**: Coordinator rotates every `coordinator_range` blocks

## Threshold Calculation

Automatically calculated as > 2/3 of participants:

- **Formula**: `threshold = floor((2 * n) / 3) + 1`
- **Examples**: 3→3, 4→3, 5→4, 6→5, 7→5, 8→6, 9→7
