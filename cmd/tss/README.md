# TSS Demo

A demo of the TSS (Threshold Signature Scheme) system with dynamic node discovery.

## Quick Start

### Run Nodes

Start nodes using the test script:

```bash
./scripts/test_tss.sh <validator-address> <port> <private-key-hex>
```

Each node automatically registers in `/tmp/tss-nodes.json` and discovers other nodes.

### Commands

All commands automatically update all node databases. Just provide `-key-id`:

```bash
./build/tss keygen -key-id=demo-key-1
./build/tss keyrefresh -key-id=demo-key-1
./build/tss sign -key-id=demo-key-1
```

For keygen, `-key-id` is optional (auto-generated if not provided).

## How It Works

1. Nodes register themselves in `/tmp/tss-nodes.json` on startup
2. Commands discover all nodes from the registry and update their databases
3. Each node polls its database for `PENDING` events
4. Coordinator is selected deterministically based on block number
5. All nodes execute the DKLS protocol
6. Status updates: `PENDING` → `IN_PROGRESS` → `SUCCESS`/`FAILED`

## Sample script to run 3 Nodes

```bash
# Terminal 1
./scripts/test_tss.sh pushvaloper1fv2fm76q7cjnr58wdwyntzrjgtc7qya6n7dmlu 39001 30B0D912700C3DF94F4743F440D1613F7EA67E1CEF32C73B925DB6CD7F1A1544

# Terminal 2
./scripts/test_tss.sh pushvaloper12jzrpp4pkucxxvj6hw4dfxsnhcpy6ddty2fl75 39002 59BA39BF8BCFE835B6ABD7FE5208D8B8AEFF7B467F9FE76F1F43ED392E5B9432

# Terminal 3
./scripts/test_tss.sh pushvaloper1vzuw2x3k2ccme70zcgswv8d88kyc07grdpvw3e 39003 957590C7179F8645368162418A3DF817E5663BBC7C24D0EFE1D64EFFB11DC595
```
