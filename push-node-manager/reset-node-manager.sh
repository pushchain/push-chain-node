#!/bin/bash
# Script to reset validator for testing from scratch

# Check if called from push-node-manager script
if [ "$1" != "--skip-confirm" ]; then
    echo "This will completely reset your validator node!"
    echo "WARNING: This will delete:"
    echo "- All blockchain data"
    echo "- All wallets and keys"
    echo "- Node configuration"
    echo
    read -p "Are you sure you want to continue? (yes/no): " confirm

    if [[ "$confirm" != "yes" ]]; then
        echo "Reset cancelled."
        exit 0
    fi
fi

echo "Stopping validator..."
docker compose down

echo "Removing validator data volume..."
docker volume rm validator_node-manager-data 2>/dev/null || true

echo "Removing validator container and image..."
docker rm push-node-manager 2>/dev/null || true
docker rmi push-node-manager:latest 2>/dev/null || true

echo "Cleaning up any remaining data..."
rm -rf data/ 2>/dev/null || true

echo
echo "Reset complete! You can now start fresh with:"
echo "./push-node-manager setup"