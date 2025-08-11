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
