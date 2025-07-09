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
