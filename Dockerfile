FROM golang:1.23-alpine3.20 AS build-env

SHELL ["/bin/sh", "-ecuxo", "pipefail"]

RUN set -eux; apk add --no-cache \
    ca-certificates \
    build-base \
    git \
    linux-headers \
    bash \
    binutils-gold \
    wget \
    curl

# Install Rust via rustup (for latest stable version with edition2024 support)
ENV RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo \
    PATH=/usr/local/cargo/bin:$PATH

RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable --profile minimal && \
    rustc --version && \
    cargo --version

WORKDIR /code

# Copy dkls23-rs and garbling first (needed for go.mod replace directive)
COPY dkls23-rs ./dkls23-rs
COPY garbling ./garbling

# Download go modules + wasmvm static library
ADD go.mod go.sum ./
RUN set -eux; \
    go mod download; \
    \
    # Detect if wasmvm is being used
    WASM_VERSION_LINE=$(go list -m -json all | grep -A2 "github.com/CosmWasm/wasmvm" || true); \
    if [ ! -z "${WASM_VERSION_LINE}" ]; then \
        WASMVM_VERS=$(go list -m all | grep github.com/CosmWasm/wasmvm | awk '{print $2}'); \
        ARCH=$(uname -m); \
        \
        case "$ARCH" in \
            x86_64) ARCH_DL="x86_64" ;; \
            aarch64) ARCH_DL="aarch64" ;; \
            arm64) ARCH_DL="aarch64" ;; \
            *) echo "Unsupported architecture: $ARCH"; exit 1 ;; \
        esac; \
        \
        echo "Downloading wasmvm static library for arch=$ARCH_DL version=$WASMVM_VERS"; \
        wget -O /usr/lib/libwasmvm_muslc.${ARCH_DL}.a \
          https://github.com/CosmWasm/wasmvm/releases/download/${WASMVM_VERS}/libwasmvm_muslc.${ARCH_DL}.a; \
        \
        ln -sf /usr/lib/libwasmvm_muslc.${ARCH_DL}.a /usr/lib/libwasmvm_muslc.a; \
    fi;

# Copy all source
COPY . /code

# Create symlink for dkls23-rs at expected location (../dkls23-rs relative to /code)
RUN ln -sf /code/dkls23-rs /dkls23-rs

# Patch Cargo.toml to use local garbling path instead of git (AFTER copying all source)
RUN echo "=== Patching Cargo.toml files ===" && \
    # Find and patch all Cargo.toml files that reference hd-migration
    find /code/dkls23-rs -name "Cargo.toml" -type f -exec grep -l "hd-migration" {} \; | while read -r file; do \
        echo "Patching: $file" && \
        # Show the line before patching
        grep -n "hd-migration" "$file" || echo "hd-migration not found" && \
        # Replace git dependency with local path (flexible pattern matching)
        sed -i 's|hd-migration = { git = "https://github.com/pushchain/garbling.git", branch = "main" }|hd-migration = { path = "../garbling/crates/hd-migration" }|g' "$file" && \
        sed -i 's|hd-migration = { git = "https://github.com/pushchain/garbling.git", branch = "main"}|hd-migration = { path = "../garbling/crates/hd-migration" }|g' "$file" && \
        # Show the line after patching
        echo "After patching:" && \
        grep -n "hd-migration" "$file" || echo "hd-migration not found after patching"; \
    done && \
    echo "=== Adding staticlib to crate-type ===" && \
    # Add staticlib to crate-type for go-dkls crate (needed to build .a file)
    if grep -q 'crate-type = \["cdylib", "rlib"\]' /code/dkls23-rs/wrapper/go-dkls/Cargo.toml; then \
        sed -i 's/crate-type = \["cdylib", "rlib"\]/crate-type = ["staticlib", "cdylib", "rlib"]/' /code/dkls23-rs/wrapper/go-dkls/Cargo.toml && \
        echo "Added staticlib to crate-type"; \
    else \
        echo "crate-type already includes staticlib or has different format"; \
    fi && \
    echo "=== Patch complete ==="

# Build pchaind as fully static muslc binary
RUN LEDGER_ENABLED=false BUILD_TAGS=muslc LINK_STATICALLY=true make build-dkls23 && \
    echo "=== Verifying libgodkls was built ===" && \
    ls -la /code/dkls23-rs/target/release/libgodkls* 2>/dev/null || (echo "ERROR: libgodkls not found in target/release" && find /code/dkls23-rs/target -name "*godkls*" -type f && exit 1) && \
    # Copy libgodkls to /lib for easy linking (like Dockerfile.base)
    cp /code/dkls23-rs/target/release/libgodkls.a /lib/ && \
    echo "=== Building Go binaries with CGO_LDFLAGS ===" && \
    CGO_LDFLAGS="-L/lib -L/code/dkls23-rs/target/release" \
    LEDGER_ENABLED=false BUILD_TAGS=muslc LINK_STATICALLY=true make build && \
    file /code/build/pchaind && \
    echo "Ensuring binary is statically linked ..." && \
    (file /code/build/pchaind | grep "statically linked")

# --------------------------------------------------------
FROM alpine:3.21

COPY --from=build-env /code/build/pchaind /usr/bin/pchaind

RUN apk add --no-cache \
    ca-certificates \
    curl \
    make \
    bash \
    jq \
    sed

WORKDIR /opt

EXPOSE 1317 26656 26657 8545 8546

CMD ["/usr/bin/pchaind", "version"]