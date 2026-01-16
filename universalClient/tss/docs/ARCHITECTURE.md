# TSS Architecture

## Package Structure

```
universalClient/tss/
├── dkls/              # Pure DKLS protocol execution (no networking)
├── networking/        # Networking abstraction layer
│   └── libp2p/       # libp2p networking implementation
├── coordinator/      # Coordinator logic (event polling, participant selection)
├── sessionmanager/   # Session management (DKLS session lifecycle)
├── keyshare/         # Encrypted keyshare storage
├── eventstore/       # Database access for TSS events
├── tss.go            # Main Node struct and orchestration
└── cmd/tss/          # Command-line tool
```

## Components

### `tss.go` (Root Node)

Main orchestration layer that coordinates all TSS components. The `Node` struct manages the lifecycle and coordinates between coordinator, session manager, networking, and event store.

**Key responsibilities:**

- Node initialization and lifecycle management
- Network setup and message routing
- Component coordination (coordinator, session manager, event store)
- Startup recovery (resets IN_PROGRESS events to PENDING on crash recovery)

**Key methods:**

- `NewNode()` - Initializes a new TSS node with configuration
- `Start()` - Starts the node, network, coordinator, and session manager
- `Stop()` - Gracefully shuts down the node
- `Send()` - Sends messages via the network layer

### `dkls/`

Pure DKLS protocol execution. Handles keygen, keyrefresh, and sign sessions. No networking or coordinator logic.

**Key responsibilities:**

- Manages DKLS sessions (keygen, keyrefresh, sign)
- Executes protocol steps
- Produces/consumes protocol messages
- Handles session state and cryptographic operations

**Files:**

- `keygen.go` - Keygen session implementation
- `keyrefresh.go` - Keyrefresh session implementation
- `sign.go` - Sign session implementation
- `types.go` - DKLS types and interfaces
- `utils.go` - Helper functions

### `networking/`

Networking abstraction layer with libp2p implementation.

**Key responsibilities:**

- Peer discovery and connection management
- Message routing via peer IDs
- Send/receive raw bytes
- Protocol-agnostic message handling

**Structure:**

- `types.go` - Networking interfaces
- `libp2p/` - libp2p-specific implementation
  - `network.go` - libp2p network implementation
  - `config.go` - Network configuration

### `coordinator/`

Handles coordinator logic for TSS events. Responsible for event polling, coordinator selection, and participant management.

**Key responsibilities:**

- Polls database for `PENDING` events
- Determines if this node is the coordinator for an event
- Selects participants based on protocol type
- Creates and broadcasts setup messages
- Tracks ACK messages from participants
- Manages validator registry and peer ID mapping

**Key methods:**

- `Start()` - Begins polling for events
- `IsCoordinator()` - Checks if this node is coordinator for an event
- `GetEligibleUV()` - Gets eligible validators for a protocol type
- `GetPeerIDFromPartyID()` - Maps validator address to peer ID

**Files:**

- `coordinator.go` - Main coordinator logic
- `types.go` - Coordinator types and interfaces
- `utils.go` - Helper functions (threshold calculation, etc.)

### `sessionmanager/`

Manages TSS protocol sessions and handles incoming messages. Bridges between coordinator messages and DKLS protocol execution.

**Key responsibilities:**

- Creates and manages DKLS sessions
- Handles incoming coordinator messages (setup, begin, step, ack)
- Validates participants and session state
- Processes protocol steps and routes messages
- Handles session expiry and cleanup
- Updates event status (PENDING → IN_PROGRESS → SUCCESS/FAILED)

**Key methods:**

- `HandleIncomingMessage()` - Routes incoming messages to appropriate handlers
- `handleSetupMessage()` - Creates new session from setup message
- `handleBeginMessage()` - Starts protocol execution
- `handleStepMessage()` - Processes protocol step
- `StartExpiryChecker()` - Background goroutine to check for expired sessions

**Files:**

- `sessionmanager.go` - Session management implementation

### `keyshare/`

Encrypted storage for keyshares and signatures. Uses password-based encryption.

**Key responsibilities:**

- Store and retrieve encrypted keyshares
- Key ID management
- Encryption/decryption of sensitive data

### `eventstore/`

Database access layer for TSS events. Provides methods for getting pending events, updating status, and querying events.

**Key responsibilities:**

- Query pending events
- Update event status
- Reset IN_PROGRESS events to PENDING (for crash recovery)
- Event expiry handling

**Key methods:**

- `GetPendingEvents()` - Gets events ready to be processed
- `UpdateStatus()` - Updates event status
- `UpdateStatusAndBlockHeight()` - Updates status and block height
- `ResetInProgressEventsToPending()` - Resets IN_PROGRESS events on startup
- `GetEventsByStatus()` - Queries events by status

**Files:**

- `store.go` - Event store implementation

### `cmd/tss/`

Command-line tool for running nodes and triggering operations.

**Commands:**

- `node` - Run a TSS node
- `keygen` - Trigger a keygen operation
- `keyrefresh` - Trigger a keyrefresh operation
- `sign` - Trigger a sign operation

## How It Works

### Node Startup

1. Node initializes with configuration (validator address, private key, database, etc.)
2. Node starts libp2p network
3. **Crash Recovery**: All `IN_PROGRESS` events are reset to `PENDING` (handles node crashes)
4. Coordinator starts polling for events
5. Session manager starts expiry checker
6. Node registers itself in `/tmp/tss-nodes.json` registry

### Event Processing Flow

1. **Event Detection**: Commands (keygen, keyrefresh, sign) discover nodes from registry and create events in databases
2. **Event Polling**: Each node's coordinator polls database for `PENDING` events
3. **Coordinator Selection**: Coordinator is selected deterministically based on block number
4. **Setup Phase**:
   - Coordinator creates setup message with participants
   - Coordinator broadcasts setup message to all participants
   - Participants create DKLS sessions and send ACK
5. **Begin Phase**:
   - Coordinator waits for all ACKs
   - Coordinator broadcasts begin message
   - Participants start protocol execution
6. **Protocol Execution**:
   - Participants exchange step messages via session manager
   - Session manager routes messages to DKLS sessions
   - DKLS sessions process steps and produce output messages
7. **Completion**:
   - Session finishes and produces result (keyshare or signature)
   - Session manager updates event status to `SUCCESS`
   - Keyshares are stored, signatures are saved

### Status Transitions

- `PENDING` → `IN_PROGRESS` (when setup message is received and session is created)
- `IN_PROGRESS` → `PENDING` (on node crash recovery or session expiry)
- `IN_PROGRESS` → `SUCCESS` (when protocol completes successfully)
- `IN_PROGRESS` → `FAILED` (on protocol error)
- `PENDING` → `EXPIRED` (if event expires before processing)

### Crash Recovery

On node startup, all `IN_PROGRESS` events are automatically reset to `PENDING`. This handles cases where:

- Node crashed while events were in progress
- Sessions were lost from memory
- Events remained in `IN_PROGRESS` state in database

The coordinator will then pick up these events again for processing.

## Coordinator Selection

Selected deterministically based on block number:

- **Formula**: `coordinator_index = (block_number / coordinator_range) % num_participants`
- **Default range**: 1000 blocks per coordinator
- **Rotation**: Coordinator rotates every `coordinator_range` blocks
- **Deterministic**: Same block number always selects the same coordinator

## Threshold Calculation

Automatically calculated as > 2/3 of participants:

- **Formula**: `threshold = floor((2 * n) / 3) + 1`
- **Examples**:
  - 3 participants → threshold 3
  - 4 participants → threshold 3
  - 5 participants → threshold 4
  - 6 participants → threshold 5
  - 7 participants → threshold 5
  - 8 participants → threshold 6
  - 9 participants → threshold 7

## Participant Selection

- **Keygen/Keyrefresh**: All eligible validators participate
- **Sign**: Exactly `threshold` participants are selected (deterministically based on event/block)

## Session Expiry

Sessions expire if inactive for a configurable duration (default: 3 minutes). When a session expires:

- Session is cleaned up from memory
- Event status is reset to `PENDING`
- Event block number is updated (current block + delay) for retry
- Coordinator will pick up the event again for processing
