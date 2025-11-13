# TSS Demo

A demo of the TSS (Threshold Signature Scheme) system with 3 local nodes.

## Quick Start

### Run Nodes

Run the test script in separate terminals:

```bash
./scripts/test_tss.sh party-1 39001  # Terminal 1
./scripts/test_tss.sh party-2 39002  # Terminal 2
./scripts/test_tss.sh party-3 39003  # Terminal 3
```

The script automatically builds the binary to `build/tss` if needed.

### Commands

**KeyGen:**

```bash
./build/tss keygen -node=party-1 -key-id=demo-key-1
```

**KeyRefresh:**

```bash
./build/tss keyrefresh -node=party-1 -key-id=demo-key-1
```

**Sign:**

```bash
./build/tss sign -node=party-1 -key-id=demo-key-1 -message="hello world"
```

## Local Demo vs Production

| Aspect               | Local Demo                                         | Production                                  |
| -------------------- | -------------------------------------------------- | ------------------------------------------- |
| **Event Population** | CLI writes to all 3 node databases                 | Each node listens to on-chain events        |
| **Peer Discovery**   | File-based (`/tmp/tss-demo-peers.json.<party-id>`) | DHT or configured endpoints                 |
| **Participants**     | Hardcoded 3-node list                              | Loaded from on-chain validator registry     |
| **Block Numbers**    | Unix timestamp                                     | Real chain block numbers                    |
| **Databases**        | Separate per node (`/tmp/tss-<party-id>.db`)       | Separate per node (populated independently) |

## How It Works

1. **Event creation**: CLI creates `PENDING` events in all node databases
2. **Coordinator polling**: Each node polls its database for `PENDING` events (10+ blocks old)
3. **Coordinator selection**: Deterministic selection based on block number
4. **Session recovery**: Nodes recover sessions from database if message arrives before session registration
5. **Protocol execution**: Coordinator broadcasts setup, all participants execute DKLS protocol
6. **Status updates**: `PENDING` → `IN_PROGRESS` → `SUCCESS`/`FAILED`
