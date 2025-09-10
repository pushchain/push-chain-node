#!/bin/bash
set -eu

# ---------------------------
# === CONFIGURATION ===
# ---------------------------

UNIVERSAL_ID=${UNIVERSAL_ID:-"1"}
CORE_VALIDATOR_GRPC=${CORE_VALIDATOR_GRPC:-"core-validator-1:9090"}
QUERY_PORT=${QUERY_PORT:-8080}

# Paths
BINARY="/usr/bin/puniversald"
HOME_DIR="/root/.puniversal"

echo "🚨 Starting universal validator $UNIVERSAL_ID setup..."
echo "Core validator GRPC: $CORE_VALIDATOR_GRPC"
echo "Query port: $QUERY_PORT"

# ---------------------------
# === WAIT FOR CORE VALIDATOR ===
# ---------------------------

echo "⏳ Waiting for core validator to be ready..."

# Extract host and port from GRPC endpoint
CORE_HOST=$(echo $CORE_VALIDATOR_GRPC | cut -d: -f1)
CORE_GRPC_PORT=$(echo $CORE_VALIDATOR_GRPC | cut -d: -f2)

# Wait for core validator GRPC to be accessible
max_attempts=60
attempt=0
while [ $attempt -lt $max_attempts ]; do
  # Try to connect to GRPC port
  if nc -z "$CORE_HOST" "$CORE_GRPC_PORT" 2>/dev/null; then
    echo "✅ Core validator GRPC is ready!"
    break
  fi
  echo "Waiting for core validator GRPC... (attempt $((attempt + 1))/$max_attempts)"
  sleep 5
  attempt=$((attempt + 1))
done

if [ $attempt -eq $max_attempts ]; then
  echo "❌ Core validator GRPC not ready after ${max_attempts} attempts"
  exit 1
fi

# ---------------------------
# === INITIALIZATION ===
# ---------------------------

# Clean start
rm -rf "$HOME_DIR"/* "$HOME_DIR"/.[!.]* "$HOME_DIR"/..?* 2>/dev/null || true

echo "🔧 Initializing universal validator..."

# Initialize puniversald (creates config directory and default config)
$BINARY init

# Update the gRPC URL and keyring backend in the config
# The CORE_VALIDATOR_GRPC env var is already set correctly in docker-compose.yml:
# - universal-validator-1 uses core-validator-1:9090
# - universal-validator-2 uses core-validator-2:9090  
# - universal-validator-3 uses core-validator-3:9090
jq '.push_chain_grpc_urls = ["'$CORE_VALIDATOR_GRPC'"] | .keyring_backend = "test"' \
  "$HOME_DIR/config/pushuv_config.json" > "$HOME_DIR/config/pushuv_config.json.tmp" && \
  mv "$HOME_DIR/config/pushuv_config.json.tmp" "$HOME_DIR/config/pushuv_config.json"

# Also update the query server port if different from default
if [ "$QUERY_PORT" != "8080" ]; then
  jq '.query_server_port = '$QUERY_PORT \
    "$HOME_DIR/config/pushuv_config.json" > "$HOME_DIR/config/pushuv_config.json.tmp" && \
    mv "$HOME_DIR/config/pushuv_config.json.tmp" "$HOME_DIR/config/pushuv_config.json"
fi

echo "📋 Universal validator config created:"
cat "$HOME_DIR/config/pushuv_config.json"

# ---------------------------
# === SETUP AUTHZ ===
# ---------------------------

echo "🔐 AuthZ configuration will be handled by the local-validator-manager"

# ---------------------------
# === START UNIVERSAL VALIDATOR ===
# ---------------------------

echo "🚀 Starting universal validator $UNIVERSAL_ID..."
echo "🔗 Connecting to core validator: $CORE_VALIDATOR_GRPC"

exec $BINARY start