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
    bc \
    && rm -rf /var/lib/apt/lists/*

# Install Python dependencies
RUN pip3 install tomlkit

# Binary configuration
ARG BINARY_VERSION=latest
ARG BINARY_URL=https://github.com/pushchain/push-chain-node/releases/download
ARG BINARY_CHECKSUM=""
ARG TARGETARCH=amd64

# Download and verify binary
RUN if [ "$BINARY_VERSION" = "latest" ]; then \
        echo "🔍 Fetching latest release information..." && \
        # Get latest release version
        LATEST_VERSION=$(curl -s https://api.github.com/repos/pushchain/push-chain-node/releases/latest | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/') && \
        if [ -z "$LATEST_VERSION" ]; then \
            echo "⚠️  Failed to get latest version, using fallback" && \
            LATEST_VERSION="v1.0.5"; \
        fi && \
        echo "📦 Latest release version: $LATEST_VERSION" && \
        DOWNLOAD_URL="${BINARY_URL}/${LATEST_VERSION}/pchaind-${LATEST_VERSION}-linux-${TARGETARCH}.tar.gz"; \
    else \
        echo "📦 Using specified version: $BINARY_VERSION" && \
        DOWNLOAD_URL="${BINARY_URL}/${BINARY_VERSION}/pchaind-${BINARY_VERSION}-linux-${TARGETARCH}.tar.gz"; \
    fi && \
    echo "📥 Downloading binary from: $DOWNLOAD_URL" && \
    curl -sSL "$DOWNLOAD_URL" -o /tmp/pchaind.tar.gz && \
    echo "📦 Extracting binary..." && \
    tar -xzf /tmp/pchaind.tar.gz -C /tmp && \
    mv /tmp/pchaind /usr/local/bin/pchaind && \
    chmod +x /usr/local/bin/pchaind && \
    rm -f /tmp/pchaind.tar.gz && \
    echo "✅ Binary installation complete!" && \
    # Verify checksum if provided
    if [ -n "$BINARY_CHECKSUM" ]; then \
        echo "🔒 Verifying checksum..." && \
        echo "$BINARY_CHECKSUM  /usr/local/bin/pchaind" | sha256sum -c || \
        (echo "❌ Checksum verification failed!" && exit 1); \
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