# 🧱 Push Chain : Universal Node Setup Guide

## 📁 Directory Structure

```
.
├── post-setup/                         # Scripts to run after node is live
│   ├── backup.sh
│   ├── setup_log_rotation.sh           # Set up logrotate (daily, 14-day retention)
│   ├── show_logs.sh                    # Tail logs
│   ├── start.sh                        # Start
│   ├── stop.sh                         # Stop
├── pre-setup/                          # Scripts to prepare infrastructure
│   ├── patch_home_path.sh              # Temp - Patch Home Path
│   ├── prepare_binary.sh               # Build Linux-compatible binary via Docker
│   └── setup_gcp_instance.sh           # Installs Dependencies & Copy files to VM
├── setup/
│   └── prepare_config.sh               # Prepare config and add hotkey
├── README.md                           # You are here
```

---

## 🚀 Pre-Setup

These are the essential steps to prepare a Push Chain node (genesis validator or regular validator or full node) before actual chain setup and start.

### 1. 🛠️ Build Binary for Linux

This step builds a **Linux-compatible static binary** of `puniversald` using Docker, with CosmWasm support (`libwasmvm_muslc`).  
Required to ensure the binary runs properly on GCP VMs or any Linux server.

#### Prerequisites

- Docker is installed and running
- `make build` and `make install` work in your project root

#### Steps

```bash
cd testnet/universal

# Make all scripts in pre-setup executable (just once)
chmod +x ./pre-setup/*

# Build Linux binary using Docker
bash ./pre-setup/prepare_binary.sh
```

> ℹ️ This script auto-downloads the correct version of `libwasmvm_muslc.a` based on your `go.mod` dependency on `CosmWasm/wasmvm`.  
> This is required for CosmWasm smart contract support and may take some time.

#### Output

```
testnet/universal/binary/puniversald
```

### 4. ⚙️ Setup Node Environment on GCP VM

Install system dependencies, Go, and copy files to remote `/home/universal`.

#### Prerequisites

- Your IP is whitelisted in GCP firewall
- You can SSH and SCP into the VM
- Local folders `binary/`, `setup/`, `post-setup/` exist

#### Steps

```bash
bash ./pre-setup/setup_gcp_instance.sh
```

> 💡 You’ll be prompted for:
>
> - GCP VM External IP
> - SSH username

#### Output

- Remote VM is ready with:
  - Go installed globally
  - Push Universal Node files placed at `/home/universal`
- You can SSH into the node with:
  ```bash
  ssh <username>@<external-ip>
  cd /home/app
  ```

---

## 🛠️ Setup

All the following steps must be executed **inside your VM instance** (the one provisioned in the previous section).

> ℹ️ Make sure you’ve SSHed into the instance:
>
> ```bash
> ssh <username>@<external-ip>
> cd /home/universal
> ```

### Set Config and Add Hotkey

```bash
cd /home/universal
bash ./setup/prepare_config.sh
```

- Manually change RPC's in config
- Provide grant to Hotkey via `pchaind` key

```
    ./pchaind tx authz grant push1xuc2d92mn4cv8nme0tqd9qz0j3he0fzrq3llrg generic \
    --msg-type=/uexecutor.v1.MsgVoteInbound \
    --from validator-key \
    --keyring-backend os \
    --fees 200000000000000upc \
    --home /home/app/.pchain \
    --chain-id "push_42101-1" \
    -y
```

## 🛠️ Post-Setup: Node Utilities & Maintenance

Once your node is setup, the following scripts help with start, stop, daily operations, monitoring, and maintenance. All these scripts are located under:

```
/home/universal/post-setup/
```

### ▶️ Start Node

Use this if you need to start the node manually.

```bash
cd /home/universal
bash ./post-setup/start.sh
```

### ⏹️ Stop Node

Stops the running `pchaind` process (based on PID tracking).

```bash
cd /home/universal
bash ./post-setup/stop.sh
```

### 🔁 Log Rotation Setup

Sets up automatic daily log rotation for Push Chain logs to prevent uncontrolled disk usage.

#### Steps

```bash
cd /home/universal
bash ./post-setup/setup_log_rotation.sh
```

### 📜 View Logs

Tails the `puniversal` log with formatting for easier reading.

#### Steps

```bash
cd /home/universal
bash ./post-setup/show_logs.sh
```

### 📜 Backup Data

Backup node Data

#### Steps

```bash
cd /home/universal
bash ./post-setup/backup.sh
```

#### Output

- New backup is created under `/home/universal/backups/`
