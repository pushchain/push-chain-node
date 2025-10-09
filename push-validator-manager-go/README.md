# Push Validator Manager (Go)

**Fast validator setup with automatic recovery for Push Chain**

## 🚀 Quick Start (2-3 minutes)

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
Wait for: `✅ Catching Up: false` (takes ~2-3 minutes with state sync)

### Step 3: Register Validator
```bash
push-validator-manager register-validator
```
**Requirements:** 2+ PC tokens from [faucet](https://faucet.push.org)

**Done! Your validator is running with automatic recovery enabled! 🎉**

## 📖 Commands

### Core
```bash
push-validator-manager start                # Start with state sync (2-3 min)
push-validator-manager stop                 # Stop node
push-validator-manager status               # Check sync & validator status
push-validator-manager register-validator   # Register as validator
push-validator-manager logs                 # View logs
```

### Monitoring
```bash
push-validator-manager sync            # Monitor sync progress
push-validator-manager peers           # Show peer connections
push-validator-manager doctor          # Run health diagnostics
```

### Management
```bash
push-validator-manager restart         # Restart node
push-validator-manager validators      # List validators
push-validator-manager balance         # Check balance
push-validator-manager reset           # Reset data
push-validator-manager backup          # Backup node
push-validator-manager update          # Update to latest version
```

## ⚡ Features

- **State Sync**: 2-3 minute setup (no full blockchain download)
- **Interactive Logs**: Real-time log viewer with search and filtering
- **Smart Detection**: Monitors for sync stalls and network issues
- **Reliable Snapshots**: Uses trusted RPC nodes for recovery
- **Multiple Outputs**: JSON, YAML, or text format support

## 📊 Network

- **Chain**: `push_42101-1` (Testnet)
- **Min Stake**: 2 PC
- **Faucet**: https://faucet.push.org
- **Explorer**: https://donut.push.network


### File Locations
- **Manager**: `~/.local/bin/push-validator-manager`
- **Binary**: `~/.local/bin/pchaind`
- **Config**: `~/.pchain/config/`
- **Data**: `~/.pchain/data/`
- **Logs**: `~/.pchain/logs/pchaind.log`
- **Backups**: `~/push-node-backups/`
