# Cosmovisor Setup Guide

Cosmovisor is a process manager for Cosmos SDK chains that handles binary upgrades automatically.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Directory Structure](#directory-structure)
- [Creating an Upgrade Proposal](#creating-an-upgrade-proposal)
- [Manual Upgrade (Without Governance)](#manual-upgrade-without-governance)
- [Troubleshooting](#troubleshooting)
- [Quick Reference](#quick-reference)

---

## Prerequisites

- Go 1.22+ installed
- Push Chain binary (`pchaind`) in `~/app/binary/`
- Chain initialized with `pchaind init`

Install Cosmovisor:
```bash
go install cosmossdk.io/tools/cosmovisor/cmd/cosmovisor@latest
```

Verify:
```bash
cosmovisor version
```

---

## Quick Start

Use the helper scripts to start/stop nodes with Cosmovisor:

```bash
# Start node with Cosmovisor
./start_cosmovisor.sh

# Stop node
./stop_cosmovisor.sh

# View logs
./show_logs.sh
```

The `start_cosmovisor.sh` script automatically:
- Sets up Cosmovisor environment variables
- Creates the required directory structure
- Copies the binary to the genesis directory
- Starts the node via Cosmovisor

---

## Directory Structure

Cosmovisor uses this structure (created automatically by the script):

```
~/.pchain/
├── config/
├── data/
└── cosmovisor/
    ├── genesis/
    │   └── bin/
    │       └── pchaind        # Initial binary
    └── upgrades/
        └── <upgrade-name>/
            └── bin/
                └── pchaind    # Upgraded binary (auto-downloaded)
```

---

## Creating an Upgrade Proposal

### Step 1: Generate Upgrade Info JSON

```bash
./cosmovisor-upgrade-info.sh v1.0.0 https://github.com/pushchain/push-chain-node/releases/download/v1.0.0
```

Output:
```json
{
  "binaries": {
    "linux/amd64": "https://github.com/.../push-chain_1.0.0_linux_amd64.tar.gz?checksum=sha256:abc123...",
    "linux/arm64": "https://github.com/.../push-chain_1.0.0_linux_arm64.tar.gz?checksum=sha256:def456...",
    "darwin/arm64": "https://github.com/.../push-chain_1.0.0_darwin_arm64.tar.gz?checksum=sha256:789ghi..."
  }
}
```

Cosmovisor auto-detects OS/architecture and downloads the correct binary.

### Step 2: Create Upgrade Proposal File

Create `upgrade-proposal.json`:

```json
{
  "messages": [{
    "@type": "/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade",
    "authority": "push10d07y265gmmuvt4z0w9aw880jnsr700jzqqyzm",
    "plan": {
      "name": "v1.0.0",
      "height": "1000000",
      "info": "{\"binaries\":{\"linux/amd64\":\"https://...\"}}"
    }
  }],
  "deposit": "10000000upc",
  "title": "Upgrade to v1.0.0",
  "summary": "This upgrade includes new features and bug fixes."
}
```

**Fields:**
- `name`: Must match upgrade handler in code
- `height`: Block height when upgrade triggers
- `info`: JSON with binary URLs and checksums

### Step 3: Submit Proposal

```bash
pchaind tx gov submit-proposal upgrade-proposal.json \
  --from <your-key> \
  --chain-id push_42101-1 \
  --gas auto \
  --gas-adjustment 1.5 \
  --gas-prices 1000000000upc \
  --yes
```

### Step 4: Vote

```bash
pchaind query gov proposals
pchaind tx gov vote <proposal-id> yes \
  --from <your-key> \
  --chain-id push_42101-1 \
  --gas auto \
  --gas-prices 1000000000upc \
  --yes
```

---

## Manual Upgrade (Without Governance)

### Option 1: Pre-place Binary

```bash
mkdir -p ~/.pchain/cosmovisor/upgrades/<upgrade-name>/bin
cp /path/to/new/pchaind ~/.pchain/cosmovisor/upgrades/<upgrade-name>/bin/
```

### Option 2: Create upgrade-info.json

```bash
cat > ~/.pchain/data/upgrade-info.json << EOF
{
  "name": "<upgrade-name>",
  "height": <current-height>,
  "info": "{\"binaries\":{...}}"
}
EOF
```

Then restart Cosmovisor.

---

## Troubleshooting

### Binary Not Found After Upgrade

```bash
ls -la ~/.pchain/cosmovisor/upgrades/<upgrade-name>/bin/
# Must contain: pchaind
```

### Checksum Mismatch

Re-generate checksums:
```bash
./cosmovisor-upgrade-info.sh <version> <release-url>
```

### Upgrade Not Triggering

```bash
pchaind query gov proposal <id>      # Check proposal passed
pchaind query upgrade plan           # Check upgrade scheduled
pchaind status | jq .sync_info.latest_block_height
```

### Rollback an Upgrade

```bash
./stop_cosmovisor.sh
cd ~/.pchain/cosmovisor
rm current
ln -s genesis current
rm ~/.pchain/data/upgrade-info.json
./start_cosmovisor.sh
```

---

## Quick Reference

| Variable | Value |
|----------|-------|
| `DAEMON_NAME` | `pchaind` |
| `DAEMON_HOME` | `~/.pchain` |
| `DAEMON_ALLOW_DOWNLOAD_BINARIES` | `true` |
| `DAEMON_RESTART_AFTER_UPGRADE` | `true` |
| `UNSAFE_SKIP_BACKUP` | `false` |

### Commands

```bash
# Start/Stop
./start_cosmovisor.sh
./stop_cosmovisor.sh

# Check version
cosmovisor run version

# Check upgrade status
pchaind query upgrade plan
pchaind query upgrade applied <upgrade-name>
```
