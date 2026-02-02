# Local Native Setup

Run Push Chain validators **natively** on your machine (no Docker).

## Prerequisites

1. Build the binaries:
```bash
cd push-chain
make build
```

This creates `build/pchaind` and `build/puniversald`.

## Quick Start

```bash
cd local-native

# Build binaries (if not done)
./devnet build

# Start all 4 core validators
./devnet start 4

# Check status
./devnet status

# Setup universal validators (registers UVs + creates AuthZ grants)
./devnet setup-uvalidators

# Start all 4 universal validators
./devnet start-uv 4

# Check full status (should show all 8 validators running)
./devnet status

# View logs
./devnet logs validator-1
./devnet logs universal-1

# Stop all
./devnet down
```

## Commands

| Command | Description |
|---------|-------------|
| `./devnet start [n]` | Start n core validators (default: 1) |
| `./devnet setup-uvalidators` | Register UVs on-chain + create AuthZ grants |
| `./devnet start-uv [n]` | Start n universal validators (default: 4) |
| `./devnet down` | Stop all validators |
| `./devnet status` | Show network status |
| `./devnet logs [service]` | View logs |
| `./devnet build` | Build binaries |
| `./devnet clean` | Complete reset |
| `./devnet help` | Show all commands |

**Workflow:** Start core validators → setup-uvalidators → start-uv

## Directory Structure

```
local-native/
├── devnet                # Main CLI (similar to docker version)
├── env.sh                # Environment configuration
├── scripts/
│   ├── generate-accounts.sh    # Generate test accounts
│   ├── setup-genesis-auto.sh   # Set up validator 1 (genesis)
│   ├── setup-validator-auto.sh # Set up validators 2-4
│   └── setup-universal.sh      # Set up universal validators
├── data/
│   ├── accounts/         # Generated accounts
│   │   ├── genesis_accounts.json
│   │   ├── validators.json
│   │   ├── hotkeys.json
│   │   └── genesis.json
│   ├── validator1/       # Validator 1 data
│   ├── validator2/       # Validator 2 data
│   ├── validator3/       # Validator 3 data
│   ├── validator4/       # Validator 4 data
│   ├── universal1/       # Universal validator 1 data
│   ├── universal2/       # Universal validator 2 data
│   ├── universal3/       # Universal validator 3 data
│   └── universal4/       # Universal validator 4 data
```

## Endpoints

### Validator 1 (Genesis)
| Service | Port | URL |
|---------|------|-----|
| RPC | 26657 | http://localhost:26657 |
| REST | 1317 | http://localhost:1317 |
| gRPC | 9090 | localhost:9090 |
| EVM HTTP | 8545 | http://localhost:8545 |
| EVM WS | 8546 | ws://localhost:8546 |

### Validator 2
| Service | Port |
|---------|------|
| RPC | 26658 |
| REST | 1318 |
| gRPC | 9093 |
| EVM | 8547 |

### Validator 3
| Service | Port |
|---------|------|
| RPC | 26659 |
| REST | 1319 |
| gRPC | 9095 |
| EVM | 8549 |

### Validator 4
| Service | Port |
|---------|------|
| RPC | 26660 |
| REST | 1320 |
| gRPC | 9097 |
| EVM | 8551 |

### Universal Validators
| Validator | API Port | TSS Port |
|-----------|----------|----------|
| Universal 1 | 8080 | 39000 |
| Universal 2 | 8081 | 39001 |
| Universal 3 | 8082 | 39002 |
| Universal 4 | 8083 | 39003 |

## Clean Reset

```bash
./devnet clean
```

## Logs

```bash
# List all log files
./devnet logs

# Follow specific validator logs
./devnet logs validator-1
./devnet logs v1           # shorthand

./devnet logs universal-1
./devnet logs u1           # shorthand
```

## Differences from Docker Setup

| Aspect | Docker (local-multi-validator) | Native (local-native) |
|--------|-------------------------------|----------------------|
| Binaries | Built in container | Built locally (`make build`) |
| Data | Docker volumes | `./data/` directory |
| Networking | Docker network | localhost ports |
| Isolation | Full container isolation | Runs on host |
| Startup | `./devnet start` | `./devnet start` |
| Commands | Same `./devnet` CLI | Same `./devnet` CLI |

## Troubleshooting

### Binaries not found
```bash
./devnet build
# or manually:
cd push-chain && make build
```

### Port already in use
```bash
# Find process using port 26657
lsof -i :26657

# Kill it
kill -9 <PID>

# Or kill all push chain processes
pkill -f pchaind
pkill -f puniversald
```

### Permission denied
```bash
chmod +x devnet scripts/*.sh
```

### View detailed logs
```bash
# Validator 1 logs
tail -f data/validator1/validator.log

# Universal validator 1 logs
tail -f data/universal1/universal.log
```
