#!/bin/bash
set -e

echo "🚀 Starting Push Chain Multi-Validator Local Setup"
echo "=================================================="

# Navigate to the directory containing docker-compose.yml
cd "$(dirname "$0")"

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo "❌ Docker is not running. Please start Docker first."
    exit 1
fi

# Check if docker-compose is available
if ! command -v docker-compose > /dev/null 2>&1; then
    echo "❌ docker-compose is not installed. Please install docker-compose first."
    exit 1
fi

# Check if images exist, build if needed
if ! docker images | grep -q "push-core.*latest" || ! docker images | grep -q "push-universal.*latest"; then
    echo "🔧 Building Docker images..."
    ./build.sh
else
    echo "✅ Docker images already exist, skipping build..."
fi

echo "🧹 Cleaning up any existing containers and volumes..."
docker-compose down -v --remove-orphans

echo "🎬 Starting validators in sequence..."

# Start genesis validator first
echo "1️⃣ Starting Genesis Validator..."
docker-compose up -d core-validator-1

# Wait for genesis validator to be healthy
echo "⏳ Waiting for genesis validator to initialize..."
timeout=120
while [ $timeout -gt 0 ]; do
    if docker-compose exec -T core-validator-1 curl -s http://localhost:26657/status > /dev/null 2>&1; then
        echo "✅ Genesis validator is ready!"
        break
    fi
    echo "Waiting... ($timeout seconds remaining)"
    sleep 5
    timeout=$((timeout - 5))
done

if [ $timeout -le 0 ]; then
    echo "❌ Genesis validator failed to start within timeout"
    docker-compose logs core-validator-1
    exit 1
fi

# Start additional core validators
echo "2️⃣ Starting Core Validator 2..."
docker-compose up -d core-validator-2

echo "3️⃣ Starting Core Validator 3..."
docker-compose up -d core-validator-3

# Wait for all core validators to sync
echo "⏳ Waiting for core validators to sync..."
sleep 30

# Start universal validators
echo "🌍 Starting Universal Validators..."
docker-compose up -d universal-validator-1
docker-compose up -d universal-validator-2
docker-compose up -d universal-validator-3

echo ""
echo "✅ Multi-validator setup complete!"
echo "=================================="
echo ""
echo "📊 Validator Endpoints:"
echo "Core Validator 1 (Genesis):"
echo "  - RPC:      http://localhost:26657"
echo "  - REST:     http://localhost:1317"
echo "  - GRPC:     localhost:9090"
echo "  - EVM HTTP: http://localhost:8545"
echo ""
echo "Core Validator 2:"
echo "  - RPC:      http://localhost:26658"
echo "  - REST:     http://localhost:1318"
echo "  - GRPC:     localhost:9093"
echo "  - EVM HTTP: http://localhost:8547"
echo ""
echo "Core Validator 3:"
echo "  - RPC:      http://localhost:26659"
echo "  - REST:     http://localhost:1319"
echo "  - GRPC:     localhost:9095"
echo "  - EVM HTTP: http://localhost:8549"
echo ""
echo "🌍 Universal Validator Endpoints:"
echo "  - Universal 1: http://localhost:8080"
echo "  - Universal 2: http://localhost:8081"
echo "  - Universal 3: http://localhost:8082"
echo ""
echo "🔍 Useful commands:"
echo "  - View logs:           docker-compose logs -f [service-name]"
echo "  - Check status:        docker-compose ps"
echo "  - Stop all:            ./stop.sh"
echo "  - Access container:    docker-compose exec [service-name] sh"
echo ""
echo "🎯 Test validator connectivity:"
echo "  curl http://localhost:26657/status"
echo "  curl http://localhost:26658/status"
echo "  curl http://localhost:26659/status"