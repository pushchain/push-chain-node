FROM ubuntu:22.04

# Prevent interactive prompts during package installation
ENV DEBIAN_FRONTEND=noninteractive

# Install all required dependencies matching manual setup
RUN apt-get update && apt-get install -y \
    build-essential \
    git \
    golang-go \
    jq \
    python3 \
    python3-pip \
    curl \
    wget \
    netcat \
    ca-certificates \
    tini \
    && rm -rf /var/lib/apt/lists/*

# Install Python dependencies
RUN pip3 install tomlkit

# Binary configuration
ARG BINARY_VERSION=latest
ARG BINARY_URL=https://github.com/push-protocol/push-chain/releases/download
ARG BINARY_CHECKSUM=""

# Download and verify binary
RUN if [ "$BINARY_VERSION" = "latest" ]; then \
        DOWNLOAD_URL="${BINARY_URL}/latest/download/pchaind-linux-amd64"; \
    else \
        DOWNLOAD_URL="${BINARY_URL}/${BINARY_VERSION}/pchaind-linux-amd64"; \
    fi && \
    echo "Downloading binary from $DOWNLOAD_URL" && \
    curl -sSL "$DOWNLOAD_URL" -o /usr/local/bin/pchaind && \
    chmod +x /usr/local/bin/pchaind && \
    # Verify checksum if provided
    if [ -n "$BINARY_CHECKSUM" ]; then \
        echo "$BINARY_CHECKSUM  /usr/local/bin/pchaind" | sha256sum -c || \
        (echo "Checksum verification failed!" && exit 1); \
    fi

# Create symlink for pchain (matching manual setup)
RUN ln -s /usr/local/bin/pchaind /usr/local/bin/pchain

# Copy scripts
COPY scripts/* /scripts/
RUN chmod +x /scripts/*

# Create necessary directories
RUN mkdir -p /root/.pchain/config /root/.pchain/data

# Keep running as root for consistency
WORKDIR /root

# Expose ports
EXPOSE 26656 26657 1317 9090 9091 6060

# Use tini as init system to handle signals properly
ENTRYPOINT ["/usr/bin/tini", "--"]

# Default command
CMD ["/scripts/entrypoint.sh", "start"]