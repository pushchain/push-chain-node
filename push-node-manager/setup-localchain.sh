#!/bin/bash
set -e

echo "🚀 Push Chain Local Node Setup"
echo "================================"

# Check if pchaind binary exists
if [ ! -f "./pchaind" ]; then
    echo "❌ Error: pchaind binary not found in current directory!"
    echo "Please ensure the binary is present at: ./pchaind"
    exit 1
fi

# Check if genesis file exists
if [ ! -f "./config/localchain-genesis.json" ]; then
    echo "❌ Error: Genesis file not found at ./config/localchain-genesis.json"
    exit 1
fi

# Make binary executable
chmod +x ./pchaind

echo "✅ Binary and genesis file verified"

# Build the Docker image
echo "🔨 Building Docker image for localchain..."
docker build -f Dockerfile.localchain -t push-chain-node:localchain .

echo "✅ Docker image built successfully"

echo ""
echo "🎉 Setup complete! You can now run the local node with:"
echo "   ./run-local.sh"
echo ""
echo "📊 Local Node Information:"
echo "   Chain ID: localchain_9000-1"
echo "   Tendermint Node ID: ecd5f2723f0cd1c78b0546a39f0607028548f288"
echo "   Moniker: localvalidator"
echo ""
echo "🌐 Endpoints will be available at:"
echo "   RPC:       http://localhost:26657"
echo "   REST API:  http://localhost:1317"
echo "   JSON-RPC:  http://localhost:8545"
echo "   gRPC:      localhost:9090"