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

# Create validator user
RUN useradd -m -s /bin/bash -d /root validator

# Binary download URL (can be overridden during build)
ARG BINARY_URL=https://github.com/push-protocol/push-chain/releases/latest/download/pchaind-linux-amd64
ARG BINARY_PATH=""

# Copy binary - either from local mount or download
RUN if [ -n "$BINARY_PATH" ]; then \
        echo "Using local binary from $BINARY_PATH"; \
    else \
        echo "Downloading binary from $BINARY_URL" && \
        curl -sSL "$BINARY_URL" -o /usr/local/bin/pchaind && \
        chmod +x /usr/local/bin/pchaind; \
    fi

# Create symlink for pchain (matching manual setup)
RUN ln -s /usr/local/bin/pchaind /usr/local/bin/pchain

# Copy scripts
COPY scripts/* /scripts/
RUN chmod +x /scripts/*

# Create necessary directories
RUN mkdir -p /root/.pchain/config /root/.pchain/data && \
    chown -R validator:validator /root

# Switch to validator user
USER validator
WORKDIR /root

# Expose ports
EXPOSE 26656 26657 1317 9090 9091 6060

# Use tini as init system to handle signals properly
ENTRYPOINT ["/usr/bin/tini", "--"]

# Default command
CMD ["/scripts/entrypoint.sh", "start"]