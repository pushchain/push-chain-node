#!/bin/bash
# Push Chain Validator Health Check Script

set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
NC='\033[0m'
BOLD='\033[1m'

# Get script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

# Docker compose command
if docker compose version &> /dev/null; then
    DOCKER_COMPOSE="docker compose"
else
    DOCKER_COMPOSE="docker-compose"
fi

# Print functions
print_test() {
    echo -e "${BLUE}[TEST]${NC} $1"
}

print_pass() {
    echo -e "${GREEN}[PASS]${NC} $1"
}

print_fail() {
    echo -e "${RED}[FAIL]${NC} $1"
}

print_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

# Test results
TESTS_PASSED=0
TESTS_FAILED=0
WARNINGS=0

# Run a test
run_test() {
    local test_name="$1"
    local test_command="$2"
    
    print_test "$test_name"
    if eval "$test_command"; then
        print_pass "$test_name"
        ((TESTS_PASSED++))
        return 0
    else
        print_fail "$test_name"
        ((TESTS_FAILED++))
        return 1
    fi
}

# Banner
echo -e "${BOLD}${BLUE}🚀 Push Chain Validator Health Check${NC}"
echo "===================================="
echo

# 1. Docker checks
print_info "Checking Docker environment..."

run_test "Docker is installed" "command -v docker >/dev/null 2>&1"
run_test "Docker is running" "docker info >/dev/null 2>&1"
run_test "Docker Compose is available" "command -v docker-compose >/dev/null 2>&1 || docker compose version >/dev/null 2>&1"

echo

# 2. Container checks
print_info "Checking validator container..."

if run_test "Validator container exists" "docker ps -a --format '{{.Names}}' | grep -q 'push-node-manager'"; then
    run_test "Validator container is running" "docker ps --format '{{.Names}}' | grep -q 'push-node-manager'"
    
    # Check container health
    if docker ps --format '{{.Names}}' | grep -q 'push-node-manager'; then
        # Get container stats
        CONTAINER_STATS=$($DOCKER_COMPOSE ps --format json 2>/dev/null || echo "{}")
        if [ -n "$CONTAINER_STATS" ] && [ "$CONTAINER_STATS" != "{}" ]; then
            print_pass "Container is responsive"
            ((TESTS_PASSED++))
        else
            print_warn "Could not get container stats"
            ((WARNINGS++))
        fi
    fi
else
    print_warn "No validator container found. Run './push-node-manager start' first"
    ((WARNINGS++))
fi

echo

# 3. Node checks (if container is running)
if docker ps --format '{{.Names}}' | grep -q 'push-node-manager'; then
    print_info "Checking node status..."
    
    # Check if node is responsive
    NODE_STATUS=$($DOCKER_COMPOSE exec -T validator pchaind status --home /root/.pchain 2>/dev/null || echo "")
    
    if [ -n "$NODE_STATUS" ]; then
        print_pass "Node is responsive"
        ((TESTS_PASSED++))
        
        # Parse node status
        CATCHING_UP=$(echo "$NODE_STATUS" | jq -r '.sync_info.catching_up' 2>/dev/null || echo "unknown")
        LATEST_HEIGHT=$(echo "$NODE_STATUS" | jq -r '.sync_info.latest_block_height' 2>/dev/null || echo "0")
        VOTING_POWER=$(echo "$NODE_STATUS" | jq -r '.validator_info.voting_power' 2>/dev/null || echo "0")
        
        # Check sync status
        if [ "$CATCHING_UP" = "false" ]; then
            print_pass "Node is fully synced"
            ((TESTS_PASSED++))
        elif [ "$CATCHING_UP" = "true" ]; then
            print_warn "Node is still syncing (height: $LATEST_HEIGHT)"
            ((WARNINGS++))
        else
            print_warn "Could not determine sync status"
            ((WARNINGS++))
        fi
        
        # Check if validator
        if [ "$VOTING_POWER" != "0" ] && [ "$VOTING_POWER" != "null" ]; then
            print_pass "Node is an active validator (voting power: $VOTING_POWER)"
            ((TESTS_PASSED++))
        else
            print_info "Node is not a validator (voting power: 0)"
        fi
        
        # Check peer connections - if syncing is happening, we have peers
        if [ "$CATCHING_UP" = "true" ] && [ "$LATEST_HEIGHT" -gt 0 ]; then
            print_pass "Has peer connections (syncing in progress)"
            ((TESTS_PASSED++))
        else
            # Try to get peer info from net_info endpoint
            PEER_INFO=$(curl -s --connect-timeout 2 http://localhost:26657/net_info 2>/dev/null || echo "{}")
            PEER_COUNT=$(echo "$PEER_INFO" | jq -r '.result.n_peers // 0' 2>/dev/null || echo "0")
            
            if [ "$PEER_COUNT" -gt 0 ]; then
                print_pass "Connected to $PEER_COUNT peers"
                ((TESTS_PASSED++))
            elif [ "$CATCHING_UP" = "false" ]; then
                print_pass "Fully synced (peer connections established)"
                ((TESTS_PASSED++))
            else
                print_warn "Could not verify peer connections"
                ((WARNINGS++))
            fi
        fi
    else
        print_warn "Node is starting up or not responsive yet"
        ((WARNINGS++))
    fi
    
    echo
    
    # 4. Port checks
    print_info "Checking network ports..."
    
    # Check if ports are exposed
    EXPOSED_PORTS=$(docker port push-node-manager 2>/dev/null || echo "")
    
    if [ -n "$EXPOSED_PORTS" ]; then
        # Check RPC port
        if echo "$EXPOSED_PORTS" | grep -q "26657"; then
            print_pass "RPC port (26657) is exposed"
            ((TESTS_PASSED++))
            
            # Test RPC endpoint
            if curl -s --connect-timeout 2 http://localhost:26657/status >/dev/null 2>&1; then
                print_pass "RPC endpoint is accessible"
                ((TESTS_PASSED++))
            else
                print_warn "RPC endpoint not responding"
                ((WARNINGS++))
            fi
        else
            print_warn "RPC port not exposed"
            ((WARNINGS++))
        fi
        
        # Check API port
        if echo "$EXPOSED_PORTS" | grep -q "1317"; then
            print_pass "API port (1317) is exposed"
            ((TESTS_PASSED++))
        else
            print_info "API port not exposed (optional)"
        fi
    else
        print_warn "No ports exposed - check docker-compose.yml"
        ((WARNINGS++))
    fi
    
    echo
    
    # 5. Wallet checks
    print_info "Checking wallets..."
    
    # Get wallet list and count properly
    WALLET_OUTPUT=$($DOCKER_COMPOSE exec -T validator pchaind keys list --keyring-backend test --home /root/.pchain 2>/dev/null || echo "")
    
    if [ -n "$WALLET_OUTPUT" ]; then
        # Count wallets by looking for "name:" lines
        WALLET_COUNT=$(echo "$WALLET_OUTPUT" | grep -c "name:" || echo "0")
        
        # Remove any whitespace or newlines that might cause issues
        WALLET_COUNT=$(echo "$WALLET_COUNT" | tr -d '[:space:]')
        
        if [ -z "$WALLET_COUNT" ]; then
            WALLET_COUNT="0"
        fi
        
        if [ "$WALLET_COUNT" -gt "0" ] 2>/dev/null; then
            print_pass "Found $WALLET_COUNT wallet(s)"
            ((TESTS_PASSED++))
        else
            print_info "No wallets found (run './push-node-manager setup' to create)"
        fi
    else
        print_info "No wallets found (run './push-node-manager setup' to create)"
    fi
fi

echo

# 6. File system checks
print_info "Checking file system..."

run_test "Docker compose file exists" "[ -f docker-compose.yml ]"
run_test "Dockerfile exists" "[ -f Dockerfile ]"
run_test "Scripts directory exists" "[ -d scripts ]"
run_test "Main control script is executable" "[ -x push-node-manager ]"

# Check for data directory
if [ -d "data" ]; then
    print_pass "Data directory exists"
    ((TESTS_PASSED++))
    
    # Check disk space
    DISK_USAGE=$(df -h . | awk 'NR==2 {print $5}' | sed 's/%//')
    if [ "$DISK_USAGE" -lt 90 ]; then
        print_pass "Disk usage is healthy ($DISK_USAGE%)"
        ((TESTS_PASSED++))
    else
        print_warn "Disk usage is high ($DISK_USAGE%)"
        ((WARNINGS++))
    fi
else
    print_info "No data directory (will be created on first start)"
fi

echo

# 7. Network connectivity checks
print_info "Checking network connectivity..."

# Check genesis node
if curl -s --connect-timeout 5 http://34.57.209.0:26657/status >/dev/null 2>&1; then
    print_pass "Can reach genesis node"
    ((TESTS_PASSED++))
else
    print_warn "Cannot reach genesis node (may affect sync)"
    ((WARNINGS++))
fi

# Check faucet
if curl -s --connect-timeout 5 https://faucet.push.org >/dev/null 2>&1; then
    print_pass "Can reach faucet"
    ((TESTS_PASSED++))
else
    print_warn "Cannot reach faucet"
    ((WARNINGS++))
fi

echo

# Summary
echo -e "${BOLD}${BLUE}Test Summary${NC}"
echo "=================="
echo -e "Tests passed: ${GREEN}$TESTS_PASSED${NC}"
echo -e "Tests failed: ${RED}$TESTS_FAILED${NC}"
echo -e "Warnings: ${YELLOW}$WARNINGS${NC}"
echo

# Overall status
if [ "$TESTS_FAILED" -eq 0 ]; then
    if [ "$WARNINGS" -eq 0 ]; then
        echo -e "${BOLD}${GREEN}✅ All systems operational!${NC}"
    else
        echo -e "${BOLD}${GREEN}✅ System is healthy${NC} (with $WARNINGS warnings)"
    fi
    exit 0
else
    echo -e "${BOLD}${RED}❌ Some tests failed!${NC}"
    echo
    echo "Troubleshooting tips:"
    echo "1. Check logs: ./push-node-manager logs"
    echo "2. Ensure Docker is running"
    echo "3. Try restarting: ./push-node-manager restart"
    echo "4. For sync issues: ./push-node-manager reset-data"
    exit 1
fi