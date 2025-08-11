# Push Node Manager

**Completely Docker-free Push Chain validator management**

## 🚀 Quick Start

### First Time Setup (One Command!)
```bash
# Auto-installs dependencies, builds binary, and starts node
./push-node-manager start

# Check sync status
./push-node-manager status

# Register as validator (when synced)
./push-node-manager register-validator
```

## ✨ What's New in v3.0.0

- **🚫 Docker-Free**: Complete removal of Docker dependency
- **⚡ Native-Only**: Uses official Push Chain binaries directly  
- **🔧 One-Command Setup**: Auto-installs dependencies, builds binary, and starts node
- **🎯 Simplified**: Clean, streamlined codebase following testnet approach
- **📱 Background Daemon**: Node runs as background process with proper management
- **📊 Real-time Sync Monitor**: Live dashboard for sync progress tracking

## 📋 Features

### Core Functions
- **Node Management**: Start, stop, restart, status monitoring
- **Validator Operations**: Simplified registration wizard
- **Wallet Management**: Balance checking, faucet integration
- **Real-time Monitoring**: Live logs, sync status, validator info
- **Automated Setup**: Dependencies, building, configuration

### Removed Complexity
- ❌ Docker containers and compose files
- ❌ Complex network configuration files
- ❌ Over-engineered error handling
- ❌ Multiple execution modes
- ❌ Complex update mechanisms

## 🛠️ System Requirements

### Dependencies (Auto-Installed)
- **Go 1.19+** (for building binaries)
- **jq** (JSON processing)
- **curl** (HTTP requests)
- **dig** (DNS resolution) 
- **Python 3.10+** with tomlkit (configuration editing)

### Operating Systems
- **Linux** (Ubuntu, Debian, CentOS, etc.)
- **macOS** (Intel and Apple Silicon)

## 📖 Commands Reference

### Node Management
```bash
./push-node-manager start         # Auto-setup + Start the Push node
./push-node-manager stop          # Stop the Push node  
./push-node-manager restart       # Restart the Push node
./push-node-manager status        # Show detailed node status
./push-node-manager sync          # Real-time sync monitoring dashboard
./push-node-manager logs          # Show live logs (Ctrl+C to exit)
```

### Validator Operations
```bash
./push-node-manager register-validator  # Interactive validator setup
./push-node-manager validators          # List all active validators
./push-node-manager balance [wallet]    # Check wallet balance
```

### Maintenance
```bash
./push-node-manager setup-deps    # Install dependencies & build binary
./push-node-manager reset         # Reset blockchain data (keeps wallets)
./push-node-manager help          # Show help information
```

## 🔧 Advanced Configuration

### Environment Variables
```bash
export MONIKER="my-validator-name"          # Custom validator name
export GENESIS_DOMAIN="your-rpc-node.com"  # Custom genesis source
```

### File Locations
- **Binary**: `./build/pchaind`
- **Config**: `~/.pchain/config/`
- **Data**: `~/.pchain/data/`
- **Keys**: `~/.pchain/keyring-test/`
- **Logs**: `~/.pchain/logs/pchaind.log`
- **PID**: `~/.pchain/pchaind.pid`

## 🔄 Migration from Docker Version

### If upgrading from previous Docker-based version:

1. **Backup your keys** (important!):
   ```bash
   mkdir -p backup-keys
   docker compose exec validator pchaind keys export validator-key > backup-keys/validator.key
   ```

2. **Stop Docker version**:
   ```bash
   docker compose down
   ```

3. **Setup native version**:
   ```bash
   ./setup-native-dependencies.sh
   ./push-node-manager start
   ```

4. **Import your keys**:
   ```bash
   ./build/pchaind keys import validator-key backup-keys/validator.key --keyring-backend test
   ```

## 🛡️ Security Notes

- **Keys are stored locally** in `~/.pchain/keyring-test/`
- **No external dependencies** beyond system packages
- **Process runs under your user account** (not root)
- **Network connections** only to official Push Chain RPCs
- **Log files** contain no sensitive information

## 🐛 Troubleshooting

### Binary Not Found
```bash
❌ Native binary not found at: ./build/pchaind
🔧 Run './setup-native-dependencies.sh' to build the binary
```
**Solution**: Run the setup script to install dependencies and build

### Node Won't Start  
**Check logs**: `./push-node-manager logs`
**Common issues**:
- Port 26657 already in use (stop other Cosmos nodes)
- Genesis file download failed (check internet connection)
- Peer connection issues (will resolve automatically)

### Node Not Syncing
**Check status**: `./push-node-manager status`
**Solutions**:
- Wait longer (initial sync can take time)
- Reset and restart: `./push-node-manager reset` then `start`
- Check peers are reachable

### Validator Registration Fails
**Requirements**:
- Node must be fully synced first
- Need 2+ PC tokens from faucet
- Account must be funded before registration

## 📊 Network Information

- **Chain ID**: `push_42101-1`
- **Network**: Push Chain Testnet
- **RPC**: http://localhost:26657 (when running)
- **Faucet**: https://faucet.push.org
- **Explorer**: https://donut.push.network
- **Min Stake**: 2 PC

## 🤝 Support

For issues with the Push Node Manager:
1. Check logs: `./push-node-manager logs`
2. Verify status: `./push-node-manager status`
3. Review this README
4. Check [Push Chain Documentation](https://docs.push.org)

## 📝 Changelog

### v3.0.0 - Native-Only Edition
- Complete Docker removal
- Native binary execution
- Automated dependency setup
- Simplified command structure
- Background daemon mode
- Clean testnet-based architecture

### v2.1.0 - Simplified & Streamlined  
- Native mode preference
- Docker fallback support
- Simplified registration wizard

### v2.0.0 - Enhanced
- Interactive features
- Public setup capabilities
- Monitoring tools

---

**Push Node Manager v3.0.0** - Built for simplicity, reliability, and native performance.


## 🎯 Quick Start Guide

### Prerequisites
- Docker installed ([Get Docker](https://docs.docker.com/get-docker/))
- That's it!

### Step 1: Install (30 seconds)
```bash
curl -sSL https://raw.githubusercontent.com/pushchain/push-chain-node/feature/validator-node-setup/push-node-manager/install.sh | bash
```

### Step 2: Start Your Node
```bash
cd push-node-manager
./push-node-manager start
```
Your node will start syncing with the network. This is normal and takes 1-2 hours.

### Step 3: Check Status
```bash
./push-node-manager status
```
Look for:
- ✅ **Catching Up: false** = Fully synced
- ⏳ **Catching Up: true** = Still syncing (this is okay for setup)
- 📊 **Sync Progress** = Shows percentage and blocks behind
- 🔍 **Node Type** = Shows if you're running as validator or full node

### Step 4: Become a Validator
```bash
./push-node-manager setup
```

The wizard will guide you through:
1. **Creating a wallet** (save your seed phrase!)
2. **Getting test tokens** from https://faucet.push.org
3. **Registering as validator** (automatic)

### Step 5: Verify You're a Validator
After registration completes:
- ✅ You'll see your validator in the list with status "BONDED"
- ✅ Your validator name will be highlighted
- ✅ Check anytime with: `./push-node-manager status`

**That's it! You're now running a Push Chain validator! 🎉**

---

## 💡 Common Questions

**"How long does it take?"**
- Installation: 30 seconds
- Becoming a validator: 2-3 minutes
- Full sync: 1-2 hours (but you can register while syncing)

**"How much PC do I need?"**
- Minimum: 2 PC for staking
- The faucet gives you test PC tokens

**"Is my validator working?"**
- Run `./push-node-manager status` to check
- Your voting power should be > 0
- You should see your validator in the active list

---

## 📚 Additional Commands & Features

<details>
<summary><b>🔧 All Commands</b></summary>

```bash
./push-node-manager help
```

| Command | Description |
|---------|-------------|
| `start` | Start your validator node |
| `stop` | Stop your validator node |
| `restart` | Restart your validator node |
| `status` | Show sync status, validator info, and sync progress with ETA |
| `setup` | Interactive wallet setup & validator registration wizard |
| `balance` | Check wallet balance and show faucet info |
| `validators` | List all active validators with FULL names and addresses |
| `logs` | View live logs (with optional filtering) |
| `monitor` | Real-time monitoring dashboard |
| `sync` | Monitor sync progress in real-time with live updates |
| `backup` | Backup validator keys to ./backup/ directory |
| `test` | Run comprehensive health checks |
| `shell` | Open shell in validator container for debugging |
| `reset-data` | Reset blockchain data (keeps wallets) - interactive options |
| `reset-all` | **DANGER:** Complete reset - deletes EVERYTHING! |
| `keys` | Key management (list, add, show, delete) |
| `update` | Update validator software to latest version |
| `auto-register [wallet]` | Automatic registration (auto-detects or specify wallet name) |
| `public-setup` | Setup HTTPS endpoints for public access (Linux only) |
| `help` | Show detailed help with examples |

</details>

<details>
<summary><b>🔄 Keeping Your Node Updated</b></summary>

**Automatic Binary Updates:**
The node manager automatically downloads the latest `pchaind` binary from GitHub releases. No manual binary management needed!

**Manual Updates (Default - Safe):**
```bash
./push-node-manager update     # Download latest binary and rebuild
./push-node-manager restart    # Apply changes
./push-node-manager status     # Verify everything works
```

**Automatic Updates (Optional):**
```bash
# Enable auto-updates in .env file
echo "AUTO_UPDATE=true" >> .env

# Now updates happen automatically when starting
./push-node-manager start      # Checks for updates first
```

**Update Process:**
- Pull latest scripts and configuration
- Download latest `pchaind` binary from GitHub releases
- Rebuild the validator image with the new binary
- Preserve all your wallets and configuration
- Skip auto-update if validator is actively validating (for safety)

**Update Notifications:**
- `./push-node-manager status` always shows if updates are available
- Provides current vs latest version information
- Shows instructions for updating

**Version Information:**
- Node Manager: v2.0.0 (now uses GitHub release binaries)
- Binary: Latest from [pushchain/push-chain-node releases](https://github.com/pushchain/push-chain-node/releases)
- Auto-detects your system architecture (amd64/arm64)

</details>

<details>
<summary><b>💾 System Requirements</b></summary>

**Minimum:**
- 2 CPU cores
- 4 GB RAM
- 20 GB disk space
- Stable internet connection

**Recommended:**
- 4 CPU cores
- 8 GB RAM
- 100 GB SSD
- 100 Mbps connection

**Network Info:**
- Chain: `push_42101-1` (Testnet)
- Min stake: 1 PUSH
- Gas: ~0.2 PUSH per transaction

</details>

<details>
<summary><b>🚨 Troubleshooting</b></summary>

**Validator not starting?**
```bash
./push-node-manager logs          # Check for errors
./push-node-manager test          # Run diagnostics
docker ps                      # Ensure container is running
```

**Balance showing 0?**
- Node might be syncing - balance queries work better after sync
- Try: `./push-node-manager balance` (uses remote node)
- Or wait for `Catching Up: false` in status

**Already registered validator?**
- The setup wizard will detect this automatically
- Offers options to: use existing validator (import wallet) or create new one
- Handles validator key conflicts intelligently

**Sync issues or corrupted data?**
```bash
./push-node-manager reset-data    # Interactive reset options
# Option 1: Quick reset (node stays running)
# Option 2: Clean reset (stops node, removes volumes)
```

**Want to start completely fresh?**
```bash
./push-node-manager reset-all     # WARNING: Deletes everything including wallets!
```

</details>

<details>
<summary><b>🔐 Security & Backup</b></summary>

**Critical: Always backup your keys!**

```bash
# Backup node keys
./push-node-manager backup

# Keys are saved to ./backup/ directory with timestamp
# Includes: node keys, validator keys, and node ID
```

**Security tips:**
- Never share your seed phrase
- Backup keys before going to mainnet
- Use a firewall in production
- Monitor your validator uptime

**Import existing validator:**
```bash
./push-node-manager setup
# If validator exists, it will prompt you
# Choose option 3: Import wallet from seed phrase
```

**Wallet management during setup:**
- Lists all existing wallets with addresses
- Option to use existing, create new, or import
- Smart detection of validator conflicts

</details>

<details>
<summary><b>🌐 Advanced Configuration</b></summary>

**Default Ports:**
- P2P: 26656
- RPC: http://localhost:26657
- API: http://localhost:1317
- gRPC: localhost:9090
- Prometheus: http://localhost:26660

**Custom Configuration:**
Edit `docker-compose.yml` for:
- Custom ports
- Resource limits
- Network settings

**Production Setup:**
- Use `PUBLIC_VALIDATOR_SETUP.md` for public endpoints
- Setup monitoring with Prometheus/Grafana
- Configure firewall rules
- Enable automated backups

</details>

<details>
<summary><b>🌐 Public Validator Setup (Optional)</b></summary>

## Making Your Validator Publicly Accessible

By default, your validator runs on localhost. If you want to make it publicly accessible with HTTPS endpoints, follow this guide.

### Architecture Overview

```
┌─────────────────────────────────┐
│         HOST MACHINE            │
│                                 │
│  ┌─────────────────────────┐   │
│  │   Nginx (Port 80/443)   │   │ ← Public Setup HERE
│  │   - SSL Certificates    │   │
│  │   - Reverse Proxy       │   │
│  └──────────┬──────────────┘   │
│             │                   │
│             ▼                   │
│  ┌─────────────────────────┐   │
│  │   Docker Container      │   │
│  │   - Push Node           │   │
│  │   - Ports:              │   │
│  │     • 26656 (P2P)       │   │
│  │     • 26657 (RPC)       │   │
│  │     • 8545 (EVM HTTP)   │   │
│  │     • 8546 (EVM WS)     │   │
│  │     • 1317 (REST)       │   │
│  │     • 9090 (gRPC)       │   │
│  └─────────────────────────┘   │
└─────────────────────────────────┘
```

### Prerequisites

- Ubuntu/Debian server with public IP
- Domain name pointing to your server
- Ports 80 and 443 open in firewall
- Validator already running (`./push-node-manager status`)

### Quick Setup

```bash
# Automated setup (Linux only)
./push-node-manager public-setup

# Or follow the manual guide:
cat PUBLIC_VALIDATOR_SETUP.md
```

### What This Sets Up

1. **HTTPS Endpoints:**
   - `https://rpc.your-domain.com` - Cosmos RPC
   - `https://evm.your-domain.com` - EVM RPC (HTTP & WebSocket)

2. **Security Features:**
   - SSL/TLS encryption with Let's Encrypt
   - Rate limiting
   - DDoS protection
   - Optional IP whitelisting

3. **High Availability:**
   - Nginx reverse proxy
   - WebSocket support
   - Connection pooling
   - Health checks

### Manual Setup Steps

1. **Install Nginx & Certbot:**
   ```bash
   sudo apt update
   sudo apt install -y nginx certbot python3-certbot-nginx
   ```

2. **Configure Nginx:**
   - See `PUBLIC_VALIDATOR_SETUP.md` for full configuration
   - Replace `your-domain.com` with your actual domain

3. **Setup SSL:**
   ```bash
   sudo certbot --nginx -d rpc.your-domain.com -d evm.your-domain.com
   ```

4. **Test Your Endpoints:**
   ```bash
   # Test Cosmos RPC
   curl https://rpc.your-domain.com/status
   
   # Test EVM RPC
   curl -X POST https://evm.your-domain.com \
     -H "Content-Type: application/json" \
     -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
   ```

### Security Best Practices

- Keep your validator signing key secure (never expose it)
- Use firewall to restrict access to essential ports only
- Enable rate limiting in nginx configuration
- Monitor access logs regularly
- Consider using a CDN for additional protection

### Notes

- This setup is **completely optional** - validators work fine on localhost
- Public endpoints allow others to use your node as an RPC provider
- Ensure you have sufficient bandwidth if making endpoints public
- Consider the security implications before exposing endpoints

For detailed instructions, see [PUBLIC_VALIDATOR_SETUP.md](PUBLIC_VALIDATOR_SETUP.md)

</details>

<details>
<summary><b>🔍 Monitoring & Maintenance</b></summary>

**Monitor your validator:**
```bash
./push-node-manager monitor       # Live dashboard
./push-node-manager logs -f       # Follow logs
```

**Key metrics to watch:**
- Block height (should increase)
- Voting power (should be > 0)
- Missed blocks (should be low)
- Peer connections (should be > 0)

**Maintenance tasks:**
- Regular backups: `./push-node-manager backup`
- Update software: `./push-node-manager update`
- Check disk space: `df -h`
- Monitor logs for errors

</details>

<details>
<summary><b>🔄 Reset Options Explained</b></summary>

**When to use each reset option:**

### `./push-node-manager reset-data`
Resets blockchain data while keeping your wallets and validator keys safe.

**Option 1: Quick Reset**
- Node stays running
- Uses `pchaind tendermint unsafe-reset-all`
- Fastest option
- Use when: Quick fix needed for sync issues

**Option 2: Clean Reset**
- Stops the node
- Removes Docker volumes and data directory
- More thorough cleanup
- Use when: AppHash errors, corrupted data, or option 1 didn't work

### `./push-node-manager reset-all`
⚠️ **DANGER**: Complete nuclear reset!
- Deletes ALL blockchain data
- Deletes ALL wallets and keys
- Removes Docker volumes and images
- You'll need to start from scratch (new wallet, new tokens, re-register)
- Use when: Testing from scratch or unrecoverable issues

**Quick decision guide:**
- Sync stuck? → Use `reset-data` (option 2)
- AppHash error? → Use `reset-data` (option 2)
- Testing fresh install? → Use `reset-all`
- Just need to clear data? → Use `reset-data` (option 1)

</details>

<details>
<summary><b>🆘 Get Help</b></summary>

- 📖 Docs: Coming soon
- 💬 Discord: Coming soon
- 🐛 Issues: Coming soon
- 📧 Email: Coming soon

**Before asking for help:**
1. Run `./push-node-manager test`
2. Check `./push-node-manager logs`
3. Verify Docker is running
4. Check you have enough disk space

</details>

---


**Remember:** The `setup` wizard handles everything automatically. Just follow the prompts! 🚀

Made with ❤️ by Push Protocol