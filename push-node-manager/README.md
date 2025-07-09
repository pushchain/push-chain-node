# Push Chain Node Docker Setup

This Docker setup allows you to run a Push Chain node with the provided `pchaind` binary and testnet genesis file. The setup includes custom initialization scripts that follow the exact deployment sequence used in production.

## Prerequisites

- Docker and Docker Compose installed
- The `pchaind` binary (linux/amd64) placed in this directory
- The testnet genesis file in `config/testnet-genesis.json`
- Access to the `deploy/test-push-chain` directory for scripts and config files

## Quick Start

1. **Build and run the node:**
   ```bash
   ./build.sh
   ```

   This will:
   - Copy required files from `deploy/test-push-chain/`
   - Build the Docker image with custom setup
   - Start the node using docker-compose

2. **View logs:**
   ```bash
   docker-compose logs -f push-chain-node
   ```

3. **Stop the node:**
   ```bash
   docker-compose down
   ```

## Custom Setup Features

This Docker setup includes a custom initialization process that mirrors the production deployment:

### 🔧 Setup Sequence

1. **Directory Structure**: Creates `~/app/` directory structure
2. **File Placement**: Copies `pchaind` binary and scripts to `~/app/`
3. **Permissions**: Sets executable permissions on binary and scripts
4. **Symlink**: Creates `/usr/local/bin/pchain` → `~/app/pchaind`
5. **Configuration**: Runs `resetConfigs.sh` for initialization
6. **Moniker**: Sets moniker to `donut-node2`
7. **Peers**: Configures persistent peers automatically
8. **Genesis**: Handles genesis file setup
9. **Validator State**: Initializes validator state file
10. **Startup**: Uses custom `start.sh` and `stop.sh` scripts

### 📁 File Structure in Container

```
/root/
├── app/                        # Main application directory
│   ├── pchaind                 # Push Chain binary
│   ├── resetConfigs.sh         # Configuration reset script
│   ├── start.sh               # Node startup script
│   ├── stop.sh                # Node stop script
│   ├── showLogs.sh            # Log viewing script
│   ├── waitFullSync.sh        # Sync waiting script
│   ├── toml_edit.py           # TOML configuration editor
│   ├── updatePath.sh          # Path update script
│   └── config-tmp/            # Configuration files from deploy/test-push-chain/config/
│       ├── genesis.json       # Chain genesis file
│       ├── config.toml        # Node configuration
│       ├── app.toml          # Application configuration
│       └── client.toml       # Client configuration
├── .pchain/                   # Node data directory
│   ├── config/                # Node configuration
│   └── data/                  # Blockchain data
└── scripts/                   # Docker entrypoint scripts
    ├── entrypoint.sh          # Main entrypoint
    ├── common.sh              # Common functions
    └── networks.json          # Network configurations
```

## Configuration

### Environment Variables

You can customize the node by modifying the environment variables in `docker-compose.yml`:

| Variable | Default | Description |
|----------|---------|-------------|
| `NETWORK` | `testnet` | Network to connect to |
| `MONIKER` | `donut-node2` | Node identifier (custom setup) |
| `VALIDATOR_MODE` | `false` | Whether to run as validator |
| `AUTO_INIT` | `true` | Auto-initialize node on startup |
| `KEYRING` | `test` | Keyring backend |
| `LOG_LEVEL` | `info` | Logging level |
| `PRUNING` | `nothing` | Pruning strategy |
| `MINIMUM_GAS_PRICES` | `1000000000upc` | Minimum gas prices |
| `PN1_URL` | `dc323e3c930d12369723373516267a213d74ea37@34.57.209.0:26656` | Persistent peer URL |
| `HOME_DIR` | `/root/.pchain` | Home directory path |
| `CHAIN_DIR` | `/root/.pchain` | Chain directory path |

### Ports

The following ports are exposed:

| Port | Service | Description |
|------|---------|-------------|
| 26656 | P2P | Peer-to-peer communication |
| 26657 | RPC | Tendermint RPC |
| 1317 | REST API | Cosmos REST API |
| 9090 | gRPC | Cosmos gRPC |
| 9091 | gRPC Web | Cosmos gRPC Web |
| 6060 | Profiling | Go profiling |
| 8545 | JSON-RPC | Ethereum JSON-RPC |
| 8546 | JSON-RPC WS | Ethereum JSON-RPC WebSocket |

## Usage Examples

### Running a Full Node

```bash
# Build and start with custom setup
./build.sh
```

### Manual Docker Commands

```bash
# Build image manually
docker build -t push-chain-node .

# Run with custom setup
docker run -d --name push-chain-node \
  -p 26656:26656 -p 26657:26657 -p 1317:1317 \
  -p 9090:9090 -p 8545:8545 -p 8546:8546 \
  -v push-chain-data:/root/.pchain \
  push-chain-node
```

### Accessing the Node

```bash
# Execute commands in the container
docker-compose exec push-chain-node pchaind status

# Access the container shell
docker-compose exec push-chain-node bash

# View node status via RPC
curl http://localhost:26657/status

# Use custom app scripts
docker-compose exec push-chain-node /root/app/showLogs.sh
```

### Custom Script Usage

```bash
# Show logs using custom script
docker-compose exec push-chain-node /root/app/showLogs.sh

# Check sync status
docker-compose exec push-chain-node /root/app/waitFullSync.sh

# Stop/start using custom scripts
docker-compose exec push-chain-node /root/app/stop.sh
docker-compose exec push-chain-node /root/app/start.sh
```

### Key Management

```bash
# List keys
docker-compose exec push-chain-node pchaind keys list

# Add a new key
docker-compose exec push-chain-node pchaind keys add mykey

# Show key details
docker-compose exec push-chain-node pchaind keys show mykey
```

## Genesis File Management

The setup supports multiple genesis file sources with the following priority order:

1. **Deploy Config**: Uses `genesis.json` from `deploy/test-push-chain/config/` (highest priority)
2. **Mounted Volume**: Place `genesis.json` in `/genesis/` volume
3. **Fallback**: Uses default testnet genesis from `config/testnet-genesis.json`

The deploy-config directory (`/root/app/config-tmp/`) contains all necessary configuration files:
- `genesis.json` - Chain genesis file
- `config.toml` - Node configuration
- `app.toml` - Application configuration  
- `client.toml` - Client configuration

### Custom Genesis File

```bash
# Copy genesis from another node (example)
docker cp source-node:/root/.pchain/config/genesis.json ./genesis.json

# Mount it to the container
docker run -v ./genesis.json:/genesis/genesis.json push-chain-node
```

## Data Persistence

Node data is persisted in a Docker volume named `push-chain-data`. To reset the node:

```bash
# Stop and remove containers
docker-compose down

# Remove the data volume
docker volume rm push-node-manager_push-chain-data

# Start fresh
./build.sh
```

## Monitoring

### Health Check

The container includes a health check that monitors the RPC endpoint:

```bash
# Check container health
docker-compose ps
```

### Logs

```bash
# View real-time logs
docker-compose logs -f push-chain-node

# View custom chain logs
docker-compose exec push-chain-node tail -f /root/app/chain.log

# View last 100 lines using custom script
docker-compose exec push-chain-node /root/app/showLogs.sh
```

### Node Status

```bash
# Check sync status
curl -s http://localhost:26657/status | jq '.result.sync_info'

# Check node info
curl -s http://localhost:26657/status | jq '.result.node_info'

# Get node ID
docker-compose exec push-chain-node pchaind tendermint show-node-id
```

## Troubleshooting

### Common Issues

1. **Port conflicts**: Ensure the required ports are not in use by other services
2. **Permissions**: Make sure the `pchaind` binary has execute permissions
3. **Genesis file**: Verify the genesis file is valid JSON and in the correct location
4. **Deploy files**: Ensure `deploy/test-push-chain/` directory exists and contains required files

### Debug Mode

To run with debug logging:

```bash
# Edit docker-compose.yml
# - LOG_LEVEL=debug

docker-compose up -d
```

### Container Shell Access

```bash
# Access container shell for debugging
docker-compose exec push-chain-node bash

# Check app directory
docker-compose exec push-chain-node ls -la /root/app/

# Verify configuration
docker-compose exec push-chain-node cat /root/.pchain/config/config.toml | grep moniker
```

### Custom Setup Validation

```bash
# Verify custom setup is working
docker-compose exec push-chain-node ls -la /root/app/
docker-compose exec push-chain-node /usr/local/bin/pchain version
docker-compose exec push-chain-node cat /root/.pchain/config/config.toml | grep "donut-node2"
```

## File Structure

```
push-node-manager/
├── Dockerfile              # Docker image definition
├── docker-compose.yml      # Docker Compose configuration
├── build.sh               # Build script with custom setup
├── README.md              # This file
├── pchaind                # Push Chain binary (linux/amd64)
├── config/
│   └── testnet-genesis.json # Testnet genesis file
└── scripts/
    ├── entrypoint.sh      # Main entrypoint script (with custom setup)
    ├── common.sh          # Common functions
    ├── toml_edit.py       # TOML configuration editor
    └── networks.json      # Network configurations
```

## Building from Source

The build script handles everything automatically:

```bash
# Run the build script
./build.sh
```

This will:
1. Copy files from `deploy/test-push-chain/`
2. Update the Dockerfile with proper file paths
3. Build the Docker image
4. Start the container with docker-compose

## Running Local Test Chain

This setup also supports running a local test chain with `localchain_9000-1`:

### Prerequisites for Local Chain

1. The `pchaind` binary (same as above)
2. The localchain genesis file at `config/localchain-genesis.json`

### Quick Start for Local Chain

1. **Setup and build the local chain image:**
   ```bash
   ./setup-localchain.sh
   ```

2. **Run the local node:**
   ```bash
   ./run-local.sh
   ```

3. **Local chain details:**
   - Chain ID: `localchain_9000-1`
   - Tendermint Node ID: `ecd5f2723f0cd1c78b0546a39f0607028548f288`
   - Moniker: `localvalidator`
   - Initial accounts:
     - acc1: `push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20` (5,000,000,000 UPC)
     - acc2: `push1j0v5urpud7kwsk9zgz2tc0v9d95ct6t5qxv38h` (3,000,000,000 UPC)
     - acc3: `push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9` (2,000,000,000 UPC)

### Local Chain Docker Commands

```bash
# View logs
docker-compose -f docker-compose.localchain.yml logs -f

# Stop the local node
docker-compose -f docker-compose.localchain.yml down

# Access the container
docker-compose -f docker-compose.localchain.yml exec push-chain-localnode bash

# Check status
curl http://localhost:26657/status
```

### Local Chain Configuration

The local chain uses a separate docker-compose file (`docker-compose.localchain.yml`) with:
- Separate data volume (`push-chain-localdata`)
- Local validator setup
- No external peers
- Custom genesis from test_node.sh script

## Support

For issues and questions:
- Check the container logs: `docker-compose logs push-chain-node`
- Check custom logs: `docker-compose exec push-chain-node /root/app/showLogs.sh`
- Verify the node status: `curl http://localhost:26657/status`
- Access the container shell: `docker-compose exec push-chain-node bash`
- Validate custom setup: `docker-compose exec push-chain-node ls -la /root/app/` 