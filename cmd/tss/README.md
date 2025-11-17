# TSS Demo

A demo of the TSS (Threshold Signature Scheme) system with dynamic node discovery.

## Quick Start

### Run Nodes

You can run any number of nodes - each node registers itself in a shared registry file (`/tmp/tss-nodes.json`) and automatically discovers other nodes.

**Using the test script:**

```bash
# Terminal 1
./scripts/test_tss.sh pushvaloper1fv2fm76q7cjnr58wdwyntzrjgtc7qya6n7dmlu 39001 30B0D912700C3DF94F4743F440D1613F7EA67E1CEF32C73B925DB6CD7F1A1544

# Terminal 2
./scripts/test_tss.sh pushvaloper12jzrpp4pkucxxvj6hw4dfxsnhcpy6ddty2fl75 39002 59BA39BF8BCFE835B6ABD7FE5208D8B8AEFF7B467F9FE76F1F43ED392E5B9432

# Terminal 3
./scripts/test_tss.sh pushvaloper1vzuw2x3k2ccme70zcgswv8d88kyc07grdpvw3e 39003 957590C7179F8645368162418A3DF817E5663BBC7C24D0EFE1D64EFFB11DC595
```

The script requires all 3 arguments:

1. Validator address (required)
2. P2P port (required)
3. Private key in hex (required)

The script automatically builds the binary to `build/tss` if needed.

### Commands

**KeyGen:**

```bash
./build/tss keygen -node=pushvaloper1fv2fm76q7cjnr58wdwyntzrjgtc7qya6n7dmlu -key-id=demo-key-1
```

**KeyRefresh:**

```bash
./build/tss keyrefresh -node=pushvaloper1fv2fm76q7cjnr58wdwyntzrjgtc7qya6n7dmlu -key-id=demo-key-1
```

**Sign:**

```bash
./build/tss sign -node=pushvaloper1fv2fm76q7cjnr58wdwyntzrjgtc7qya6n7dmlu -key-id=demo-key-1 -message="hello world"
```

**Note:** The `-node` flag now expects a validator address, not a party ID.

## How Dynamic Discovery Works

1. **Node Registration**: When a node starts, it registers itself in `/tmp/tss-nodes.json` with:

   - Validator address
   - Peer ID (from libp2p)
   - Multiaddrs (listening addresses)
   - Last updated timestamp

2. **Dynamic Discovery**: `GetUniversalValidators` reads from the registry file, so all nodes automatically discover each other without any hardcoded configuration.

3. **Scalability**: You can run any number of nodes - just start them and they'll be discovered automatically.

## Local Demo vs Production

| Aspect               | Local Demo                                                  | Production                                  |
| -------------------- | ----------------------------------------------------------- | ------------------------------------------- |
| **Event Population** | CLI writes to all node databases (discovered from registry) | Each node listens to on-chain events        |
| **Peer Discovery**   | File-based registry (`/tmp/tss-nodes.json`)                 | On-chain network info or DHT                |
| **Participants**     | Dynamic - loaded from registry file                         | Loaded from on-chain validator registry     |
| **Block Numbers**    | Unix timestamp                                              | Real chain block numbers                    |
| **Databases**        | Separate per node (`/tmp/tss-<validator-address>.db`)       | Separate per node (populated independently) |

## How It Works

1. **Node startup**: Each node registers itself in `/tmp/tss-nodes.json` with its validator address, peer ID, and multiaddrs
2. **Dynamic discovery**: `GetUniversalValidators` reads from the registry file to discover all active nodes
3. **Event creation**: CLI creates `PENDING` events in all discovered node databases
4. **Coordinator polling**: Each node polls its database for `PENDING` events (10+ blocks old)
5. **Coordinator selection**: Deterministic selection based on block number
6. **Session recovery**: Nodes recover sessions from database if message arrives before session registration
7. **Protocol execution**: Coordinator broadcasts setup, all participants execute DKLS protocol
8. **Status updates**: `PENDING` → `IN_PROGRESS` → `SUCCESS`/`FAILED`

## Registry File

The registry file (`/tmp/tss-nodes.json`) is automatically created and updated when nodes start. It contains:

```json
{
  "nodes": [
    {
      "validator_address": "pushvaloper1...",
      "peer_id": "12D3KooW...",
      "multiaddrs": ["/ip4/127.0.0.1/tcp/39001"],
      "last_updated": "2024-01-01T12:00:00Z"
    }
  ]
}
```

Each node updates its entry when it starts, and `GetUniversalValidators` reads from this file to discover all active nodes.
