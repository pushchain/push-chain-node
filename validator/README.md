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

### 1. Backup Your Keys (CRITICAL!)

```bash
./push-validator backup
```
⚠️ **Save the backup folder somewhere safe!**

### 2. Get Test Tokens

Get your wallet address:
```bash
./push-validator keys show validator
```

Then visit the faucet: https://faucet.push.org

### 3. Register as Validator

Once funded and synced:
```bash
./push-validator shell
/scripts/register-validator.sh
```

Follow the prompts - it's that simple!

## Essential Commands

| Command | Description |
|---------|-------------|
| `./push-validator start` | Start validator |
| `./push-validator stop` | Stop validator |
| `./push-validator status` | Check sync status |
| `./push-validator logs` | View logs |
| `./push-validator backup` | Backup keys |

## Access Points

Once running, your validator exposes:
- **RPC**: `http://localhost:26657`
- **API**: `http://localhost:1317`
- **gRPC**: `localhost:9090`
- **EVM RPC**: `http://localhost:8545`
- **EVM WebSocket**: `ws://localhost:8546`
- **Metrics**: `http://localhost:6060`

## Troubleshooting

**Not syncing?**
```bash
./push-validator logs
```

**Need help?**
- Docs: https://docs.push.org

## FAQ

**How long does sync take?**
- Testnet: 1-2 hours

**How much PUSH do I need?**
- Testnet: 1 PUSH (free from faucet)
- Mainnet: 10,000 PUSH

**Is my validator working?**
- Check explorer: https://donut.push.network

**Need the EVM RPC endpoint?**
- Testnet: https://evm.rpc-testnet-donut-node1.push.org

**Want to make your validator public?**
- See [PUBLIC_VALIDATOR_SETUP.md](PUBLIC_VALIDATOR_SETUP.md) for nginx/HTTPS setup (optional)

---

Made with ❤️ by Push Protocol