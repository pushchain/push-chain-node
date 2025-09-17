#!/bin/bash

###############################################
# Push Chain Linux Binary Builder (via Docker)
#
# Builds a static Linux-compatible `pchaind` binary
# Output: testnet/v1/binary/pchaind
#
# Prerequisites:
# - Docker installed and running
# - Valid Makefile with `install` and `build` targets
###############################################

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$SCRIPT_DIR/../../.."
BINARY_OUT_DIR="$SCRIPT_DIR/../binary"
mkdir -p "$BINARY_OUT_DIR"

# 🔧 Patch chain ID inside app/app.go
APP_FILE="$ROOT_DIR/app/app.go"
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

echo "🐳 Building Linux binary via Docker..."

docker run --rm \
  -v "$ROOT_DIR":/app \
  -w /app \
  --platform linux/amd64 \
  golang:1.23.7 \
  bash -c '
    set -e

    echo "📦 Installing build dependencies..."
    apt update -qq && apt install -y curl wget git build-essential pkg-config jq unzip

    # Fetch wasmvm version from go.mod
    WASM_LINE=$(go list -m all | grep github.com/CosmWasm/wasmvm)
    WASM_REPO=$(echo $WASM_LINE | awk "{print \$1}")
    WASM_VER=$(echo $WASM_LINE | awk "{print \$2}")

    echo "📥 Downloading libwasmvm_muslc for $WASM_VER"
    wget -qO /usr/local/lib/libwasmvm_muslc.a \
      https://github.com/CosmWasm/wasmvm/releases/download/${WASM_VER}/libwasmvm_muslc.x86_64.a

    echo "🔧 Setting up build environment..."
    export CGO_ENABLED=1
    export BUILD_TAGS="muslc"
    export LINK_STATICALLY=true
    export LEDGER_ENABLED=false
    export CGO_LDFLAGS="-L/usr/local/lib -lwasmvm_muslc -lm -ldl -lpthread -lrt -static"

    echo "⚙️  Running make build..."
    make build
  '

echo "📁 Copying built binary to $BINARY_OUT_DIR/pchaind ..."
cp "$ROOT_DIR/build/pchaind" "$BINARY_OUT_DIR/pchaind"

echo "✅ Done. Linux binary available at: $BINARY_OUT_DIR/pchaind"
