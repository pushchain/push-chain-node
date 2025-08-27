#!/bin/bash
set -eu

# ---------------------------
# === AUTHZ SETUP SCRIPT ===
# ---------------------------

UNIVERSAL_ID=${UNIVERSAL_ID:-"1"}
CORE_VALIDATOR_GRPC=${CORE_VALIDATOR_GRPC:-"core-validator-1:9090"}

# Paths
BINARY="/usr/bin/puniversald"
HOME_DIR="/root/.puniversal"
CONFIG_DIR="$HOME_DIR/config"
AUTHZ_CONFIG="$CONFIG_DIR/authz_config.json"
AUTHZ_KEYS_DIR="$HOME_DIR/authz_keys"

echo "🔐 Setting up AuthZ configuration for Universal Validator $UNIVERSAL_ID..."

# Ensure directories exist
mkdir -p "$CONFIG_DIR" "$AUTHZ_KEYS_DIR"

# Copy AuthZ config template if it doesn't exist
if [ ! -f "$AUTHZ_CONFIG" ]; then
    echo "📋 Creating AuthZ configuration..."
    cp /opt/configs/authz_config.json "$AUTHZ_CONFIG"
    
    # Update config with dynamic values
    sed -i "s|core-validator-1:9090|$CORE_VALIDATOR_GRPC|g" "$AUTHZ_CONFIG"
    
    # Update HTTP RPC endpoint based on GRPC endpoint
    HTTP_RPC_ENDPOINT=$(echo "$CORE_VALIDATOR_GRPC" | sed 's/:9090/:26657/')
    sed -i "s|core-validator-1:26657|$HTTP_RPC_ENDPOINT|g" "$AUTHZ_CONFIG"
fi

# Generate hot key if it doesn't exist
HOT_KEY_FILE="$AUTHZ_KEYS_DIR/hot_key_mnemonic.txt"
if [ ! -f "$HOT_KEY_FILE" ]; then
    echo "🔑 Generating new hot key..."
    
    # Generate hot key using puniversald
    echo "test1234" | $BINARY keys add hot_key --keyring-backend test --output json > "$AUTHZ_KEYS_DIR/hot_key.json" 2>/dev/null || true
    
    # Generate operator key for testing (in real setup, this would be imported)
    echo "test1234" | $BINARY keys add operator --keyring-backend test --output json > "$AUTHZ_KEYS_DIR/operator.json" 2>/dev/null || true
    
    # Extract addresses and update config
    if [ -f "$AUTHZ_KEYS_DIR/hot_key.json" ]; then
        HOT_KEY_ADDR=$(jq -r '.address' "$AUTHZ_KEYS_DIR/hot_key.json")
        OPERATOR_ADDR=$(jq -r '.address' "$AUTHZ_KEYS_DIR/operator.json" 2>/dev/null || echo "")
        
        # Update AuthZ config with addresses
        jq ".hot_key.address = \"$HOT_KEY_ADDR\"" "$AUTHZ_CONFIG" > "$AUTHZ_CONFIG.tmp" && mv "$AUTHZ_CONFIG.tmp" "$AUTHZ_CONFIG"
        if [ -n "$OPERATOR_ADDR" ]; then
            jq ".operator.address = \"$OPERATOR_ADDR\"" "$AUTHZ_CONFIG" > "$AUTHZ_CONFIG.tmp" && mv "$AUTHZ_CONFIG.tmp" "$AUTHZ_CONFIG"
        fi
        
        echo "✅ Hot key generated: $HOT_KEY_ADDR"
        echo "✅ Operator key generated: $OPERATOR_ADDR"
        
        # Save addresses for easy access
        echo "$HOT_KEY_ADDR" > "$AUTHZ_KEYS_DIR/hot_key_address.txt"
        echo "$OPERATOR_ADDR" > "$AUTHZ_KEYS_DIR/operator_address.txt"
    fi
else
    echo "ℹ️  Hot key already exists, skipping generation"
fi

# Create keyring directory for persistent storage
mkdir -p "$HOME_DIR/keyring-test"

echo "✅ AuthZ setup completed for Universal Validator $UNIVERSAL_ID"
echo "📋 Config file: $AUTHZ_CONFIG"
echo "🔑 Keys directory: $AUTHZ_KEYS_DIR"