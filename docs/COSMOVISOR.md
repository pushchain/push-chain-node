# Cosmovisor Setup Guide

Cosmovisor is a process manager for Cosmos SDK chains that handles binary upgrades automatically. This guide covers how to set up Cosmovisor for Push Chain and how to perform upgrades.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Installing Cosmovisor](#installing-cosmovisor)
- [Directory Structure](#directory-structure)
- [Starting a Chain with Cosmovisor](#starting-a-chain-with-cosmovisor)
- [Creating an Upgrade Proposal](#creating-an-upgrade-proposal)
- [Manual Upgrade (Without Governance)](#manual-upgrade-without-governance)
- [Troubleshooting](#troubleshooting)

---

## Prerequisites

- Go 1.22+ installed
- Push Chain binary (`pchaind`) built or downloaded
- Chain initialized with `pchaind init`

## Installing Cosmovisor

```bash
go install cosmossdk.io/tools/cosmovisor/cmd/cosmovisor@latest
```

Verify installation:
```bash
cosmovisor version
```

## Directory Structure

Cosmovisor expects a specific directory structure:

```
$DAEMON_HOME/
├── config/
├── data/
└── cosmovisor/
    ├── genesis/
    │   └── bin/
    │       └── pchaind        # Initial binary
    └── upgrades/
        └── <upgrade-name>/
            └── bin/
                └── pchaind    # Upgraded binary (auto-downloaded or manual)
```

## Starting a Chain with Cosmovisor

### 1. Set Environment Variables

```bash
export DAEMON_NAME=pchaind
export DAEMON_HOME=$HOME/.pchain
export DAEMON_ALLOW_DOWNLOAD_BINARIES=true   # Auto-download upgrades
export DAEMON_RESTART_AFTER_UPGRADE=true     # Auto-restart after upgrade
export UNSAFE_SKIP_BACKUP=true               # Skip backup (set false for production)
```

Add these to your shell profile (`~/.bashrc` or `~/.zshrc`) for persistence.

### 2. Create Directory Structure

```bash
mkdir -p $DAEMON_HOME/cosmovisor/genesis/bin
mkdir -p $DAEMON_HOME/cosmovisor/upgrades
```

### 3. Copy Genesis Binary

```bash
# If you built from source:
cp $(which pchaind) $DAEMON_HOME/cosmovisor/genesis/bin/

# Or download from release:
VERSION=v0.0.19
curl -L "https://github.com/pushchain/push-chain-node/releases/download/${VERSION}/push-chain_${VERSION#v}_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/').tar.gz" | tar -xz -C $DAEMON_HOME/cosmovisor/genesis/
```

### 4. Initialize Chain (if not already done)

```bash
$DAEMON_HOME/cosmovisor/genesis/bin/pchaind init <moniker> --chain-id <chain-id> --home $DAEMON_HOME
```

### 5. Start with Cosmovisor

```bash
cosmovisor run start --home $DAEMON_HOME
```

### 6. Run as a Service (Recommended for Production)

Create `/etc/systemd/system/pchaind.service`:

```ini
[Unit]
Description=Push Chain Node
After=network.target

[Service]
Type=simple
User=<your-user>
Environment="DAEMON_NAME=pchaind"
Environment="DAEMON_HOME=/home/<your-user>/.pchain"
Environment="DAEMON_ALLOW_DOWNLOAD_BINARIES=true"
Environment="DAEMON_RESTART_AFTER_UPGRADE=true"
Environment="UNSAFE_SKIP_BACKUP=false"
ExecStart=/home/<your-user>/go/bin/cosmovisor run start --home /home/<your-user>/.pchain
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

Enable and start:
```bash
sudo systemctl daemon-reload
sudo systemctl enable pchaind
sudo systemctl start pchaind
```

---

## Creating an Upgrade Proposal

### Step 1: Generate Upgrade Info JSON

Use the helper script to generate the upgrade info with checksums:

```bash
./scripts/cosmovisor-upgrade-info.sh v1.0.0 https://github.com/pushchain/push-chain-node/releases/download/v1.0.0
```

This outputs JSON like:
```json
{
  "binaries": {
    "linux/amd64": "https://github.com/.../push-chain_1.0.0_linux_amd64.tar.gz?checksum=sha256:abc123...",
    "linux/arm64": "https://github.com/.../push-chain_1.0.0_linux_arm64.tar.gz?checksum=sha256:def456...",
    "darwin/arm64": "https://github.com/.../push-chain_1.0.0_darwin_arm64.tar.gz?checksum=sha256:789ghi..."
  }
}
```

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
      "info": "{\"binaries\":{\"linux/amd64\":\"https://github.com/pushchain/push-chain-node/releases/download/v1.0.0/push-chain_1.0.0_linux_amd64.tar.gz?checksum=sha256:abc123\",\"linux/arm64\":\"https://github.com/pushchain/push-chain-node/releases/download/v1.0.0/push-chain_1.0.0_linux_arm64.tar.gz?checksum=sha256:def456\",\"darwin/arm64\":\"https://github.com/pushchain/push-chain-node/releases/download/v1.0.0/push-chain_1.0.0_darwin_arm64.tar.gz?checksum=sha256:789ghi\"}}"
    }
  }],
  "deposit": "10000000upc",
  "title": "Upgrade to v1.0.0",
  "summary": "This upgrade includes new features and bug fixes. See changelog for details."
}
```

**Important fields:**
- `name`: Must match the upgrade handler name in the code
- `height`: Block height when upgrade will trigger
- `info`: JSON string with binary download URLs and checksums
- `deposit`: Minimum deposit to submit proposal

### Step 3: Submit the Proposal

```bash
pchaind tx gov submit-proposal upgrade-proposal.json \
  --from <your-key> \
  --chain-id <chain-id> \
  --gas auto \
  --gas-adjustment 1.5 \
  --gas-prices 1000000000upc \
  --yes
```

### Step 4: Vote on the Proposal

```bash
# Get proposal ID from the submission output or query:
pchaind query gov proposals

# Vote yes:
pchaind tx gov vote <proposal-id> yes \
  --from <your-key> \
  --chain-id <chain-id> \
  --gas auto \
  --gas-prices 1000000000upc \
  --yes
```

### Step 5: Monitor the Upgrade

Once the proposal passes and the chain reaches the upgrade height:

1. Cosmovisor detects the upgrade
2. Downloads the new binary (if `DAEMON_ALLOW_DOWNLOAD_BINARIES=true`)
3. Stops the current binary
4. Switches to the new binary
5. Restarts the chain

Watch the logs:
```bash
journalctl -u pchaind -f
```

---

## Manual Upgrade (Without Governance)

For testnets or when you need to manually trigger an upgrade:

### Option 1: Pre-place the Binary

```bash
# Create upgrade directory
mkdir -p $DAEMON_HOME/cosmovisor/upgrades/<upgrade-name>/bin

# Copy new binary
cp /path/to/new/pchaind $DAEMON_HOME/cosmovisor/upgrades/<upgrade-name>/bin/

# The upgrade will trigger when the chain reaches the upgrade height
```

### Option 2: Create upgrade-info.json Manually

```bash
# Stop the chain at the desired height, then create:
cat > $DAEMON_HOME/data/upgrade-info.json << EOF
{
  "name": "<upgrade-name>",
  "height": <current-height>,
  "info": "{\"binaries\":{...}}"
}
EOF

# Restart cosmovisor - it will download and apply the upgrade
```

---

## Troubleshooting

### Binary Not Found After Upgrade

Check the upgrade directory structure:
```bash
ls -la $DAEMON_HOME/cosmovisor/upgrades/<upgrade-name>/bin/
```

The binary must be named exactly `pchaind`.

### macOS: Library Not Loaded (libwasmvm.dylib)

Ensure `libwasmvm.dylib` is in the same directory as the binary:
```bash
ls $DAEMON_HOME/cosmovisor/upgrades/<upgrade-name>/bin/
# Should show both: pchaind and libwasmvm.dylib
```

Or install system-wide:
```bash
sudo cp libwasmvm.dylib /usr/local/lib/
```

### Checksum Mismatch

If auto-download fails with checksum error:
1. Verify the checksum in the proposal matches the actual file
2. Re-generate using `./scripts/cosmovisor-upgrade-info.sh`

### Upgrade Not Triggering

1. Check if proposal passed: `pchaind query gov proposal <id>`
2. Verify upgrade is scheduled: `pchaind query upgrade plan`
3. Check current height: `pchaind status | jq .sync_info.latest_block_height`

### Rollback an Upgrade

If an upgrade fails:
```bash
# Stop cosmovisor
sudo systemctl stop pchaind

# Point current symlink back to genesis (or previous working version)
cd $DAEMON_HOME/cosmovisor
rm current
ln -s genesis current

# Remove the failed upgrade info
rm $DAEMON_HOME/data/upgrade-info.json

# Restart
sudo systemctl start pchaind
```

---

## Quick Reference

| Environment Variable | Description | Recommended Value |
|---------------------|-------------|-------------------|
| `DAEMON_NAME` | Binary name | `pchaind` |
| `DAEMON_HOME` | Chain data directory | `$HOME/.pchain` |
| `DAEMON_ALLOW_DOWNLOAD_BINARIES` | Auto-download upgrades | `true` |
| `DAEMON_RESTART_AFTER_UPGRADE` | Auto-restart after upgrade | `true` |
| `UNSAFE_SKIP_BACKUP` | Skip data backup | `false` (production) |

## Useful Commands

```bash
# Check current binary version
cosmovisor run version

# Check scheduled upgrades
pchaind query upgrade plan

# Check applied upgrades
pchaind query upgrade applied <upgrade-name>

# View cosmovisor logs
journalctl -u pchaind -f --no-hostname
```
