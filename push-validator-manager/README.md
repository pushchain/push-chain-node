# Push Validator Manager

**Fast validator setup for Push Chain**

## 🚀 Quick Start (1-2 minutes)

### Step 1: Install & Start
```bash
curl -fsSL https://get.push.network/node/install.sh | bash
```
Automatically installs and starts your validator using state sync (no full sync needed).

> **Note:** Restart terminal or run `source ~/.bashrc` to use `push-validator` from anywhere.

### Step 2: Verify Sync
```bash
push-validator status
```
Wait for: `✅ Catching Up: false` (takes ~1-2 minutes with state sync)

### Step 3: Register Validator
```bash
push-validator register-validator
```
**Requirements:** 2+ PC tokens from [faucet](https://faucet.push.org)

**Done! Your validator is running with automatic recovery enabled! 🎉**

## 📊 Dashboard

Monitor your validator in real-time with an interactive dashboard:

```bash
push-validator dashboard
```

**Features:**
- **Node Status** - Process state, RPC connectivity, resource usage (CPU, memory, disk)
- **Chain Sync** - Real-time block height, sync progress with ETA, network latency
- **Validator Metrics** - Bonding status, voting power, commission rate, accumulated rewards
- **Network Overview** - Connected peers, chain ID, active validators list
- **Live Logs** - Stream node activity with search and filtering
- **Auto-Refresh** - Updates every 2 seconds for real-time monitoring

The dashboard provides everything you need to monitor validator health and performance at a glance.

## 📖 Commands

### Core
```bash
push-validator start                # Start with state sync (2-3 min)
push-validator stop                 # Stop node
push-validator status               # Check sync & validator status
push-validator dashboard            # Live interactive monitoring dashboard
push-validator register-validator   # Register as validator
push-validator logs                 # View logs
```

### Validator Operations
```bash
push-validator increase-stake       # Increase validator stake and voting power
push-validator unjail               # Restore jailed validator to active status
push-validator withdraw-rewards     # Withdraw validator rewards and commission
```

### Monitoring
```bash
push-validator sync            # Monitor sync progress
push-validator peers           # Show peer connections (from local RPC)
push-validator doctor          # Run diagnostic checks on validator setup
```

### Management
```bash
push-validator restart         # Restart node
push-validator validators      # List validators (supports --output json)
push-validator balance         # Check balance (defaults to validator key)
push-validator reset           # Reset chain data (keeps address book)
push-validator full-reset      # ⚠️ Complete reset (deletes ALL keys and data)
push-validator backup          # Backup config and validator state
```

## ⚡ Features

- **State Sync**: 1-2 minute setup (no full blockchain download)
- **Interactive Logs**: Real-time log viewer with search and filtering
- **Smart Detection**: Monitors for sync stalls and network issues
- **Reliable Snapshots**: Uses trusted RPC nodes for recovery
- **Multiple Outputs**: JSON, YAML, or text format support

## 📊 Network

- **Chain**: `push_42101-1` (Testnet)
- **Min Stake**: 2 PC
- **Faucet**: https://faucet.push.org
- **Explorer**: https://donut.push.network


## 🔧 Advanced Setup (Optional)

### Setup NGINX with SSL
```bash
bash scripts/setup-nginx.sh yourdomain.com
```
**Creates:**
- `https://yourdomain.com` - Cosmos RPC endpoint
- `https://evm.yourdomain.com` - EVM RPC endpoint
- Automatic SSL certificates via Let's Encrypt
- Rate limiting and security headers

**Requirements:**
- Domain pointing to your server IP
- Ports 80/443 open
- Ubuntu/Debian system

### Log Rotation
```bash
bash scripts/setup-log-rotation.sh
```
Configures daily rotation with 14-day retention and compression.

### File Locations
- **Manager**: `~/.local/bin/push-validator`
- **Binary**: `~/.local/bin/pchaind`
- **Config**: `~/.pchain/config/`
- **Data**: `~/.pchain/data/`
- **Logs**: `~/.pchain/logs/pchaind.log`
- **Backups**: `~/push-node-backups/`
