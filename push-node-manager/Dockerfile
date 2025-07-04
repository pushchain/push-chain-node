# syntax=docker/dockerfile:1
# Stage 1: Copy local binary
FROM --platform=$BUILDPLATFORM alpine:latest AS builder
COPY pchaind /host/pchaind

# Stage 2: Runtime image
FROM --platform=$BUILDPLATFORM ubuntu:22.04

# Prevent interactive prompts during package installation
ENV DEBIAN_FRONTEND=noninteractive

# Build arguments for multi-arch support
ARG BUILDPLATFORM
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH

# Install all required dependencies (without golang-go, we'll install newer version)
RUN apt-get update && apt-get install -y \
    build-essential \
    git \
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

# Install Go 1.23 (compatible with the repository)
RUN case "$TARGETARCH" in \
        "amd64") \
            GO_ARCH="amd64" ;; \
        "arm64") \
            GO_ARCH="arm64" ;; \
        *) \
            echo "⚠️  Unsupported architecture: $TARGETARCH, defaulting to amd64" && \
            GO_ARCH="amd64" ;; \
    esac && \
    echo "🔍 Installing Go 1.23.4 for architecture: $GO_ARCH" && \
    wget -O go.tar.gz "https://go.dev/dl/go1.23.4.linux-${GO_ARCH}.tar.gz" && \
    tar -C /usr/local -xzf go.tar.gz && \
    rm go.tar.gz && \
    echo "✅ Go 1.23.4 installed successfully"

# Set Go environment variables
ENV PATH="/usr/local/go/bin:$PATH"
ENV GOPATH="/usr/local"

# Install Python dependencies
RUN pip3 install tomlkit

# Install libwasmvm for the target architecture
RUN case "$TARGETARCH" in \
        "amd64") \
            WASMVM_ARCH="x86_64" ;; \
        "arm64") \
            WASMVM_ARCH="aarch64" ;; \
        *) \
            echo "⚠️  Unsupported architecture: $TARGETARCH, defaulting to x86_64" && \
            WASMVM_ARCH="x86_64" ;; \
    esac && \
    echo "🔍 Installing libwasmvm for architecture: $WASMVM_ARCH" && \
    WASMVM_VERSION="1.5.4" && \
    wget "https://github.com/CosmWasm/wasmvm/releases/download/v${WASMVM_VERSION}/libwasmvm.${WASMVM_ARCH}.so" \
        -O "/usr/lib/libwasmvm.${WASMVM_ARCH}.so" && \
    chmod +x "/usr/lib/libwasmvm.${WASMVM_ARCH}.so" && \
    ln -sf "/usr/lib/libwasmvm.${WASMVM_ARCH}.so" "/usr/lib/libwasmvm.so" && \
    echo "📋 Installed libraries:" && \
    ls -la /usr/lib/libwasmvm* && \
    ldconfig && \
    echo "✅ libwasmvm installed successfully"

# Set library path environment variable
ENV LD_LIBRARY_PATH="/usr/lib:$LD_LIBRARY_PATH"

# Copy local pchaind binary instead of building from source
COPY --from=builder /host/pchaind /usr/local/bin/pchaind
RUN chmod +x /usr/local/bin/pchaind && \
    echo "✅ Local binary copied successfully!" && \
    echo "🔍 Binary dependencies:" && \
    ldd /usr/local/bin/pchaind | grep -i wasmvm || echo "No wasmvm dependency found"

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