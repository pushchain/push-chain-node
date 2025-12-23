#!/bin/bash

###############################################
# Push Chain Linux Binary Builder (via Docker)
#
# Builds a static Linux-compatible `pchaind` binary
# Output: testnet/v1/binary/pchaind
#
# Prerequisites:
# - Docker installed and running
# - dkls23-rs and garbling as sibling directories to push-chain
###############################################

set -e

###############################################################################
# SECTION 1: Setup and Path Resolution
###############################################################################

# Get absolute paths - script can be run from anywhere
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR_ABS="$(cd "$SCRIPT_DIR/../../.." && pwd)"
BINARY_OUT_DIR_ABS="$(cd "$SCRIPT_DIR/../binary" && pwd)"
mkdir -p "$BINARY_OUT_DIR_ABS"

###############################################################################
# SECTION 2: Patch Chain ID
###############################################################################

# Update chain ID in app/app.go from localchain to push_42101-1
APP_FILE="$ROOT_DIR_ABS/app/app.go"
OLD_CHAIN_ID="localchain_9000-1"
NEW_CHAIN_ID="push_42101-1"

if grep -q "$OLD_CHAIN_ID" "$APP_FILE"; then
  echo "🔁 Patching chain ID in app/app.go: $OLD_CHAIN_ID → $NEW_CHAIN_ID"
  if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "s/\"$OLD_CHAIN_ID\"/\"$NEW_CHAIN_ID\"/" "$APP_FILE"
  else
    sed -i "s/\"$OLD_CHAIN_ID\"/\"$NEW_CHAIN_ID\"/" "$APP_FILE"
  fi
else
  echo "✅ Chain ID already set to $NEW_CHAIN_ID in app/app.go"
fi

###############################################################################
# SECTION 3: Verify Required Dependencies
###############################################################################

# Check for dkls23-rs (required) and garbling (optional) as sibling directories
DKLS23_PARENT="$(dirname "$ROOT_DIR_ABS")"
DKLS23_PATH="$DKLS23_PARENT/dkls23-rs"
GARBLING_PATH="$DKLS23_PARENT/garbling"

if [ ! -d "$DKLS23_PATH" ]; then
  echo "❌ ERROR: dkls23-rs not found at $DKLS23_PATH"
  echo "   Please clone it: cd $DKLS23_PARENT && git clone https://github.com/pushchain/dkls23-rs.git"
  exit 1
fi

if [ ! -d "$GARBLING_PATH" ]; then
  echo "⚠️  WARNING: garbling not found at $GARBLING_PATH (may be optional)"
fi

###############################################################################
# SECTION 4: Build Cached Docker Base Image
###############################################################################

# Base image with all build dependencies pre-installed
# This image is cached and reused, making subsequent builds much faster
BASE_IMAGE="push-chain-builder-base:latest"

echo "🐳 Building Linux binary via Docker..."

# Build base image only if it doesn't exist or is wrong architecture
# Contains: Go 1.23.8, Rust, build-essential, and other build tools
if ! docker image inspect "$BASE_IMAGE" >/dev/null 2>&1 || \
   [ "$(docker image inspect "$BASE_IMAGE" --format '{{.Architecture}}')" != "amd64" ]; then
  echo "📦 Building base image with dependencies for linux/amd64 (this will be cached)..."
  docker build --platform linux/amd64 -q -t "$BASE_IMAGE" - <<EOF
FROM golang:1.23.8
# Install system dependencies (build tools, curl, etc.)
RUN apt update -qq && apt install -y -qq curl wget git build-essential pkg-config jq unzip >/dev/null 2>&1 && \
    # Install Rust (required for building dkls23-rs)
    export RUSTUP_HOME=/usr/local/rustup CARGO_HOME=/usr/local/cargo PATH=/usr/local/cargo/bin:\$PATH && \
    curl -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable --profile minimal >/dev/null 2>&1
# Set Rust environment variables
ENV RUSTUP_HOME=/usr/local/rustup CARGO_HOME=/usr/local/cargo PATH=/usr/local/cargo/bin:\$PATH
WORKDIR /code
EOF
fi

# Verify base image was created successfully
if ! docker image inspect "$BASE_IMAGE" >/dev/null 2>&1; then
  echo "❌ ERROR: Failed to build base image"
  exit 1
fi
echo "✅ Base image ready"

###############################################################################
# SECTION 5: Build Binary in Docker Container
###############################################################################

# Run build inside Docker container with mounted volumes
docker run --rm \
  --platform linux/amd64 \
  --pull=never \
  -v "$ROOT_DIR_ABS:/code" \
  -v "$DKLS23_PATH:/dkls23-rs-src" \
  ${GARBLING_PATH:+-v "$GARBLING_PATH:/garbling-src"} \
  "$BASE_IMAGE" \
  bash -c '
    set -e
    
    ###########################################################################
    # Step 1: Copy required directories into container
    ###########################################################################
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "🔄 Step 1: Copying directories..."
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    cd /code
    # Remove any existing directories/symlinks to avoid conflicts
    rm -rf dkls23-rs garbling
    # Copy dkls23-rs and garbling from mounted volumes into /code
    cp -r /dkls23-rs-src dkls23-rs
    [ -d /garbling-src ] && cp -r /garbling-src garbling || true
    # Create symlink so ../dkls23-rs resolves correctly (required by go.mod replace directive)
    ln -sf /code/dkls23-rs /dkls23-rs
    echo "✅ Step 1 complete: Directories copied"
    
    ###########################################################################
    # Step 2: Download CosmWasm wasmvm static library
    ###########################################################################
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "🔄 Step 2: Downloading CosmWasm wasmvm static library..."
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    cd /code
    # Get wasmvm version from go.mod
    WASM_VER=$(go list -m all | grep github.com/CosmWasm/wasmvm | awk "{print \$2}")
    # Detect architecture
    ARCH=$(uname -m | sed "s/x86_64/x86_64/; s/aarch64/aarch64/; s/arm64/aarch64/")
    # Download the static library
    mkdir -p /usr/lib
    wget -qO /usr/lib/libwasmvm_muslc.${ARCH}.a \
      https://github.com/CosmWasm/wasmvm/releases/download/${WASM_VER}/libwasmvm_muslc.${ARCH}.a
    # Create symlink for easier linking
    ln -sf /usr/lib/libwasmvm_muslc.${ARCH}.a /usr/lib/libwasmvm_muslc.a
    echo "✅ Step 2 complete: wasmvm library downloaded"
    
    ###########################################################################
    # Step 3: Update Go dependencies
    ###########################################################################
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "🔄 Step 3: Updating Go dependencies (go mod tidy)..."
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    go mod tidy
    echo "✅ Step 3 complete: Go dependencies updated"
    
    ###########################################################################
    # Step 4: Build pchaind binary (core testnet only needs pchaind, not puniversald)
    ###########################################################################
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "🔄 Step 4: Building pchaind binary..."
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    # Set build environment variables
    export CGO_ENABLED=1 BUILD_TAGS=muslc LINK_STATICALLY=true LEDGER_ENABLED=false
    # Build dkls23-rs library first (if not already built)
    make build-dkls23
    # Build only pchaind (not puniversald) with static linking
    CGO_LDFLAGS="-L/usr/lib -L/code/dkls23-rs/target/release -lwasmvm_muslc -lm -ldl -lpthread -lrt -static" \
    go build -mod=readonly \
      -tags "muslc" \
      -ldflags "-X github.com/cosmos/cosmos-sdk/version.Name=pchain -X github.com/cosmos/cosmos-sdk/version.AppName=pchaind -X github.com/cosmos/cosmos-sdk/version.Version=- -X github.com/cosmos/cosmos-sdk/version.Commit= -s -w -linkmode=external -extldflags \"-Wl,-z,muldefs -static\"" \
      -trimpath \
      -o build/pchaind ./cmd/pchaind
    echo "✅ Step 4 complete: pchaind binary built"
  '

###############################################################################
# SECTION 6: Copy Built Binary to Output Directory
###############################################################################

echo "📁 Copying built binary to $BINARY_OUT_DIR_ABS/pchaind ..."
if [ ! -f "$ROOT_DIR_ABS/build/pchaind" ]; then
  echo "❌ ERROR: Binary not found at $ROOT_DIR_ABS/build/pchaind"
  exit 1
fi
cp "$ROOT_DIR_ABS/build/pchaind" "$BINARY_OUT_DIR_ABS/pchaind"

echo "✅ Done. Linux binary available at: $BINARY_OUT_DIR_ABS/pchaind"
