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

### Primary Commands

| Command | Description |
|---------|-------------|
| `./devnet up` | Start validators (auto-pulls from cache if available) |
| `./devnet up --build` | Force rebuild images before starting |
| `./devnet up --skip-cache` | Skip remote cache, build locally |
| `./devnet down` | Stop all validators |
| `./devnet restart` | Restart all validators |
| `./devnet status` | Show network status |
| `./devnet logs [service]` | View logs (all or specific container) |

### Build Commands

| Command | Description |
|---------|-------------|
| `./devnet base` | Build/rebuild base image |
| `./devnet rebuild [target]` | Rebuild images (all\|core\|universal\|base\|clean) |

### TSS Commands

| Command | Description |
|---------|-------------|
| `./devnet tss-keygen` | Initiate TSS key generation |
| `./devnet tss-refresh` | Initiate TSS key refresh |
| `./devnet tss-quorum` | Initiate TSS quorum change |

### Setup Commands

| Command | Description |
|---------|-------------|
| `./devnet setup-authz` | Setup hot key and AuthZ grants |
| `./devnet verify-authz` | Verify AuthZ configuration |
| `./devnet setup-uvalidators` | Register universal validators + grants |
| `./devnet setup-registry` | Add chains and tokens to registry |
| `./devnet show-registry` | Display registered chains and tokens |

### Cache Commands

| Command | Description |
|---------|-------------|
| `./devnet pull-cache` | Pull pre-built images from GCR |
| `./devnet push-cache` | Push local images to GCR |
| `./devnet refresh-cache` | Force rebuild and push to GCR |

### Maintenance

| Command | Description |
|---------|-------------|
| `./devnet clean` | Complete reset (removes all data, with confirmation) |

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

## Docker Image Caching

The devnet uses a remote cache (Google Container Registry) to speed up builds for team members. This avoids rebuilding the base image (~15-20 min) on every machine.

### Images Cached

| Local Image | Remote Cache | Build Time |
|-------------|--------------|------------|
| `local-multi-validator-base:latest` | `gcr.io/push-chain-testnet/push-base:latest` | ~15-20 min |
| `push-core:latest` | `gcr.io/push-chain-testnet/push-core:latest` | ~2-3 min |
| `push-universal:latest` | `gcr.io/push-chain-testnet/push-universal:latest` | ~1 min |

### How Caching Works

**`./devnet up` (default behavior):**
```
1. Check if images exist locally
   ├─ YES → Start containers immediately
   └─ NO  → Try pulling from GCR cache
            ├─ SUCCESS → Tag as local, start containers (~2-3 min)
            └─ FAIL    → Build locally, then auto-push to cache
```

**`./devnet up --no-cache`:**
- Forces a complete rebuild from scratch
- Does NOT push to cache afterward
- Use when you need to rebuild everything (e.g., dependency updates)

**`./devnet up --skip-cache`:**
- Skips pulling from remote cache
- Builds locally if images missing
- Does NOT push to cache afterward
- Use when testing local changes you don't want cached

### Cache Commands

| Command | Pulls from GCR? | Builds Locally? | Pushes to GCR? |
|---------|-----------------|-----------------|----------------|
| `./devnet up` | Yes (if missing) | Yes (if pull fails) | Yes (background) |
| `./devnet up --no-cache` | No | Yes (forced) | No |
| `./devnet up --skip-cache` | No | Yes (if missing) | No |
| `./devnet rebuild core` | No | Yes (core only) | No |
| `./devnet push-cache` | No | No | Yes (manual) |

### Testing Local Changes

When you modify setup scripts or code and want to test locally without affecting the cache:

```bash
# Option 1: Rebuild specific image and start with docker compose directly
./devnet rebuild core
docker compose up -d

# Option 2: Use --skip-cache to avoid pulling old cached images
./devnet rebuild core
./devnet up --skip-cache
```

### Populating the Cache

After building locally, images are automatically pushed to GCR in the background. To manually push:

```bash
./devnet push-cache
```

### Custom Registry

Override the default GCR registry:
```bash
GCR_REGISTRY=gcr.io/your-project ./devnet up
```

### Quick Reference: Stop/Reset Commands

| Command | Volumes | Use Case |
|---------|---------|----------|
| `./devnet down` | Preserved | Quick stop, keep chain state |
| `./devnet down -v` | Removed | Clean stop, reset everything |
| `./devnet clean` | Removed | Same as above, with confirmation prompt |

**Note:** Without `-v`, validators keep their chain data. If you restart after code changes, this can cause state mismatches. Use `./devnet down -v` or `./devnet clean` for a fresh start.
