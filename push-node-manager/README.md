# Push Node Manager

**Native Push Chain validator management - No Docker required**

## 🚀 Quick Start

### Step 1: Setup & Start (30 seconds)
```bash
# Install dependencies & start node
./push-node-manager start
```

### Step 2: Check Sync Status
```bash
./push-node-manager status
```
Wait for: `✅ Catching Up: false` (fully synced)

### Step 3: Become a Validator
```bash
./push-node-manager register-validator
```

**That's it! You're now running a Push Chain validator! 🎉**

## 📖 Commands

### Essential Commands
```bash
./push-node-manager start           # Setup + start node  
./push-node-manager stop            # Stop node
./push-node-manager status          # Show sync status & validator info
./push-node-manager register-validator # Become a validator
./push-node-manager logs            # View live logs
```

### Additional Commands
```bash
./push-node-manager restart         # Restart node
./push-node-manager sync            # Real-time sync monitor
./push-node-manager validators      # List all validators
./push-node-manager balance         # Check wallet balance
./push-node-manager reset           # Reset blockchain data
./push-node-manager help            # Show all commands
```

### Public Setup Commands (Optional)
```bash
./push-node-manager setup-nginx <domain>  # Setup NGINX + SSL for public RPC
./push-node-manager setup-logs            # Configure log rotation
./push-node-manager backup                # Create node backup
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

## 🚨 Troubleshooting

### Node Won't Start
```bash
./push-node-manager logs    # Check error messages
```

### Binary Not Found
```bash
❌ Native binary not found
# Solution: The setup script will build it automatically
./scripts/setup-dependencies.sh
```

### Sync Issues
```bash
./push-node-manager status  # Check sync progress
./push-node-manager reset   # If corrupted, reset data
```

### Validator Registration Fails
**Requirements:**
- Node must be synced (catching_up: false)
- Need 2+ PC tokens from faucet
- Account must be funded first

## 🌍 Public Setup (Optional)

### Making Your Node Publicly Accessible

By default, your validator runs locally. These optional commands help set up public HTTPS endpoints:

#### Setup NGINX with SSL
```bash
./push-node-manager setup-nginx yourdomain.com
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
./push-node-manager setup-logs
```
**Configures:**
- Daily log rotation
- 14-day retention
- Automatic compression
- System logrotate integration

#### Create Backups  
```bash
./push-node-manager backup
```
**Creates:**
- Timestamped backup in `~/push-node-backups/`
- Includes all config, keys, and blockchain data  
- Compressed archive with integrity verification

## 🔐 Security

- **Keys stored locally** in `~/.pchain/keyring-test/`
- **No root access required**
- **No external dependencies** beyond system packages
- **Network connections** only to official Push Chain RPCs

## 💡 FAQ

**"How long does sync take?"**
- Usually 30-60 minutes for initial sync

**"Can I register while syncing?"**
- Yes, if within 120 blocks of network height

**"How much PC do I need?"**
- Minimum 2 PC for validator registration

**"Is my validator working?"**
- Check with `./push-node-manager status`
- Look for "BONDED" status and voting power > 0

## 📝 Support

1. Check logs: `./push-node-manager logs`
2. Verify status: `./push-node-manager status`  
3. Review troubleshooting section above
4. Check [Push Chain Documentation](https://docs.push.org)

---

**Push Node Manager v4.0** - Built for simplicity and native performance.
