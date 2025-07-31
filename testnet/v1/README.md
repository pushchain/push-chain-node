# 🧱 Push Chain : Node Setup Guide

## 📁 Directory Structure

```
.
├── post-setup/                         # Scripts to run after node is live
│   ├── backup.sh                       # Manual backup of `.pchain` data dir
│   ├── setup_log_rotation.sh           # Set up logrotate (daily, 14-day retention)
│   ├── setup_nginx.sh                  # Nginx + HTTPS + rate limiting
│   ├── show_logs.sh                    # Tail logs from ~/.pchain/logs/
│   ├── start.sh                        # Start pchaind
│   ├── stop.sh                         # Stop pchaind
│   └── sync_status.sh                  # Check Cosmos/EVM sync status
├── pre-setup/                          # Scripts to prepare infrastructure
│   ├── create_gcp_instance.sh          # Spin up GCP VM (reserved IP, SSH keys, metadata)
│   ├── generate_genesis_accounts.sh    # Set up genesis accounts
│   ├── prepare_binary.sh               # Build Linux-compatible binary via Docker
│   └── setup_gcp_instance.sh           # Installs Dependencies & Copy files to VM
├── setup/
│   └── setup_genesis_validator.sh      # End-to-end local genesis setup
├── README.md                           # You are here
```

---

## 🚀 Pre-Setup

These are the essential steps to prepare a Push Chain node (genesis validator or regular validator or full node) before actual chain setup and start.

### 1. 🛠️ Build Binary for Linux

This step builds a **Linux-compatible static binary** of `pchaind` using Docker, with CosmWasm support (`libwasmvm_muslc`).  
Required to ensure the binary runs properly on GCP VMs or any Linux server.

#### Prerequisites

- Docker is installed and running
- `make build` and `make install` work in your project root

#### Steps

```bash
cd testnet/v1

# Make all scripts in pre-setup executable (just once)
chmod +x ./pre-setup/*

# Build Linux binary using Docker
bash ./pre-setup/prepare_binary.sh
```

> ℹ️ This script auto-downloads the correct version of `libwasmvm_muslc.a` based on your `go.mod` dependency on `CosmWasm/wasmvm`.  
> This is required for CosmWasm smart contract support and may take some time.

#### Output

```
testnet/v1/binary/pchaind
```

---

### 2. 🧪 (Optional) Set Up Genesis Accounts

This step is **only required if you're setting up the first node (genesis validator)** in the testnet.

It generates `<NUM>` genesis accounts that will collectively hold the initial token supply of the chain (e.g., 10 billion tokens across 5 accounts).

#### Steps

```bash
cd testnet/v1
bash ./pre-setup/generate_genesis_accounts.sh <NUM>
```

> 🔐 **Save the printed mnemonics securely.** These accounts will be funded in the genesis and cannot be recovered if lost.

After generating the accounts, replace the `ADDR1`–`ADDR5` placeholders in `setup/setup_genesis_validator.sh` with the generated Bech32 addresseses.

> ⚠️ This step is intentionally manual to ensure that private keys are generated and stored **only on your local machine**, never on the remote validator node.

---

### 3. 🖥️ Create GCP VM Instance

Provision a Push Chain validator VM with static IP, firewall rules, network setup, and SSH access.

#### Prerequisites

- GCP CLI is installed and authenticated:
  ```bash
  gcloud auth login
  ```
- Project is set to `push-chain-testnet`:
  ```bash
  gcloud config set project push-chain-testnet
  ```
- SSH key is generated locally:
  ```bash
  ssh-keygen -t rsa -f ~/.ssh/YOUR_KEY_NAME
  ```

#### Steps

```bash
cd testnet/v1
bash ./pre-setup/create_gcp_instance.sh
```

> 💡 You will be prompted to enter:
>
> - VM instance name
> - SSH username
> - SSH public key path

#### Output

- GCP VM instance created under `push-chain-testnet`
- You can SSH into it with:
  ```bash
  ssh <username>@<external-ip>
  ```

---

### 4. ⚙️ Setup Node Environment on GCP VM

Install system dependencies, Go, and copy files to remote `/home/app`.

#### Prerequisites

- Your IP is whitelisted in GCP firewall
- You can SSH and SCP into the VM
- Local folders `binary/`, `setup/`, `post-setup/` exist

#### Steps

```bash
cd testnet/v1
bash ./pre-setup/setup_gcp_instance.sh
```

> 💡 You’ll be prompted for:
>
> - GCP VM External IP
> - SSH username

#### Output

- Remote VM is ready with:
  - Go installed globally
  - Push Chain files placed at `/home/app`
- You can SSH into the node with:
  ```bash
  ssh <username>@<external-ip>
  cd /home/app
  ```

---
