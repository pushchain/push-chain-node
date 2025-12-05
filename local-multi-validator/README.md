# Local Multi-Validator Setup

## Prerequisites

Run from the parent directory containing:
```
PUSH/
├── push-chain/          # This repo
├── dkls23-rs/           # TSS library (clone from silence-laboratories)
└── garbling/            # Garbling circuits (clone from silence-laboratories)
```

Build the base image first (one-time, ~5-10 min):
```bash
cd push-chain/local-multi-validator
./devnet base
```

## Quick Start

```bash
cd local-multi-validator

# Build and start (first time ~5-8 min, rebuilds ~1-2 min)
./devnet up

# Or use docker compose directly
docker compose up --build
```

## Commands

| Command | Description |
|---------|-------------|
| `./devnet up` | Build and start all validators |
| `./devnet down` | Stop all validators |
| `./devnet logs` | View logs (all or specific container) |
| `./devnet status` | Show container status |
| `./devnet tss-keygen` | Initiate TSS key generation |
| `./devnet tss-refresh` | Initiate TSS key refresh |
| `./devnet tss-quorum` | Initiate TSS quorum change |

## Endpoints

| Service | Port | Description |
|---------|------|-------------|
| Core RPC | 26657 | Tendermint RPC |
| Core REST | 1317 | REST API |
| Core gRPC | 9090 | gRPC |
| EVM HTTP | 8545 | EVM JSON-RPC |
| Universal API | 8080-8082 | Query API |
| TSS P2P | 39000-39002 | TSS communication |

## How It Works

The setup runs 6 validators:
- **3 Core Validators** (`pchaind`) - Consensus and block production
- **3 Universal Validators** (`puniversald`) - Off-chain compute with TSS signing

Each universal validator connects to its paired core validator via gRPC.
