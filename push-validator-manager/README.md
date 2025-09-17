# Push Validator Manager

**Fast validator setup with automatic recovery for Push Chain**

## 🚀 Quick Start (5-10 minutes)

### Step 1: Install & Start
```bash
curl -fsSL https://get.push.network/node/install.sh | bash
```
Automatically installs and starts your validator using state sync (no full sync needed).

> **Note:** Restart terminal or run `source ~/.bashrc` to use `push-validator-manager` from anywhere.

### Step 2: Verify Sync
```bash
push-validator-manager status
```
Wait for: `✅ Catching Up: false` (takes ~5-10 minutes with state sync)

### Step 3: Register Validator
```bash
push-validator-manager register-validator
```
**Requirements:** 2+ PC tokens from [faucet](https://faucet.push.org)

**Done! Your validator is running with automatic recovery enabled! 🎉**

## 📖 Commands

### Core
```bash
push-validator-manager start                # Start with state sync (5-10 min)
push-validator-manager stop                 # Stop node
push-validator-manager status               # Check sync & validator status
push-validator-manager register-validator   # Register as validator
push-validator-manager logs                 # View logs
```

### Recovery & Monitoring
```bash
push-validator-manager recover         # Manual snapshot recovery
push-validator-manager recovery-status # Check auto-recovery status
push-validator-manager sync            # Monitor sync progress
```

### Management
```bash
push-validator-manager restart    # Restart node
push-validator-manager validators # List validators
push-validator-manager balance    # Check balance
push-validator-manager reset      # Reset data
push-validator-manager backup     # Backup node
```

## ⚡ Features

- **State Sync**: 5-10 minute setup (no full blockchain download)
- **Auto Recovery**: Automatic snapshot recovery on sync failures
- **Smart Detection**: Monitors for sync stalls (>120s)
- **Reliable Snapshots**: Uses trusted RPC nodes for recovery

## 📊 Network

- **Chain**: `push_42101-1` (Testnet)
- **Min Stake**: 2 PC
- **Faucet**: https://faucet.push.org
- **Explorer**: https://donut.push.network


## 🔧 Advanced Setup (Optional)

### Setup NGINX with SSL
```bash
push-validator-manager setup-nginx yourdomain.com
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
push-validator-manager setup-logs
```
Configures daily rotation with 14-day retention and compression.

### File Locations
- **Binary**: `./build/pchaind`
- **Config**: `~/.pchain/config/`
- **Data**: `~/.pchain/data/`
- **Logs**: `~/.pchain/logs/pchaind.log`
- **Backups**: `~/push-node-backups/`

