# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM ubuntu:22.04

# Prevent interactive prompts during package installation
ENV DEBIAN_FRONTEND=noninteractive

# Build arguments for multi-arch support
ARG BUILDPLATFORM
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH

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

# Binary configuration - default to known working version
ARG BINARY_VERSION=v1.1.0
ARG BINARY_URL=https://github.com/pushchain/push-chain-node/releases/download
ARG BINARY_CHECKSUM=""

# Multi-architecture binary download with proper platform mapping
RUN echo "🔍 Build Info:" && \
    echo "  BUILDPLATFORM: $BUILDPLATFORM" && \
    echo "  TARGETPLATFORM: $TARGETPLATFORM" && \
    echo "  TARGETOS: $TARGETOS" && \
    echo "  TARGETARCH: $TARGETARCH" && \
    # Map Docker platforms to binary naming convention
    case "$TARGETPLATFORM" in \
        "linux/amd64") \
            BINARY_ARCH="linux-amd64" ;; \
        "linux/arm64") \
            BINARY_ARCH="linux-arm64" ;; \
        "darwin/amd64") \
            BINARY_ARCH="darwin-amd64" ;; \
        "darwin/arm64") \
            BINARY_ARCH="darwin-arm64" ;; \
        *) \
            echo "⚠️  Unsupported platform: $TARGETPLATFORM, defaulting to linux-amd64" && \
            BINARY_ARCH="linux-amd64" ;; \
    esac && \
    echo "🔍 Using binary architecture: $BINARY_ARCH" && \
    if [ "$BINARY_VERSION" = "latest" ]; then \
        echo "🔍 Fetching latest release information..." && \
        LATEST_VERSION=$(curl -s https://api.github.com/repos/pushchain/push-chain-node/releases/latest | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/') && \
        if [ -z "$LATEST_VERSION" ]; then \
            echo "⚠️  Failed to get latest version, using fallback" && \
            LATEST_VERSION="v1.1.0"; \
        fi && \
        echo "📦 Latest release version: $LATEST_VERSION" && \
        DOWNLOAD_URL="${BINARY_URL}/${LATEST_VERSION}/pchaind-${LATEST_VERSION}-${BINARY_ARCH}.tar.gz"; \
    else \
        echo "📦 Using specified version: $BINARY_VERSION" && \
        DOWNLOAD_URL="${BINARY_URL}/${BINARY_VERSION}/pchaind-${BINARY_VERSION}-${BINARY_ARCH}.tar.gz"; \
    fi && \
    echo "📥 Downloading binary from: $DOWNLOAD_URL" && \
    # Try to download, with fallback to v1.1.0 if download fails or file is invalid
    if ! curl -sSL "$DOWNLOAD_URL" -o /tmp/pchaind.tar.gz 2>/dev/null || ! file /tmp/pchaind.tar.gz | grep -q "gzip compressed"; then \
        echo "⚠️  Download failed or invalid file, trying fallback version v1.1.0..." && \
        FALLBACK_URL="${BINARY_URL}/v1.1.0/pchaind-v1.1.0-${BINARY_ARCH}.tar.gz" && \
        curl -sSL "$FALLBACK_URL" -o /tmp/pchaind.tar.gz; \
    fi && \
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