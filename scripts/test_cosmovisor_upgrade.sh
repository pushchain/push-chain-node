#!/bin/bash
# Test Cosmovisor Upgrade Flow Locally (macOS)
#
# This script tests the complete Cosmovisor auto-upgrade flow:
# 1. Sets up Cosmovisor with a "genesis" binary (v0.0.20-test)
# 2. Starts a local test chain
# 3. Schedules an upgrade to v0.0.21-test
# 4. Verifies Cosmovisor auto-downloads and applies the upgrade
#
# Usage:
#   ./scripts/test_cosmovisor_upgrade.sh
#
# Prerequisites:
#   - Go installed (for cosmovisor)
#   - curl, jq installed

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# Configuration
GENESIS_VERSION="v0.0.19-test"
UPGRADE_VERSION="v0.0.22-test"
CHAIN_ID="cosmovisor-test-1"
DAEMON_NAME="pchaind"
DAEMON_HOME="$HOME/.pchain-cosmovisor-test"
GITHUB_REPO="pushchain/push-chain-node"

# Export for Cosmovisor
export DAEMON_NAME
export DAEMON_HOME
export DAEMON_ALLOW_DOWNLOAD_BINARIES=true
export DAEMON_RESTART_AFTER_UPGRADE=true
export UNSAFE_SKIP_BACKUP=true

cleanup() {
    log_info "Cleaning up..."
    # Kill any running pchaind processes
    pkill -f "pchaind.*--home.*$DAEMON_HOME" 2>/dev/null || true
    pkill -f "cosmovisor.*$DAEMON_HOME" 2>/dev/null || true
    sleep 2
}

trap cleanup EXIT

# ============================================
# Step 0: Install Cosmovisor if needed
# ============================================
install_cosmovisor() {
    log_info "Checking Cosmovisor installation..."

    if ! command -v cosmovisor &> /dev/null; then
        log_info "Installing Cosmovisor..."
        go install cosmossdk.io/tools/cosmovisor/cmd/cosmovisor@latest
    fi

    cosmovisor version 2>/dev/null || log_warn "Cosmovisor installed (version check may not work)"
    log_success "Cosmovisor ready"
}

# ============================================
# Step 1: Setup Directory Structure
# ============================================
setup_directories() {
    log_info "Setting up directory structure at $DAEMON_HOME..."

    # Clean previous test
    rm -rf "$DAEMON_HOME"

    # Create Cosmovisor structure
    mkdir -p "$DAEMON_HOME/cosmovisor/genesis/bin"
    mkdir -p "$DAEMON_HOME/cosmovisor/upgrades"
    mkdir -p "$DAEMON_HOME/config"
    mkdir -p "$DAEMON_HOME/data"

    log_success "Directory structure created"
}

# ============================================
# Step 2: Download Genesis Binary
# ============================================
download_genesis_binary() {
    log_info "Downloading genesis binary ($GENESIS_VERSION)..."

    local VERSION_NO_V="${GENESIS_VERSION#v}"
    local ARCHIVE_URL="https://github.com/$GITHUB_REPO/releases/download/$GENESIS_VERSION/push-chain_${VERSION_NO_V}_darwin_arm64.tar.gz"

    cd "$DAEMON_HOME/cosmovisor/genesis/bin"

    log_info "Downloading from: $ARCHIVE_URL"
    curl -L -o push-chain.tar.gz "$ARCHIVE_URL"

    tar -xzf push-chain.tar.gz

    # Rename binary to pchaind
    if [ -f "pchaind-darwin-arm64" ]; then
        mv pchaind-darwin-arm64 pchaind
    fi

    chmod +x pchaind
    rm -f push-chain.tar.gz

    # Handle libwasmvm.dylib - MUST install to /usr/local/lib for upgrades to work
    if [ -f "libwasmvm.dylib" ]; then
        log_info "Installing libwasmvm.dylib to /usr/local/lib/ (required for Cosmovisor upgrades)..."
        if sudo cp libwasmvm.dylib /usr/local/lib/ 2>/dev/null; then
            log_success "libwasmvm.dylib installed to /usr/local/lib/"
        else
            log_warn "Could not install to /usr/local/lib (need sudo), trying alternative..."
            # Create a symlink in a system path or use DYLD_LIBRARY_PATH
            mkdir -p "$HOME/lib"
            cp libwasmvm.dylib "$HOME/lib/"
            export DYLD_LIBRARY_PATH="$HOME/lib:$DYLD_LIBRARY_PATH"
            log_info "Using DYLD_LIBRARY_PATH=$DYLD_LIBRARY_PATH"
        fi
    fi

    # Verify binary works
    ./pchaind version
    log_success "Genesis binary ready: $(./pchaind version 2>&1 | head -1)"
}

# ============================================
# Step 3: Initialize Chain
# ============================================
initialize_chain() {
    log_info "Initializing test chain..."

    local BINARY="$DAEMON_HOME/cosmovisor/genesis/bin/pchaind"
    local DENOM="upc"
    local GENESIS="$DAEMON_HOME/config/genesis.json"

    # Initialize with default-denom
    $BINARY init test-node --chain-id $CHAIN_ID --default-denom $DENOM --home $DAEMON_HOME 2>/dev/null

    # Update genesis parameters (matching test_node.sh)
    log_info "Updating genesis parameters..."

    # Gov
    jq --arg denom "$DENOM" '.app_state.gov.params.min_deposit=[{"denom":$denom,"amount":"1000000"}]' "$GENESIS" > "$GENESIS.tmp" && mv "$GENESIS.tmp" "$GENESIS"
    jq '.app_state.gov.params.voting_period="30s"' "$GENESIS" > "$GENESIS.tmp" && mv "$GENESIS.tmp" "$GENESIS"
    jq '.app_state.gov.params.expedited_voting_period="15s"' "$GENESIS" > "$GENESIS.tmp" && mv "$GENESIS.tmp" "$GENESIS"

    # EVM
    jq --arg denom "$DENOM" '.app_state.evm.params.evm_denom=$denom' "$GENESIS" > "$GENESIS.tmp" && mv "$GENESIS.tmp" "$GENESIS"

    # Staking
    jq --arg denom "$DENOM" '.app_state.staking.params.bond_denom=$denom' "$GENESIS" > "$GENESIS.tmp" && mv "$GENESIS.tmp" "$GENESIS"
    jq '.app_state.staking.params.min_commission_rate="0.050000000000000000"' "$GENESIS" > "$GENESIS.tmp" && mv "$GENESIS.tmp" "$GENESIS"

    # Mint
    jq --arg denom "$DENOM" '.app_state.mint.params.mint_denom=$denom' "$GENESIS" > "$GENESIS.tmp" && mv "$GENESIS.tmp" "$GENESIS"

    # Crisis
    jq --arg denom "$DENOM" '.app_state.crisis.constant_fee={"denom":$denom,"amount":"1000"}' "$GENESIS" > "$GENESIS.tmp" && mv "$GENESIS.tmp" "$GENESIS"

    # ABCI vote extensions
    jq '.consensus.params.abci.vote_extensions_enable_height="1"' "$GENESIS" > "$GENESIS.tmp" && mv "$GENESIS.tmp" "$GENESIS"

    # Create validator key
    $BINARY keys add validator --keyring-backend test --algo eth_secp256k1 --home $DAEMON_HOME 2>/dev/null

    # Get validator address
    VALIDATOR_ADDR=$($BINARY keys show validator -a --keyring-backend test --home $DAEMON_HOME)
    log_info "Validator address: $VALIDATOR_ADDR"

    # Add genesis account with lots of tokens (matching test_node.sh amounts)
    # 5 000000000 . 000000000 000000000
    $BINARY genesis add-genesis-account $VALIDATOR_ADDR 5000000000000000000000000000$DENOM --keyring-backend test --home $DAEMON_HOME

    # Create gentx with correct amount (matching test_node.sh)
    # 10 000 . 000000000 000000000
    $BINARY genesis gentx validator 10000000000000000000000$DENOM \
        --gas-prices 1000000000$DENOM \
        --keyring-backend test \
        --chain-id $CHAIN_ID \
        --home $DAEMON_HOME 2>/dev/null

    # Collect gentxs
    $BINARY genesis collect-gentxs --home $DAEMON_HOME 2>/dev/null

    # Validate genesis
    $BINARY genesis validate-genesis --home $DAEMON_HOME

    # Update config for fast blocks (1 second)
    sed -i '' 's/timeout_commit = "5s"/timeout_commit = "1s"/' "$DAEMON_HOME/config/config.toml" 2>/dev/null || true

    log_success "Chain initialized"
}

# ============================================
# Step 4: Calculate Upgrade Checksum
# ============================================
get_upgrade_checksum() {
    log_info "Calculating checksum for upgrade binary ($UPGRADE_VERSION)..."

    local VERSION_NO_V="${UPGRADE_VERSION#v}"
    local ARCHIVE_URL="https://github.com/$GITHUB_REPO/releases/download/$UPGRADE_VERSION/push-chain_${VERSION_NO_V}_darwin_arm64.tar.gz"

    # Download to temp and calculate checksum
    local TEMP_FILE=$(mktemp)
    curl -sL -o "$TEMP_FILE" "$ARCHIVE_URL"

    UPGRADE_CHECKSUM=$(shasum -a 256 "$TEMP_FILE" | cut -d' ' -f1)
    rm -f "$TEMP_FILE"

    log_success "Checksum: $UPGRADE_CHECKSUM"
}

# ============================================
# Step 5: Start Chain with Cosmovisor
# ============================================
start_chain() {
    log_info "Starting chain with Cosmovisor..."

    # Start in background
    cosmovisor run start --home $DAEMON_HOME > "$DAEMON_HOME/cosmovisor.log" 2>&1 &
    COSMOVISOR_PID=$!

    log_info "Cosmovisor PID: $COSMOVISOR_PID"
    log_info "Logs: $DAEMON_HOME/cosmovisor.log"

    # Wait for chain to start
    log_info "Waiting for chain to start..."
    sleep 5

    # Check if running
    for i in {1..30}; do
        if $DAEMON_HOME/cosmovisor/genesis/bin/pchaind status --home $DAEMON_HOME 2>/dev/null | jq -e '.sync_info.latest_block_height' > /dev/null 2>&1; then
            CURRENT_HEIGHT=$($DAEMON_HOME/cosmovisor/genesis/bin/pchaind status --home $DAEMON_HOME 2>&1 | jq -r '.sync_info.latest_block_height')
            log_success "Chain running at height: $CURRENT_HEIGHT"
            return 0
        fi
        sleep 1
    done

    log_error "Chain failed to start. Check logs: tail -f $DAEMON_HOME/cosmovisor.log"
    exit 1
}

# ============================================
# Step 6: Schedule Upgrade
# ============================================
schedule_upgrade() {
    log_info "Scheduling upgrade..."

    local BINARY="$DAEMON_HOME/cosmovisor/genesis/bin/pchaind"

    # Get current height
    CURRENT_HEIGHT=$($BINARY status --home $DAEMON_HOME 2>&1 | jq -r '.sync_info.latest_block_height')
    log_info "Current height: $CURRENT_HEIGHT"

    # Schedule upgrade 20 blocks from now
    UPGRADE_HEIGHT=$((CURRENT_HEIGHT + 20))
    log_info "Upgrade scheduled for height: $UPGRADE_HEIGHT"

    # Build upgrade info JSON
    local VERSION_NO_V="${UPGRADE_VERSION#v}"
    local ARCHIVE_URL="https://github.com/$GITHUB_REPO/releases/download/$UPGRADE_VERSION/push-chain_${VERSION_NO_V}_darwin_arm64.tar.gz"

    UPGRADE_INFO=$(cat << EOF
{"binaries":{"darwin/arm64":"${ARCHIVE_URL}?checksum=sha256:${UPGRADE_CHECKSUM}"}}
EOF
)

    log_info "Upgrade info: $UPGRADE_INFO"

    # For single-validator test, we can submit upgrade proposal and vote
    # Or directly create the upgrade-info.json (simulating post-governance)

    # Method: Direct upgrade-info.json (simulates governance passed)
    log_info "Creating upgrade-info.json (simulating governance approval)..."

    # The upgrade handler needs to be registered in the app
    # For testing auto-download, we'll trigger via software upgrade proposal

    # Submit upgrade proposal
    log_info "Submitting software upgrade proposal..."

    $BINARY tx upgrade software-upgrade "$UPGRADE_VERSION" \
        --title "Upgrade to $UPGRADE_VERSION" \
        --description "Test upgrade" \
        --upgrade-height $UPGRADE_HEIGHT \
        --upgrade-info "$UPGRADE_INFO" \
        --deposit 10000000upc \
        --from validator \
        --keyring-backend test \
        --chain-id $CHAIN_ID \
        --home $DAEMON_HOME \
        --fees 1000000upc \
        --yes \
        --broadcast-mode sync 2>/dev/null || {
            log_warn "Upgrade proposal submission failed, trying alternative method..."
            # Alternative: directly write upgrade-info.json
            create_upgrade_info_directly
            return
        }

    sleep 3

    # Vote on proposal
    log_info "Voting on upgrade proposal..."
    $BINARY tx gov vote 1 yes \
        --from validator \
        --keyring-backend test \
        --chain-id $CHAIN_ID \
        --home $DAEMON_HOME \
        --fees 1000000upc \
        --yes \
        --broadcast-mode sync 2>/dev/null || log_warn "Vote may have failed"

    log_success "Upgrade scheduled for height $UPGRADE_HEIGHT"
}

create_upgrade_info_directly() {
    log_info "Creating upgrade-info.json directly..."

    local VERSION_NO_V="${UPGRADE_VERSION#v}"
    local ARCHIVE_URL="https://github.com/$GITHUB_REPO/releases/download/$UPGRADE_VERSION/push-chain_${VERSION_NO_V}_darwin_arm64.tar.gz"

    # Get current height
    local BINARY="$DAEMON_HOME/cosmovisor/genesis/bin/pchaind"
    CURRENT_HEIGHT=$($BINARY status --home $DAEMON_HOME 2>&1 | jq -r '.sync_info.latest_block_height')
    UPGRADE_HEIGHT=$((CURRENT_HEIGHT + 15))

    cat > "$DAEMON_HOME/data/upgrade-info.json" << EOF
{
  "name": "$UPGRADE_VERSION",
  "height": $UPGRADE_HEIGHT,
  "info": "{\"binaries\":{\"darwin/arm64\":\"${ARCHIVE_URL}?checksum=sha256:${UPGRADE_CHECKSUM}\"}}"
}
EOF

    log_info "Created upgrade-info.json for height $UPGRADE_HEIGHT"
    cat "$DAEMON_HOME/data/upgrade-info.json"
}

# ============================================
# Step 7: Monitor Upgrade
# ============================================
monitor_upgrade() {
    log_info "Monitoring for upgrade at height $UPGRADE_HEIGHT..."
    log_info "Watching logs: tail -f $DAEMON_HOME/cosmovisor.log"

    local BINARY="$DAEMON_HOME/cosmovisor/genesis/bin/pchaind"

    while true; do
        # Get current height
        CURRENT=$($BINARY status --home $DAEMON_HOME 2>&1 | jq -r '.sync_info.latest_block_height' 2>/dev/null || echo "0")

        if [ "$CURRENT" = "0" ] || [ -z "$CURRENT" ]; then
            # Chain might be restarting
            log_info "Chain restarting..."
            sleep 2
            continue
        fi

        echo -ne "\r${BLUE}[INFO]${NC} Current height: $CURRENT / $UPGRADE_HEIGHT    "

        if [ "$CURRENT" -ge "$UPGRADE_HEIGHT" ]; then
            echo ""
            log_success "Upgrade height reached!"
            break
        fi

        sleep 1
    done

    # Wait for chain to restart with new binary
    log_info "Waiting for chain to restart with new binary..."
    sleep 10
}

# ============================================
# Step 8: Verify Upgrade
# ============================================
verify_upgrade() {
    log_info "Verifying upgrade..."

    # Check if upgrade directory was created
    if [ -d "$DAEMON_HOME/cosmovisor/upgrades/$UPGRADE_VERSION" ]; then
        log_success "Upgrade directory created: $DAEMON_HOME/cosmovisor/upgrades/$UPGRADE_VERSION"
        ls -la "$DAEMON_HOME/cosmovisor/upgrades/$UPGRADE_VERSION/bin/"
    else
        log_error "Upgrade directory not found!"
        log_info "Checking Cosmovisor logs for errors..."
        tail -50 "$DAEMON_HOME/cosmovisor.log"
        return 1
    fi

    # Check current symlink
    if [ -L "$DAEMON_HOME/cosmovisor/current" ]; then
        CURRENT_LINK=$(readlink "$DAEMON_HOME/cosmovisor/current")
        log_info "Current symlink points to: $CURRENT_LINK"
    fi

    # Try to get version from running binary
    sleep 5
    for i in {1..10}; do
        if $DAEMON_HOME/cosmovisor/current/bin/pchaind version 2>/dev/null; then
            NEW_VERSION=$($DAEMON_HOME/cosmovisor/current/bin/pchaind version 2>&1 | head -1)
            log_success "New binary version: $NEW_VERSION"
            break
        fi
        sleep 2
    done

    # Check chain is still running
    for i in {1..10}; do
        if $DAEMON_HOME/cosmovisor/current/bin/pchaind status --home $DAEMON_HOME 2>/dev/null | jq -e '.sync_info.latest_block_height' > /dev/null 2>&1; then
            FINAL_HEIGHT=$($DAEMON_HOME/cosmovisor/current/bin/pchaind status --home $DAEMON_HOME 2>&1 | jq -r '.sync_info.latest_block_height')
            log_success "Chain running after upgrade at height: $FINAL_HEIGHT"
            return 0
        fi
        sleep 2
    done

    log_error "Chain not running after upgrade"
    return 1
}

# ============================================
# Main
# ============================================
main() {
    echo "==========================================="
    echo "  Cosmovisor Upgrade Test"
    echo "  Genesis: $GENESIS_VERSION"
    echo "  Upgrade: $UPGRADE_VERSION"
    echo "==========================================="
    echo ""

    cleanup
    install_cosmovisor
    setup_directories
    download_genesis_binary
    initialize_chain
    get_upgrade_checksum
    start_chain
    schedule_upgrade
    monitor_upgrade
    verify_upgrade

    echo ""
    echo "==========================================="
    log_success "COSMOVISOR UPGRADE TEST COMPLETE!"
    echo "==========================================="
    echo ""
    echo "Summary:"
    echo "  - Genesis version: $GENESIS_VERSION"
    echo "  - Upgraded to: $UPGRADE_VERSION"
    echo "  - Upgrade height: $UPGRADE_HEIGHT"
    echo "  - Data directory: $DAEMON_HOME"
    echo ""
    echo "To view logs:"
    echo "  tail -f $DAEMON_HOME/cosmovisor.log"
    echo ""
    echo "To stop:"
    echo "  pkill -f cosmovisor"
}

# Run
main "$@"
