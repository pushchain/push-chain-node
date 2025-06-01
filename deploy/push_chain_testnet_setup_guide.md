# Push Chain Testnet Setup Guide

This guide walks through setting up a multi-node Push Chain testnet with:
- **Genesis Node (val-1)**: Initial validator node that creates the network
- **Validator Node (val-2)**: Additional validator that joins the network

## Network Configuration
- **Chain ID**: `42101`
- **Denomination**: `npc` (base unit), `PUSH` (display unit)
- **Total Supply**: 10 Billion PUSH tokens
- **Genesis Accounts**: 5 accounts with 2 billion PUSH each

---

## Prerequisites

### Local Machine
- Go 1.21+
- Make
- Git
- Push Chain source code

### Remote Nodes
- Ubuntu 22.04 LTS
- 4 vCPU, 10GB RAM, 200GB SSD
- Ports open: 26656 (P2P), 26657 (RPC)

---

## Part 1: Infrastructure Setup

### 1.1 Create GCP Instances (Optional)

```bash
# Reserve static IPs
gcloud compute addresses create push-chain-testnet-genesis-ip --region=us-central1
gcloud compute addresses create push-chain-testnet-val-02-ip --region=us-central1

# Create Genesis Node
gcloud compute instances create push-chain-testnet-genesis \
  --zone=us-central1-a \
  --machine-type=custom-4-10496 \
  --image-family=ubuntu-2204-lts \
  --image-project=ubuntu-os-cloud \
  --boot-disk-size=200GB \
  --boot-disk-type=pd-ssd \
  --address=push-chain-testnet-genesis-ip \
  --tags=cosmos-p2p,http-server,https-server

# Create Validator Node
gcloud compute instances create push-chain-testnet-val-02 \
  --zone=us-central1-a \
  --machine-type=custom-4-10496 \
  --image-family=ubuntu-2204-lts \
  --image-project=ubuntu-os-cloud \
  --boot-disk-size=200GB \
  --boot-disk-type=pd-ssd \
  --address=push-chain-testnet-val-02-ip \
  --tags=cosmos-p2p,http-server,https-server
```

### 1.2 Setup SSH Access
```bash
# Add SSH keys to instances
gcloud compute instances add-metadata push-chain-testnet-genesis \
  --zone=us-central1-a \
  --metadata=ssh-keys="USERNAME:$(cat ~/.ssh/id_rsa.pub)"

gcloud compute instances add-metadata push-chain-testnet-val-02 \
  --zone=us-central1-a \
  --metadata=ssh-keys="USERNAME:$(cat ~/.ssh/id_rsa.pub)"
```

---

## Part 2: Node Preparation

### 2.1 Prepare Both Nodes

**Run on both Genesis and Validator nodes:**

```bash
# Create directories
mkdir ~/app
mkdir ~/.pchain

# Update system
sudo apt-get update

# Install dependencies
sudo apt-get install -y \
    build-essential \
    git \
    golang-go \
    jq \
    python3 \
    python3-pip \
    curl \
    wget \
    netcat

# Install Python dependencies
pip3 install tomlkit

# Update PATH
echo 'export PATH="$HOME/app:$PATH"' >> ~/.bashrc
source ~/.bashrc

# Verify Python version (should be 3.10+)
python3 --version
```

### 2.2 Test Network Connectivity

**On Genesis node:**
```bash
nc -l 26656
```

**On Validator node:**
```bash
telnet <GENESIS_NODE_IP> 26656
```

---

## Part 3: Genesis Node Setup

### 3.1 Build and Upload Binary

**On local machine:**
```bash
cd push-chain
make clean && make build

# Upload to Genesis node
scp "build/pchaind" "<GENESIS_IP>:~/app/pchaind"
scp "deploy/make_first_node.sh" "<GENESIS_IP>:~/app/make_first_node.sh"
scp -r deploy/test-push-chain/scripts/* "<GENESIS_IP>:~/app/"
```

### 3.2 Configure Genesis Node

**On Genesis node:**
```bash
# Set permissions
chmod +x ~/app/pchaind
chmod u+x ~/app/*.sh

# Create symlink
sudo ln -s ~/app/pchaind /usr/local/bin/pchaind

# Initialize and start genesis node
~/app/make_first_node.sh
```

**Expected Genesis Accounts (from make_first_node.sh):**
- `acc1` (faucet): `push10wqxnvqj9q56jtspzg3kcxsuju2ga3lrjcqurc`
- `acc2`: `push1vxzp6fn4wkkjm4ujuq9qdf8tl80sukaplgrsmw`
- `acc3`: `push1d504cdz2m40gt7qgqktl4ctddx63mjjecl99z0`
- `acc4`: `push13dml0rdx029j932d80xrz243x7s5l48a7nac76`
- `acc5`: `push12m9hw26hrzz7ktah4h3mu0ytv2442gvlajsq27`

### 3.3 Start Genesis Node

```bash
# Start the node
~/app/start.sh

# Check logs
~/app/showLogs.sh

# Verify node is running
pchaind status
```

### 3.4 Get Genesis Node Information

```bash
# Get node ID (needed for validator setup)
pchaind tendermint show-node-id

# Check validator status
pchaind query staking validators --output json | jq '.validators[] | {moniker: .description.moniker, status: .status, tokens: .tokens}'
```

---

## Part 4: Validator Node Setup

### 4.1 Upload Binary and Scripts

**On local machine:**
```bash
# Upload to Validator node
scp "build/pchaind" "<VALIDATOR_IP>:~/app/pchaind"
scp -r deploy/test-push-chain/scripts/* "<VALIDATOR_IP>:~/app/"
scp -r deploy/test-push-chain/config/* "<VALIDATOR_IP>:~/app/config-tmp/"
```

### 4.2 Configure Validator Node

**On Validator node:**
```bash
# Set permissions
chmod u+x ~/app/pchaind
chmod u+x ~/app/*.sh

# Create symlink
sudo ln -s ~/app/pchaind /usr/local/bin/pchaind

# Verify binary works
pchaind version
```

### 4.3 Initialize Configuration

```bash
# Reset and initialize config
export CHAIN_DIR="$HOME/.pchain"
~/app/resetConfigs.sh

# Set moniker
python3 ~/app/toml_edit.py \
  ~/.pchain/config/config.toml \
  "moniker" \
  "val-2"

# Configure persistent peers (replace with actual genesis node ID and IP)
export GENESIS_NODE_ID="<GENESIS_NODE_ID_FROM_STEP_3.4>"
export GENESIS_NODE_IP="<GENESIS_NODE_IP>"
export PEER_URL="${GENESIS_NODE_ID}@${GENESIS_NODE_IP}:26656"

python3 ~/app/toml_edit.py \
  ~/.pchain/config/config.toml \
  "p2p.persistent_peers" \
  "$PEER_URL"
```

### 4.4 Copy Genesis File

**Option 1: Copy from local machine**
```bash
# On local machine
scp "<GENESIS_IP>:~/.pchain/config/genesis.json" "./genesis.json"
scp "./genesis.json" "<VALIDATOR_IP>:~/.pchain/config/genesis.json"
```

**Option 2: Direct copy (if nodes can communicate)**
```bash
# On Validator node
scp "<GENESIS_IP>:~/.pchain/config/genesis.json" "~/.pchain/config/genesis.json"
```

### 4.5 Start Validator Node and Sync

```bash
# Start node
~/app/start.sh

# Wait for sync (this script will monitor until fully synced)
~/app/waitFullSync.sh

# Verify sync status
pchaind status | jq '.SyncInfo'
```

---

## Part 5: Validator Registration

### 5.1 Create Validator Wallet

**On Validator node:**
```bash
export KEYRING="test"
export NODE_OWNER_WALLET_NAME="acc21"

# Generate new wallet
pchaind keys add $NODE_OWNER_WALLET_NAME --keyring-backend "$KEYRING"

# Save the mnemonic safely!
# Example output:
# - address: push1pjnedth4tezmu6z2ywwswx8c4lpnerqlgkw3sl
# - mnemonic: gallery awkward dream such inspire scissors table...
```

### 5.2 Fund Validator Wallet

**On Genesis node:**
```bash
export KEYRING="test"
export FAUCET_WALLET="push10wqxnvqj9q56jtspzg3kcxsuju2ga3lrjcqurc"  # acc1
export NODE_OWNER_WALLET="<VALIDATOR_WALLET_ADDRESS>"  # From step 5.1
export ONE_PUSH="000000000000000000npush"
export CHAIN_ID="42101"

# Transfer 30,000 PUSH to validator wallet
pchaind tx bank send "$FAUCET_WALLET" "$NODE_OWNER_WALLET" "30000$ONE_PUSH" \
  --fees 1000000000000000npush \
  --chain-id "$CHAIN_ID" \
  --keyring-backend "$KEYRING" \
  --yes
```

### 5.3 Create Validator Registration

**On Validator node:**
```bash
# Generate validator keys and registration file
export VALIDATOR_PUBKEY=$(pchaind comet show-validator)
export ONE_PUSH="000000000000000000npush"
export VALIDATOR_NAME="val-2"

cat <<EOF > register-validator.json
{
	"pubkey": $VALIDATOR_PUBKEY,
	"amount": "20000$ONE_PUSH",
	"moniker": "$VALIDATOR_NAME",
	"website": "https://push.org",
	"security": "security@push.org",
	"details": "Push Protocol Validator Node 2",
	"commission-rate": "0.10",
	"commission-max-rate": "0.20",
	"commission-max-change-rate": "0.01",
	"min-self-delegation": "1"
}
EOF

# Verify the file
cat register-validator.json
```

### 5.4 Register Validator

```bash
export NODE_OWNER_WALLET_NAME="acc21"
export CHAIN_ID="42101"
export ONE_PUSH="000000000000000000npush"
export GENESIS_NODE_IP="<GENESIS_NODE_IP>"

# Register as validator
pchaind tx staking create-validator register-validator.json \
  --chain-id $CHAIN_ID \
  --fees "1$ONE_PUSH" \
  --gas "1000000" \
  --from $NODE_OWNER_WALLET_NAME \
  --node=tcp://${GENESIS_NODE_IP}:26657 \
  --keyring-backend test \
  --yes
```

### 5.5 Verify Registration

```bash
# Check transaction status
export TX_ID="<TRANSACTION_HASH_FROM_ABOVE>"
pchaind query tx $TX_ID --chain-id $CHAIN_ID --output json | jq '{code, raw_log}'

# Verify validator is active
pchaind query staking validators --output json | jq '.validators[] | {moniker: .description.moniker, status: .status, tokens: .tokens}'

# Should show both validators:
# - "val-1" (genesis validator)
# - "val-2" (your new validator)
```

### 5.6 Restart Validator Node

```bash
# Restart to ensure participation in consensus
~/app/stop.sh
~/app/start.sh

# Monitor logs
~/app/showLogs.sh
```

---

## Part 6: Network Verification

### 6.1 Check Network Health

**On any node:**
```bash
# Check all validators
pchaind query staking validators --output json | jq '.validators[] | {moniker: .description.moniker, status: .status, tokens: .tokens, jailed: .jailed}'

# Check network info
pchaind status | jq '{node_info: .NodeInfo, sync_info: .SyncInfo}'

# Check latest blocks
pchaind query block | jq '.block.header | {height, time, proposer_address}'
```

### 6.2 Test Transactions

```bash
# Check balances
pchaind query bank balances <WALLET_ADDRESS> --chain-id 42101

# Send test transaction
pchaind tx bank send <FROM_WALLET> <TO_WALLET> "1000000000000000000npush" \
  --fees 1000000000000000npush \
  --chain-id 42101 \
  --keyring-backend test \
  --yes
```

---

## Part 7: Maintenance Commands

### 7.1 Node Management

```bash
# Start node
~/app/start.sh

# Stop node
~/app/stop.sh

# Check logs
~/app/showLogs.sh

# Check sync status
~/app/waitFullSync.sh
```

### 7.2 Validator Management

```bash
# Check validator status
pchaind query staking validator $(pchaind keys show acc21 --bech val --address --keyring-backend test)

# Edit validator info
pchaind tx staking edit-validator \
  --new-moniker="New Name" \
  --website="https://new-website.com" \
  --details="Updated description" \
  --chain-id 42101 \
  --fees "1000000000000000000npush" \
  --from acc21 \
  --keyring-backend test \
  --yes

# Unjail validator (if jailed)
pchaind tx slashing unjail \
  --chain-id 42101 \
  --fees "1000000000000000000npush" \
  --from acc21 \
  --keyring-backend test \
  --yes
```

### 7.3 Wallet Management

```bash
# List all wallets
pchaind keys list --keyring-backend test

# Show specific wallet
pchaind keys show <WALLET_NAME> --keyring-backend test

# Export wallet
pchaind keys export <WALLET_NAME> --keyring-backend test

# Import wallet
pchaind keys import <NEW_NAME> --keyring-backend test
```

---

## Part 8: Troubleshooting

### 8.1 Common Issues

**Node not syncing:**
```bash
# Check peers
pchaind query tendermint-validator-set | jq '.validators[].address'

# Check network info
curl http://localhost:26657/net_info | jq '.result.peers'
```

**Validator not active:**
```bash
# Check if validator is jailed
pchaind query staking validator $(pchaind keys show acc21 --bech val --address --keyring-backend test) | jq '.jailed'

# Check validator power
pchaind query tendermint-validator-set | jq '.validators[] | select(.address=="<VALIDATOR_CONSENSUS_ADDRESS>")'
```

**Transaction failures:**
```bash
# Check account sequence
pchaind query auth account <WALLET_ADDRESS>

# Check balance
pchaind query bank balances <WALLET_ADDRESS>
```

### 8.2 Reset Node (Nuclear Option)

```bash
# Stop node
~/app/stop.sh

# Remove data (keeps config)
rm -rf ~/.pchain/data

# Or reset everything
rm -rf ~/.pchain

# Reinitialize
~/app/resetConfigs.sh
# ... repeat setup from Part 4.3
```

---

## Part 9: Network Expansion

### 9.1 Adding More Validators

To add additional validators, repeat Part 4 and Part 5 with:
- Different moniker names (val-3, val-4, etc.)
- Different wallet names (acc31, acc41, etc.)
- Same genesis file
- Same persistent peers configuration

### 9.2 Adding Observer Nodes

For non-validator nodes:
- Skip the validator registration (Part 5)
- Use same genesis file and peer configuration
- Start with `~/app/start.sh`

---

## Important Notes

1. **Security**: Never share private keys or mnemonics
2. **Backups**: Always backup wallet mnemonics and validator keys
3. **Monitoring**: Monitor validator uptime to avoid jailing
4. **Updates**: Coordinate binary updates across all validators
5. **Governance**: Use the governance module for network upgrades

---

## Network Information Summary

- **Chain ID**: `42101`
- **Genesis Node**: Creates network with 5 funded accounts
- **Token Supply**: 10 billion PUSH (each account gets 2 billion)
- **Block Time**: 1000ms (1 second)
- **Consensus**: Tendermint PoS
- **Minimum Commission**: 5% 