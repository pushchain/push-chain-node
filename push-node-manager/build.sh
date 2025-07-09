#!/bin/bash

set -e

echo "🚀 Push Chain Node Docker Setup"
echo "================================"

# Check if we're in the push-node-manager directory
if [ ! -f "Dockerfile" ]; then
    echo "❌ Error: Please run this script from the push-node-manager directory"
    exit 1
fi

# Check if pchaind binary exists
if [ ! -f "pchaind" ]; then
    echo "❌ Error: pchaind binary not found in current directory"
    echo "Please place the pchaind linux/amd64 binary in this directory"
    exit 1
fi

# Note: Genesis file will come from deploy/test-push-chain/config/genesis.json
# No need to check for config/testnet-genesis.json as we're using deploy-config instead

# Copy required files from deploy/test-push-chain
echo "📋 Copying required files from deploy/test-push-chain..."

# Create temporary directories for the build
mkdir -p deploy-scripts
mkdir -p deploy-config

# Copy scripts from deploy/test-push-chain/scripts/*
if [ -d "../deploy/test-push-chain/scripts" ]; then
    echo "  📁 Copying scripts from deploy/test-push-chain/scripts..."
    cp -r ../deploy/test-push-chain/scripts/* deploy-scripts/
    chmod +x deploy-scripts/*.sh
    echo "  ✅ Scripts copied successfully"
else
    echo "  ❌ Warning: deploy/test-push-chain/scripts directory not found"
fi

# Copy config files from deploy/test-push-chain/config/*
if [ -d "../deploy/test-push-chain/config" ]; then
    echo "  📁 Copying config files from deploy/test-push-chain/config..."
    cp -r ../deploy/test-push-chain/config/* deploy-config/
    echo "  ✅ Config files copied successfully"
else
    echo "  ❌ Warning: deploy/test-push-chain/config directory not found"
fi

# Update Dockerfile to copy these files
echo "📝 Updating Dockerfile to include deploy files..."
cat > Dockerfile.tmp << 'EOF'
FROM --platform=linux/amd64 ubuntu:22.04

# Avoid prompts from apt
ENV DEBIAN_FRONTEND=noninteractive

# Install system dependencies
RUN apt-get update && apt-get install -y \
    build-essential \
    git \
    jq \
    python3 \
    python3-pip \
    curl \
    wget \
    netcat \
    tini \
    ca-certificates \
    sudo \
    && rm -rf /var/lib/apt/lists/*

# Install libwasmvm for Cosmos SDK WASM support (version 1.5.4 as required by pchaind)
RUN wget https://github.com/CosmWasm/wasmvm/releases/download/v1.5.4/libwasmvm.x86_64.so -O /lib/libwasmvm.x86_64.so

# Install Go 1.23.7
RUN wget https://go.dev/dl/go1.23.7.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go1.23.7.linux-amd64.tar.gz && \
    rm go1.23.7.linux-amd64.tar.gz

# Set Go environment variables
ENV PATH="/usr/local/go/bin:$PATH"
ENV GOPATH="/usr/local"

# Install Python dependencies
RUN pip3 install tomlkit

# Clean up any existing directories and create fresh ones
RUN rm -rf /root/app /root/snap /root/.pchain /root/.pchaind && \
    mkdir -p /root/app && \
    mkdir -p /root/.pchaind

# Copy the pchaind binary to ~/app/
COPY pchaind /root/app/pchaind

# Copy scripts from deploy/test-push-chain/scripts/* to ~/app/
COPY deploy-scripts/* /root/app/

# Copy config files from deploy/test-push-chain/config/* to ~/app/config-tmp/
COPY deploy-config/* /root/app/config-tmp/

# Set permissions
RUN chmod u+x /root/app/pchaind && \
    chmod u+x /root/app/*.sh

# Remove existing pchain symlink and create new one
RUN rm -f /usr/local/bin/pchain && \
    ln -s /root/app/pchaind /usr/local/bin/pchain

# Note: Genesis file comes from deploy-config/genesis.json

# Copy original scripts for compatibility
COPY scripts/ /scripts/
RUN chmod +x /scripts/*

# Create necessary directories
RUN mkdir -p /root/.pchain/config /root/.pchain/data

# Set working directory
WORKDIR /root

# Environment variables with defaults
ENV NETWORK=testnet
ENV MONIKER=donut-node2
ENV VALIDATOR_MODE=false
ENV AUTO_INIT=true
ENV KEYRING=test
ENV LOG_LEVEL=info
ENV PRUNING=nothing
ENV MINIMUM_GAS_PRICES=1000000000upc
ENV PCHAIN_HOME=/root/.pchain
ENV HOME_DIR=/root/.pchain
ENV CHAIN_DIR=/root/.pchain

# Node connection environment variables
ENV PN1_URL="dc323e3c930d12369723373516267a213d74ea37@34.57.209.0:26656"

# Expose ports
EXPOSE 26656 26657 1317 9090 9091 6060 8545 8546

# Use tini as init system to handle signals properly
ENTRYPOINT ["/usr/bin/tini", "--"]

# Default command - use the new setup script
CMD ["/scripts/entrypoint.sh", "start"]
EOF

# Replace the original Dockerfile
mv Dockerfile.tmp Dockerfile

# Make sure binary is executable
chmod +x pchaind

# Make sure scripts are executable
chmod +x scripts/*.sh scripts/*.py

echo "✅ All files are present and executable"

# Build the Docker image
echo "🔨 Building Docker image..."
docker build -t push-chain-node .

echo "✅ Docker image built successfully"

# Clean up temporary directories
echo "🧹 Cleaning up temporary files..."
rm -rf deploy-scripts deploy-config

# Check if docker-compose is available
if command -v docker-compose &> /dev/null; then
    echo "🚀 Starting the node with docker-compose..."
    docker-compose up -d
    
    echo "✅ Node started successfully!"
    echo ""
    echo "📊 Useful commands:"
    echo "  View logs:     docker-compose logs -f push-chain-node"
    echo "  Check status:  curl http://localhost:26657/status"
    echo "  Stop node:     docker-compose down"
    echo "  Node shell:    docker-compose exec push-chain-node bash"
    echo ""
    echo "🌐 Endpoints:"
    echo "  RPC:           http://localhost:26657"
    echo "  REST API:      http://localhost:1317"
    echo "  JSON-RPC:      http://localhost:8545"
    echo "  gRPC:          localhost:9090"
    
elif command -v docker &> /dev/null; then
    echo "🚀 Starting the node with docker run..."
    docker run -d --name push-chain-node \
        -p 26656:26656 -p 26657:26657 -p 1317:1317 \
        -p 9090:9090 -p 8545:8545 -p 8546:8546 \
        -v push-chain-data:/root/.pchain \
        push-chain-node
    
    echo "✅ Node started successfully!"
    echo ""
    echo "📊 Useful commands:"
    echo "  View logs:     docker logs -f push-chain-node"
    echo "  Check status:  curl http://localhost:26657/status"
    echo "  Stop node:     docker stop push-chain-node"
    echo "  Remove node:   docker rm push-chain-node"
    echo "  Node shell:    docker exec -it push-chain-node bash"
    
else
    echo "❌ Error: Neither docker-compose nor docker command found"
    echo "Please install Docker and Docker Compose"
    exit 1
fi

echo ""
echo "🎉 Setup complete! Your Push Chain node is running."
echo "⏳ Please wait a few moments for the node to initialize and start syncing."
echo ""
echo "🔧 Custom Setup Features:"
echo "  - Uses resetConfigs.sh for initialization"
echo "  - Moniker set to: donut-node2"
echo "  - Persistent peers configured"
echo "  - Uses custom start/stop scripts"
echo "  - Automatic log monitoring"
echo "  - Genesis file from deploy-config"
echo "  - Pre-configured config files (config.toml, app.toml, client.toml)" 