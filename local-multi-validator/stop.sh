#!/bin/bash
set -e

echo "🛑 Stopping Push Chain Multi-Validator Local Setup"
echo "================================================="

# Navigate to the directory containing docker-compose.yml
cd "$(dirname "$0")"

# Check if docker-compose is available
if ! command -v docker-compose > /dev/null 2>&1; then
    echo "❌ docker-compose is not installed."
    exit 1
fi

echo "🛑 Stopping all containers..."
docker-compose down

echo "🧹 Cleaning up containers and networks..."
docker-compose down --remove-orphans

# Ask user if they want to remove volumes (data)
read -p "🗑️  Remove all validator data? (y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "🗑️ Removing all volumes and data..."
    docker-compose down -v --remove-orphans
    echo "✅ All data removed!"
else
    echo "💾 Data preserved. Validators will resume from last state on next start."
fi

echo ""
echo "✅ Multi-validator setup stopped successfully!"
echo ""
echo "🔧 Useful commands:"
echo "  - Restart setup:       ./start.sh"
echo "  - View remaining data: docker volume ls | grep local-multi-validator"
echo "  - Force clean all:     docker-compose down -v --remove-orphans"