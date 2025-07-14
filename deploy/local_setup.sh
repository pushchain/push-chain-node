#!/bin/bash

# Local deployment script for PUSH Chain validator node

set -e  # Exit on error

echo "Setting up local PUSH Chain validator node..."

# Step 1: Clean up existing directories
echo "Cleaning up existing directories..."
rm -rf ~/app ~/.pchain ~/.pchaind
mkdir -p ~/app ~/.pchaind

# Step 2: Build pchaind if not already built
if [ ! -f "build/pchaind" ]; then
    echo "Building pchaind..."
    make build
fi

# Step 3: Copy binaries and scripts
echo "Copying binaries and scripts..."
cp build/pchaind ~/app/pchaind

# Copy from deploy folder in the codebase
if [ -d "deploy/test-push-chain/scripts" ]; then
    cp -r deploy/test-push-chain/scripts/* ~/app/
else
    echo "Warning: deploy/test-push-chain/scripts not found"
fi

# Create config-tmp directory and copy config files
mkdir -p ~/app/config-tmp
if [ -d "deploy/test-push-chain/config" ]; then
    cp -r deploy/test-push-chain/config/* ~/app/config-tmp/
else
    echo "Warning: deploy/test-push-chain/config not found"
fi

# Copy toml_edit.py if it exists
if [ -f "deploy/test-push-chain/scripts/toml_edit.py" ]; then
    cp deploy/test-push-chain/scripts/toml_edit.py ~/app/toml_edit.py
elif [ -f "deploy/toml_edit.py" ]; then
    cp deploy/toml_edit.py ~/app/toml_edit.py
else
    echo "Warning: toml_edit.py not found"
fi

# Step 4: Set permissions
echo "Setting permissions..."
chmod u+x ~/app/pchaind
chmod u+x ~/app/*.sh

# Step 5: Create symlink
echo "Creating symlink..."
# Commenting out sudo commands - run these manually if needed:
# sudo rm -f /usr/local/bin/pchain
# sudo ln -s ~/app/pchaind /usr/local/bin/pchain
echo "Skipping symlink creation - run manually with:"
echo "  sudo rm -f /usr/local/bin/pchain"
echo "  sudo ln -s ~/app/pchaind /usr/local/bin/pchain"

# Step 6: Install Python dependencies
echo "Installing Python dependencies..."
pip3 install --break-system-packages --user tomlkit || pip install --break-system-packages --user tomlkit

# Step 7: Reset configs
echo "Resetting configurations..."
echo "DELETEALL" | ~/app/resetConfigs.sh

# Step 8: Configure node name
echo "Configuring node name..."
python3 ~/app/toml_edit.py \
  ~/.pchain/config/config.toml \
  "moniker" \
  "local-validator-node"

# Step 9: Copy genesis.json from root folder
echo "Copying genesis.json..."
if [ -f "genesis.json" ]; then
    cp genesis.json ~/.pchain/config/genesis.json
    echo "genesis.json copied successfully"
else
    echo "ERROR: genesis.json not found in root folder!"
    exit 1
fi

# Step 10: Configure persistent peers
echo "Configuring persistent peers..."
# Get node1's ID (you may need to adjust this)
export pn1_url="dc323e3c930d12369723373516267a213d74ea37@34.57.209.0:26656"

python3 ~/app/toml_edit.py \
  ~/.pchain/config/config.toml \
  "p2p.persistent_peers" \
  "$pn1_url"

# Step 11: Initialize validator state
echo "Initializing validator state..."
echo '{"height":"0","round":0,"step":0}' > ~/.pchain/data/priv_validator_state.json
chmod 600 ~/.pchain/config/priv_validator_key.json 2>/dev/null || true

# Step 12: Start the node
echo "Starting the node..."
~/app/stop.sh 2>/dev/null || true
~/app/start.sh

echo "Node started! Check logs with:"
echo "tail -f ~/app/chain.log"
echo ""
echo "Wait for sync with:"
echo "~/app/waitFullSync.sh"