#!/bin/bash
set -e

echo "📊 Push Chain Multi-Validator Status"
echo "===================================="

cd "$(dirname "$0")"

# Check if containers are running
echo "🐳 Container Status:"
docker-compose ps

echo ""
echo "🔍 Core Validator Status:"
echo "------------------------"

# Function to check validator status
check_validator_status() {
    local name=$1
    local port=$2
    local validator_num=$3
    
    echo "[$validator_num] $name:"
    if curl -s "http://localhost:$port/status" > /dev/null 2>&1; then
        local status=$(curl -s "http://localhost:$port/status")
        local catching_up=$(echo "$status" | jq -r '.result.sync_info.catching_up')
        local latest_height=$(echo "$status" | jq -r '.result.sync_info.latest_block_height')
        local validator_info=$(echo "$status" | jq -r '.result.validator_info.address')
        
        if [ "$catching_up" = "false" ]; then
            echo "  ✅ Status: Synced | Height: $latest_height | Address: $validator_info"
        else
            echo "  🔄 Status: Syncing | Height: $latest_height"
        fi
    else
        echo "  ❌ Status: Not responding"
    fi
}

check_validator_status "Genesis Validator" 26657 1
check_validator_status "Validator 2" 26658 2
check_validator_status "Validator 3" 26659 3

echo ""
echo "🌍 Universal Validator Status:"
echo "-----------------------------"

# Function to check universal validator status
check_universal_status() {
    local name=$1
    local port=$2
    local validator_num=$3
    
    echo "[$validator_num] $name:"
    if curl -s "http://localhost:$port/status" > /dev/null 2>&1; then
        echo "  ✅ Status: Running | Port: $port"
    else
        echo "  ❌ Status: Not responding"
    fi
}

check_universal_status "Universal Validator 1" 8080 1
check_universal_status "Universal Validator 2" 8081 2
check_universal_status "Universal Validator 3" 8082 3

echo ""
echo "🔗 Network Connectivity:"
echo "------------------------"

# Check peer connections
echo "Checking peer connections..."
for port in 26657 26658 26659; do
    if curl -s "http://localhost:$port/net_info" > /dev/null 2>&1; then
        local peers=$(curl -s "http://localhost:$port/net_info" | jq -r '.result.n_peers')
        echo "  Validator on port $port: $peers peers connected"
    fi
done

echo ""
echo "📈 Latest Block Heights:"
echo "-----------------------"

for port in 26657 26658 26659; do
    if curl -s "http://localhost:$port/status" > /dev/null 2>&1; then
        local height=$(curl -s "http://localhost:$port/status" | jq -r '.result.sync_info.latest_block_height')
        echo "  Port $port: Block $height"
    fi
done

echo ""
echo "🎯 Quick Test Commands:"
echo "----------------------"
echo "# Test core validator RPC:"
echo "curl http://localhost:26657/status | jq '.result.sync_info'"
echo ""
echo "# Test universal validator API:"
echo "curl http://localhost:8080/status"
echo ""
echo "# View validator logs:"
echo "docker-compose logs -f core-validator-1"
echo ""
echo "# Access validator container:"
echo "docker-compose exec core-validator-1 sh"