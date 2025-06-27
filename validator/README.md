# Push Chain Validator 🚀

Run a Push Chain validator node in minutes with our simple one-line installer.

## 🎯 Quick Start Guide

### Prerequisites
- Docker installed ([Get Docker](https://docs.docker.com/get-docker/))
- That's it!

### Step 1: Install (30 seconds)
```bash
curl -sSL https://raw.githubusercontent.com/pushchain/push-chain-node/feature/validator-node-setup/validator/install.sh | bash
```

### Step 2: Start Your Node
```bash
cd push-validator
./push-validator start
```
Your node will start syncing with the network. This is normal and takes 1-2 hours.

### Step 3: Check Status
```bash
./push-validator status
```
Look for:
- ✅ **Catching Up: false** = Fully synced
- ⏳ **Catching Up: true** = Still syncing (this is okay for setup)

### Step 4: Become a Validator
```bash
./push-validator setup
```

The wizard will guide you through:
1. **Creating a wallet** (save your seed phrase!)
2. **Getting test tokens** from https://faucet.push.org
3. **Registering as validator** (automatic)

### Step 5: Verify You're a Validator
After registration completes:
- ✅ You'll see your validator in the list with status "BONDED"
- ✅ Your validator name will be highlighted
- ✅ Check anytime with: `./push-validator status`

**That's it! You're now running a Push Chain validator! 🎉**

---

## 💡 Common Questions

**"How long does it take?"**
- Installation: 30 seconds
- Becoming a validator: 2-3 minutes
- Full sync: 1-2 hours (but you can register while syncing)

**"How much PUSH do I need?"**
- Minimum: 1.3 PUSH (1 for staking + 0.3 for fees)
- The faucet gives you 2 PUSH

**"Is my validator working?"**
- Run `./push-validator status` to check
- Your voting power should be > 0
- You should see your validator in the active list

---

## 📚 Additional Commands & Features

<details>
<summary><b>🔧 All Commands</b></summary>

```bash
./push-validator help
```

| Command | Description |
|---------|-------------|
| `start` | Start your validator node |
| `stop` | Stop your validator node |
| `restart` | Restart your validator node |
| `status` | Show sync status and validator info |
| `setup` | Interactive validator registration wizard |
| `balance` | Check wallet balance |
| `logs` | View live logs |
| `monitor` | Real-time monitoring dashboard |
| `backup` | Backup your validator keys |
| `test` | Run health checks |

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
./push-validator logs          # Check for errors
./push-validator test          # Run diagnostics
docker ps                      # Ensure container is running
```

**Balance showing 0?**
- Node might be syncing - balance queries work better after sync
- Try: `./push-validator balance` (uses remote node)
- Or wait for `Catching Up: false` in status

**Already registered validator?**
- The setup wizard will detect this and show your validator info
- No need to register again

**Want to start fresh?**
```bash
./push-validator reset                    # Reset chain data only
docker volume rm validator_validator-data # Complete reset (removes wallets too)
```

**Common issues:**
- "AppHash mismatch" = Normal during sync, ignore it
- "Validator not in list" = Wait 1-2 minutes after registration
- "Port already in use" = Another service using ports, check `docker-compose.yml`

</details>

<details>
<summary><b>🔐 Security & Backup</b></summary>

**Critical: Always backup your keys!**

```bash
# Backup validator keys
./push-validator backup

# Keys are saved to ./backup/ directory
```

**Security tips:**
- Never share your seed phrase
- Backup keys before going to mainnet
- Use a firewall in production
- Monitor your validator uptime

**Import existing validator:**
```bash
./push-validator setup
# Choose option 2: Import wallet
```

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
<summary><b>🔍 Monitoring & Maintenance</b></summary>

**Monitor your validator:**
```bash
./push-validator monitor       # Live dashboard
./push-validator logs -f       # Follow logs
```

**Key metrics to watch:**
- Block height (should increase)
- Voting power (should be > 0)
- Missed blocks (should be low)
- Peer connections (should be > 0)

**Maintenance tasks:**
- Regular backups: `./push-validator backup`
- Update software: `./push-validator update`
- Check disk space: `df -h`
- Monitor logs for errors

</details>

<details>
<summary><b>🆘 Get Help</b></summary>

- 📖 Docs: Coming soon
- 💬 Discord: https://discord.gg/pushprotocol
- 🐛 Issues: https://github.com/push-protocol/push-chain/issues
- 📧 Email: support@push.org

**Before asking for help:**
1. Run `./push-validator test`
2. Check `./push-validator logs`
3. Verify Docker is running
4. Check you have enough disk space

</details>

---

**Remember:** The `setup` wizard handles everything automatically. Just follow the prompts! 🚀

Made with ❤️ by Push Protocol