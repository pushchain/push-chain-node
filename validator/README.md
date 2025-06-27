# Push Chain Validator - 5 Minute Setup 🚀

Run your own Push Chain validator in minutes, not hours!

## Prerequisites

- **Docker** installed ([Get Docker](https://docs.docker.com/get-docker/))
- **4GB RAM** minimum
- **100GB SSD** storage

## Quick Start

### 1. Install (30 seconds)

```bash
curl -sSL https://get.push.org/validator | bash
cd push-validator
```

### 2. Start Validator (2 minutes)

```bash
./push-validator start
```

The installer will ask you to enter your validator name.

That's it! Your validator will automatically:
✅ Download the correct genesis file
✅ Connect to the network
✅ Start syncing blocks

### 3. Check Status

```bash
./push-validator status
```

Wait for `Catching Up: false` - this means you're fully synced!

## Becoming an Active Validator

Two flows based on your starting point:

### Flow 1: Create Wallet → Fund → Register 🆕

For new validators starting from scratch:

```bash
# Start the validator first
./push-validator start

# Then run setup wizard
./push-validator setup
```

This interactive wizard will:
1. **Create new wallet** - Generate secure mnemonic (SAVE IT!)
2. **Show EVM address** - For faucet funding
3. **Guide to faucet** - https://faucet.push.org (need 1.3 PUSH minimum)
4. **Monitor funding** - Auto-detect incoming tokens
5. **Register validator** - With pre-flight checks
6. **Verify status** - Confirm registration success

### Flow 2: Import Funded Wallet → Register 📥

For validators with existing funded wallets:

**Interactive Method:**
```bash
# Ensure validator is running
./push-validator start

# Run setup and choose import option
./push-validator setup
# Choose option 2 to import wallet with mnemonic
# Skip faucet, proceed directly to registration
```

**Automated Method (CI/CD):**
```bash
# Ensure validator is running
./push-validator start

# Import wallet
docker compose exec -e MNEMONIC="your twelve word mnemonic phrase here" validator /scripts/import-wallet.sh

# Auto-register
./push-validator auto-register
```

**Prerequisites for Flow 2**:
- Existing wallet with 1.3+ PUSH
- Mnemonic phrase ready
- Node should be synced

### Important Notes

**Funding Requirements**: 1.3 PUSH minimum (1 PUSH stake + 0.3 PUSH fees/buffer)

**Backup Keys**: Always backup after setup
```bash
./push-validator backup
```


## Access Points

Once running, your validator exposes:
- **RPC**: `http://localhost:26657`
- **API**: `http://localhost:1317`
- **gRPC**: `localhost:9090`
- **EVM RPC**: `http://localhost:8545`
- **EVM WebSocket**: `ws://localhost:8546`
- **Metrics**: `http://localhost:6060`

## Health Monitoring

### Comprehensive Test Suite

Run the included test suite to verify your validator is working correctly:

```bash
./push-validator test
```

## Troubleshooting

**Not syncing?**
```bash
./push-validator logs
```

**Health check failed?**
```bash
./push-validator test    # Run comprehensive diagnostics
./push-validator status  # Check basic status
```

**Need help?**
- Docs: https://docs.push.org

## FAQ

**How long does sync take?**
- Testnet: 1-2 hours

**How much PUSH do I need?**
- Testnet: Minimum 1.3 PUSH (1 PUSH stake + 0.3 PUSH for fees/buffer)
- Mainnet: 10,000 PUSH + fees

**Is my validator working?**
- Run health checks: `./push-validator test`
- Check explorer: https://donut.push.network
- Monitor status: `./push-validator status`

**Need the EVM RPC endpoint?**
- Testnet: https://evm.rpc-testnet-donut-node1.push.org

**Want to make your validator public?**
- See [PUBLIC_VALIDATOR_SETUP.md](PUBLIC_VALIDATOR_SETUP.md) for nginx/HTTPS setup (optional)

## Command Reference

### Basic Operations
| Command | Description |
|---------|-------------|
| `./push-validator start` | Start validator |
| `./push-validator stop` | Stop validator |
| `./push-validator restart` | Restart validator |
| `./push-validator status` | Check sync status |
| `./push-validator logs` | View logs |
| `./push-validator monitor` | Live monitoring view |

### Validator Management
| Command | Description |
|---------|-------------|
| `./push-validator setup` | Interactive wallet setup & registration |
| `./push-validator auto-register` | Automatic registration (existing wallet) |
| `./push-validator balance` | Check wallet balance |
| `./push-validator test` | Run health checks |
| `./push-validator backup` | Backup validator keys |

### Advanced Commands
| Command | Description |
|---------|-------------|
| `./push-validator shell` | Open container shell |
| `./push-validator keys list` | List all keys |
| `./push-validator keys add [name]` | Create new key |
| `./push-validator reset` | Reset blockchain data |
| `./push-validator update` | Update validator software |

---

Made with ❤️ by Push Protocol