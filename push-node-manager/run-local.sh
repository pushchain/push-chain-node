#!/bin/bash
set -e

# Script to run the Push Chain local node

echo "Starting Push Chain Local Node..."
echo "Chain ID: localchain_9000-1"
echo "Tendermint Node ID: ecd5f2723f0cd1c78b0546a39f0607028548f288"

# Check if binary exists
if [ ! -f "./pchaind" ]; then
    echo "Error: pchaind binary not found in current directory!"
    echo "Please copy the binary to: ./pchaind"
    exit 1
fi

# Make binary executable
chmod +x ./pchaind

# Check if genesis file exists
if [ ! -f "./config/localchain-genesis.json" ]; then
    echo "Error: Genesis file not found at ./config/localchain-genesis.json"
    exit 1
fi

# Build the Docker image
echo "Building Docker image..."
docker-compose -f docker-compose.localchain.yml build

# Stop any existing container
echo "Stopping any existing container..."
docker-compose -f docker-compose.localchain.yml down

# Start the node
echo "Starting the node..."
docker-compose -f docker-compose.localchain.yml up -d

# Show logs
echo "Showing logs (press Ctrl+C to stop viewing logs)..."
docker-compose -f docker-compose.localchain.yml logs -f