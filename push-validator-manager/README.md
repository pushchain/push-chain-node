# Push Validator Manager

**Push Chain validator management**

## 🚀 Quick Start

### Step 1: Install & Start
```bash
curl -fsSL https://get.push.network/node/install.sh | bash
```
This installs and starts your validator automatically.

> **Note:** After installation, restart your terminal or run `source ~/.bashrc` (or `~/.zshrc`) to use the `push-validator-manager` command from anywhere.

### Step 2: Check Sync Status
```bash
push-validator-manager status
```
Wait for: `✅ Catching Up: false` (fully synced)

### Step 3: Become a Validator
```bash
push-validator-manager register-validator
```

**Requirements:**
- Node must be synced (catching_up: false)
- Need 2+ PC tokens from [faucet](https://faucet.push.org)
- Account must be funded first

**That's it! You're now running a Push Chain validator! 🎉**

## 📖 Commands

### Essential Commands
```bash
push-validator-manager start                # Setup + start node  
push-validator-manager stop                 # Stop node
push-validator-manager status               # Show sync status & validator info
push-validator-manager register-validator   # Become a validator
push-validator-manager logs                 # View live logs
```

### Additional Commands
```bash
push-validator-manager restart         # Restart node
push-validator-manager sync            # Real-time sync monitor
push-validator-manager validators      # List all validators
push-validator-manager balance         # Check wallet balance
push-validator-manager reset           # Reset blockchain data
push-validator-manager help            # Show all commands
```

### Public Setup Commands (Optional)
```bash
push-validator-manager setup-nginx <domain>  # Setup NGINX + SSL for public RPC
push-validator-manager setup-logs            # Configure log rotation
push-validator-manager backup                # Create node backup
```

## 📊 Network Info

- **Chain ID**: `push_42101-1`
- **Network**: Push Chain Testnet
- **Min Stake**: 2 PC
- **Faucet**: https://faucet.push.org
- **Explorer**: https://donut.push.network

## 🔧 File Locations

- **Binary**: `./build/pchaind`
- **Config**: `~/.pchain/config/`
- **Data**: `~/.pchain/data/`
- **Keys**: `~/.pchain/keyring-test/`
- **Logs**: `~/.pchain/logs/pchaind.log`


## 🌍 Public Setup (Optional)

### Making Your Node Publicly Accessible

By default, your validator runs locally. These optional commands help set up public HTTPS endpoints:

#### Setup NGINX with SSL
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

#### Setup Log Rotation
```bash
push-validator-manager setup-logs
```
**Configures:**
- Daily log rotation
- 14-day retention
- Automatic compression
- System logrotate integration

#### Create Backups  
```bash
push-validator-manager backup
```
- Timestamped backup in `~/push-node-backups/`
- Includes all config, keys, and blockchain data  
- Compressed archive with integrity verification

