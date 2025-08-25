#!/bin/bash
set -e

echo "🏗️  Building Push Chain Multi-Validator Images"
echo "=============================================="

# Navigate to the directory containing docker-compose.yml
cd "$(dirname "$0")"

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo "❌ Docker is not running. Please start Docker first."
    exit 1
fi

echo "🔨 Building base image first..."
docker build -f Dockerfile.base -t local-multi-validator-base:latest ..

echo "🔨 Building push-core and push-universal images..."
# Use docker build directly with --pull=false to use local images
docker build --pull=false -f Dockerfile.core -t push-core:latest ..
docker build --pull=false -f Dockerfile.universal -t push-universal:latest ..

echo "✅ All images built successfully!"
echo ""
echo "🚀 Ready to start validators with:"
echo "   ./start.sh"