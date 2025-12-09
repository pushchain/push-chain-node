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
./devnet start

# Or use docker compose directly
docker compose up --build
```

## Common Developer Workflows

### First Time Setup
```bash
./devnet start
```
- Auto-builds base image if missing (~15-20 min first time)
- Pulls core/universal from cache or builds locally
- Starts all 6 validators

### I Changed Core Validator Code
**Files:** `cmd/pchaind/`, `app/`, `x/` modules

```bash
./devnet rebuild core    # Rebuild pchaind binary (~30 seconds)
./devnet restart         # Restart with new binary
```

### I Changed Universal Validator Code
**Files:** `cmd/puniversald/`, `universalClient/`

```bash
./devnet rebuild universal    # Rebuild puniversald binary (~10-15 seconds)
./devnet restart              # Restart with new binary
```

### I Changed Code in Multiple Modules
**Files:** Both core and universal code

```bash
./devnet rebuild all     # Rebuild both binaries (~1 minute)
./devnet restart         # Restart validators
```

Or rebuild + restart in one command:
```bash
./devnet start --build
```

### I Updated Dependencies (go.mod, Cargo.toml)
```bash
./devnet rebuild clean    # Full rebuild including base (~20-25 minutes)
./devnet start            # Start validators
```

### I Want a Fresh Start (Reset Chain State)
```bash
./devnet clean    # Stops, removes containers + volumes, prompts for confirmation
./devnet start    # Start fresh
```

### I'm Testing Changes Before Sharing
```bash
# Edit code...

./devnet rebuild core     # Build without pushing to cache
docker compose up -d      # Start containers directly

# Test...

# When ready to share with team:
./devnet push-cache       # Push to GCR for team
```

## Command Reference

### Essential Commands

| Command | Description | When to Use |
|---------|-------------|-------------|
| `./devnet start` | Build (if needed) and start validators | First time or after stopping |
| `./devnet stop` | Pause validators (keep containers) | Quick pause, fastest restart |
| `./devnet down` | Stop and remove containers (keep data) | Normal shutdown |
| `./devnet clean` | Stop and remove everything (reset chain) | Fresh start needed |
| `./devnet restart` | Restart all validators | After rebuild |
| `./devnet status` | Show validator health and block height | Check if running |
| `./devnet logs [service]` | View container logs | Debugging |

### Build Commands

| Command | Rebuilds | Build Time | Use Case |
|---------|----------|------------|----------|
| `./devnet rebuild core` | pchaind only | ~30s | Changed core code |
| `./devnet rebuild universal` | puniversald only | ~10-15s | Changed universal code |
| `./devnet rebuild all` | Both binaries | ~1 min | Changed multiple modules |
| `./devnet rebuild clean` | Everything (base + binaries) | ~20-25 min | Dependency updates |
| `./devnet start --build` | Core + universal, then start | ~1-2 min | Rebuild and restart in one |

### Advanced Commands

| Command | Description |
|---------|-------------|
| `./devnet base` | Rebuild base image only |
| `./devnet setup-authz` | Setup hot key and AuthZ grants |
| `./devnet verify-authz` | Verify AuthZ configuration |
| `./devnet setup-uvalidators` | Register universal validators + grants |
| `./devnet setup-registry` | Add chains and tokens to registry |
| `./devnet show-registry` | Display registered chains and tokens |
| `./devnet tss-keygen` | Initiate TSS key generation |
| `./devnet tss-refresh` | Initiate TSS key refresh |
| `./devnet tss-quorum` | Initiate TSS quorum change |
| `./devnet pull-cache` | Pull pre-built images from GCR |
| `./devnet push-cache` | Push local images to GCR |
| `./devnet refresh-cache` | Force rebuild and push to GCR |

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

## Docker Build System

### Image Architecture

The build system uses a layered approach optimized for fast iteration:

```
┌─────────────────────────────────────┐
│  local-multi-validator-base:latest  │  ← Dependencies (Go, Rust, libs)
│  Build time: ~15-20 min             │  ← Rarely changes
│  Auto-builds on first ./devnet start│
└──────────────┬──────────────────────┘
               │
      ┌────────┴────────┐
      ↓                 ↓
┌──────────┐      ┌──────────────┐
│ Core     │      │ Universal    │
│ (pchaind)│      │(puniversald) │    ← Built from same Dockerfile.unified
│ ~30s     │      │ ~10-15s      │    ← Share Go compilation cache (70% faster)
└──────────┘      └──────────────┘
```

### When to Rebuild Each Image

| Image | Rebuild When | Command |
|-------|--------------|---------|
| **base** | Dependencies changed (go.mod, Cargo.toml) | `./devnet rebuild clean` |
| **core** | Code in `cmd/pchaind/`, `app/`, `x/` | `./devnet rebuild core` |
| **universal** | Code in `cmd/puniversald/`, `universalClient/` | `./devnet rebuild universal` |
| **both** | Multiple modules changed | `./devnet rebuild all` |

**Key insight:** You almost never need to rebuild base manually - only when dependencies change.

### Build Behavior Matrix

| Command | Pulls from GCR? | Builds Locally? | Pushes to GCR? | Starts Validators? |
|---------|----------------|-----------------|----------------|-------------------|
| `./devnet start` | Yes (if missing) | Yes (if pull fails) | Yes (background) | ✓ |
| `./devnet start --build` | No | Yes (forced) | Yes (background) | ✓ |
| `./devnet start --skip-cache` | No | Yes (if missing) | No | ✓ |
| `./devnet rebuild core` | No | Yes (core only) | No | ✗ |
| `./devnet push-cache` | No | No | Yes (manual) | ✗ |

### Remote Cache (GCR)

Pre-built images are cached in Google Container Registry to speed up builds for team members:

- **Default registry:** `gcr.io/push-chain-testnet/push-{base,core,universal}:latest`
- **Override:** `GCR_REGISTRY=gcr.io/your-project ./devnet start`
- **Cache hit:** Pull image in ~2-3 min instead of building (~20 min for base)
- **Cache miss:** Build locally, auto-push to cache for next team member

**First-time setup (team member):**
```bash
./devnet start    # Pulls from cache, starts in ~5 min (vs ~25 min building)
```

**First-time setup (no cache access):**
```bash
./devnet start    # Builds locally, starts in ~25 min, pushes to cache
```

## Quick Reference

### Stop/Restart/Reset Commands

| Command | Containers | Volumes | Chain Data | Use Case |
|---------|-----------|---------|------------|----------|
| `./devnet stop` | Kept | Kept | Kept | Pause validators (fastest restart) |
| `./devnet down` | Removed | Kept | Kept | Stop and remove containers |
| `./devnet down -v` | Removed | Removed | Removed | Clean stop, reset everything |
| `./devnet clean` | Removed | Removed | Removed | Same as `down -v`, with confirmation prompt |

**Command Hierarchy:**
- **`stop`**: Gentlest - just pauses containers, quick restart with `./devnet start`
- **`down`**: Removes containers but keeps volumes/data
- **`down -v`**: Removes containers AND volumes (full reset)
- **`clean`**: Same as `down -v` but prompts for confirmation and cleans account files

**Note:** After code changes, use `./devnet down -v` or `./devnet clean` for a fresh start to avoid state mismatches.
