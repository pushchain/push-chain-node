#!/bin/bash

###############################################
# Push Node Manager Command Test Suite
#
# Comprehensive testing of all push-node-manager commands
# Tests functionality, error handling, and edge cases
###############################################

set -euo pipefail

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BLUE='\033[1;94m'
MAGENTA='\033[0;35m'
NC='\033[0m'
BOLD='\033[1m'

# Print functions
print_status() { echo -e "${CYAN}$1${NC}"; }
print_header() { echo -e "${BLUE}$1${NC}"; }
print_success() { echo -e "${GREEN}$1${NC}"; }
print_error() { echo -e "${RED}$1${NC}"; }
print_warning() { echo -e "${YELLOW}$1${NC}"; }

# Test configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PUSH_NODE_MANAGER="$SCRIPT_DIR/../push-node-manager"
TEST_RESULTS_DIR="./test-results"
TIMESTAMP=$(date +"%Y-%m-%d_%H-%M-%S")
LOG_FILE="$TEST_RESULTS_DIR/command-test-$TIMESTAMP.log"

# Test counter
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Create test results directory
mkdir -p "$TEST_RESULTS_DIR"

# Logging functions
log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') $1" | tee -a "$LOG_FILE"
}

log_test_result() {
    local test_name="$1"
    local result="$2"
    local details="${3:-}"
    
    ((TESTS_RUN++))
    
    if [ "$result" = "PASS" ]; then
        ((TESTS_PASSED++))
        print_success "  ✅ $test_name"
        [ -n "$details" ] && print_status "     $details"
        log "TEST PASS: $test_name - $details"
    else
        ((TESTS_FAILED++))
        print_error "  ❌ $test_name"
        [ -n "$details" ] && print_status "     $details"
        log "TEST FAIL: $test_name - $details"
    fi
}

# Test helper function
run_command_test() {
    local test_name="$1"
    local command="$2"
    local expected_exit_code="${3:-0}"
    local should_contain="${4:-}"
    local should_not_contain="${5:-}"
    
    print_status "🧪 Testing: $test_name"
    
    local output
    local exit_code
    
    # Capture both output and exit code
    set +e
    output=$($command 2>&1)
    exit_code=$?
    set -e
    
    # Check exit code
    if [ "$exit_code" -eq "$expected_exit_code" ]; then
        local exit_result="PASS"
    else
        local exit_result="FAIL"
        log_test_result "$test_name (exit code)" "FAIL" "Expected $expected_exit_code, got $exit_code"
        return 1
    fi
    
    # Check output content if specified
    local content_result="PASS"
    local content_details=""
    
    if [ -n "$should_contain" ]; then
        if echo "$output" | grep -q "$should_contain"; then
            content_details="Contains expected text: $should_contain"
        else
            content_result="FAIL"
            content_details="Missing expected text: $should_contain"
        fi
    fi
    
    if [ -n "$should_not_contain" ] && [ "$content_result" = "PASS" ]; then
        if echo "$output" | grep -q "$should_not_contain"; then
            content_result="FAIL"
            content_details="Contains unexpected text: $should_not_contain"
        else
            content_details="$content_details, Correctly excludes: $should_not_contain"
        fi
    fi
    
    # Overall result
    if [ "$exit_result" = "PASS" ] && [ "$content_result" = "PASS" ]; then
        log_test_result "$test_name" "PASS" "$content_details"
    else
        log_test_result "$test_name" "FAIL" "$content_details"
    fi
}

# Test essential commands
test_essential_commands() {
    print_header "⚙️ Testing Essential Commands"
    
    # Test help command
    run_command_test "Help command" \
        "$PUSH_NODE_MANAGER help" \
        0 \
        "Node Management" \
        ""
    
    # Test help with no arguments (should show help)
    run_command_test "No arguments shows help" \
        "$PUSH_NODE_MANAGER" \
        0 \
        "Node Management" \
        ""
    
    # Test status command (should work even if node not running)
    run_command_test "Status command" \
        "$PUSH_NODE_MANAGER status" \
        0 \
        "" \
        ""
    
    # Test validators command (may fail without network, but shouldn't crash)
    run_command_test "Validators command structure" \
        "$PUSH_NODE_MANAGER validators" \
        0 \
        "" \
        ""
    
    # Test balance command (may fail without wallet, but shouldn't crash)
    run_command_test "Balance command structure" \
        "$PUSH_NODE_MANAGER balance" \
        0 \
        "" \
        ""
    
    echo
}

# Test public setup commands
test_public_setup_commands() {
    print_header "🌍 Testing Public Setup Commands"
    
    # Test setup-nginx without domain (should fail with error message)
    run_command_test "Setup-nginx without domain" \
        "$PUSH_NODE_MANAGER setup-nginx" \
        1 \
        "Domain required" \
        ""
    
    # Test setup-nginx with invalid domain format
    run_command_test "Setup-nginx with domain" \
        "$PUSH_NODE_MANAGER setup-nginx test.example.com" \
        0 \
        "" \
        ""
    
    # Test setup-logs command (should handle OS detection)
    run_command_test "Setup-logs command" \
        "$PUSH_NODE_MANAGER setup-logs" \
        0 \
        "Setting up log rotation" \
        ""
    
    # Test backup command with cancel
    run_command_test "Backup command (cancelled)" \
        "echo 'n' | $PUSH_NODE_MANAGER backup" \
        0 \
        "Backup cancelled" \
        ""
    
    echo
}

# Test error handling
test_error_handling() {
    print_header "🚨 Testing Error Handling"
    
    # Test invalid command
    run_command_test "Invalid command" \
        "$PUSH_NODE_MANAGER invalid-command" \
        1 \
        "Unknown command" \
        ""
    
    # Test command with extra arguments
    run_command_test "Help with extra args" \
        "$PUSH_NODE_MANAGER help extra args" \
        0 \
        "Essential Commands" \
        ""
    
    # Test setup-nginx with too many arguments
    run_command_test "Setup-nginx with extra args" \
        "$PUSH_NODE_MANAGER setup-nginx domain.com extra arg" \
        0 \
        "" \
        ""
    
    echo
}

# Test command availability
test_command_availability() {
    print_header "📋 Testing Command Availability"
    
    # Get list of available commands from help
    local help_output
    help_output=$($PUSH_NODE_MANAGER help 2>/dev/null || echo "")
    
    # Essential commands that should always be available
    local essential_commands=(
        "start"
        "stop"
        "status"
        "restart"
        "logs"
        "sync"
        "validators"
        "balance"
        "reset"
        "help"
        "register-validator"
    )
    
    for cmd in "${essential_commands[@]}"; do
        if echo "$help_output" | grep -q "$cmd"; then
            log_test_result "Command '$cmd' listed in help" "PASS" "Available in help output"
        else
            log_test_result "Command '$cmd' listed in help" "FAIL" "Not found in help output"
        fi
    done
    
    # Public setup commands
    local setup_commands=(
        "setup-nginx"
        "setup-logs"
        "backup"
    )
    
    for cmd in "${setup_commands[@]}"; do
        if echo "$help_output" | grep -q "$cmd"; then
            log_test_result "Setup command '$cmd' listed" "PASS" "Available in help output"
        else
            log_test_result "Setup command '$cmd' listed" "FAIL" "Not found in help output"
        fi
    done
    
    echo
}

# Test script dependencies
test_script_dependencies() {
    print_header "📦 Testing Script Dependencies"
    
    # Check if all required scripts exist
    local required_scripts=(
        "scripts/setup-dependencies.sh"
        "scripts/register-validator.sh"
        "scripts/setup-nginx.sh"
        "scripts/setup-log-rotation.sh"
        "scripts/backup.sh"
    )
    
    local script_base_dir
    script_base_dir="$(dirname "$PUSH_NODE_MANAGER")"
    
    for script in "${required_scripts[@]}"; do
        local script_path="$script_base_dir/$script"
        if [ -f "$script_path" ] && [ -x "$script_path" ]; then
            log_test_result "Script $(basename "$script") exists" "PASS" "Found and executable"
        else
            log_test_result "Script $(basename "$script") exists" "FAIL" "Missing or not executable"
        fi
    done
    
    echo
}

# Test binary dependencies
test_binary_dependencies() {
    print_header "🔧 Testing Binary Dependencies"
    
    local binary_path
    binary_path="$(dirname "$PUSH_NODE_MANAGER")/build/pchaind"
    
    if [ -f "$binary_path" ]; then
        log_test_result "pchaind binary exists" "PASS" "Found at $binary_path"
        
        # Test binary execution
        if [ -x "$binary_path" ]; then
            log_test_result "pchaind binary executable" "PASS" "Has execute permissions"
            
            # Test basic binary functionality
            if "$binary_path" version >/dev/null 2>&1; then
                log_test_result "pchaind binary functional" "PASS" "Version command works"
            else
                log_test_result "pchaind binary functional" "FAIL" "Version command failed"
            fi
        else
            log_test_result "pchaind binary executable" "FAIL" "No execute permissions"
        fi
    else
        log_test_result "pchaind binary exists" "FAIL" "Binary not found"
    fi
    
    echo
}

# Test configuration files
test_configuration_files() {
    print_header "⚙️ Testing Configuration Files"
    
    # Check if node configuration exists (may not exist until first run)
    local config_dir="$HOME/.pchain/config"
    
    if [ -d "$config_dir" ]; then
        log_test_result "Node config directory exists" "PASS" "Found at $config_dir"
        
        # Check important config files
        local config_files=(
            "config.toml"
            "app.toml"
            "genesis.json"
        )
        
        for config_file in "${config_files[@]}"; do
            if [ -f "$config_dir/$config_file" ]; then
                log_test_result "Config file $config_file" "PASS" "Present"
            else
                log_test_result "Config file $config_file" "FAIL" "Missing (will be created on first run)"
            fi
        done
    else
        log_test_result "Node config directory exists" "PASS" "Will be created on first start"
    fi
    
    echo
}

# Test command syntax validation
test_command_syntax() {
    print_header "📝 Testing Command Syntax"
    
    # Test the main script syntax
    if bash -n "$PUSH_NODE_MANAGER" 2>/dev/null; then
        log_test_result "Main script syntax" "PASS" "Valid bash syntax"
    else
        log_test_result "Main script syntax" "FAIL" "Syntax errors detected"
    fi
    
    # Test individual script syntax
    local script_base_dir
    script_base_dir="$(dirname "$PUSH_NODE_MANAGER")"
    
    local scripts_to_check=(
        "scripts/setup-dependencies.sh"
        "scripts/register-validator.sh"
        "scripts/setup-nginx.sh"
        "scripts/setup-log-rotation.sh"
        "scripts/backup.sh"
        "scripts/validate-installation.sh"
    )
    
    for script in "${scripts_to_check[@]}"; do
        local script_path="$script_base_dir/$script"
        if [ -f "$script_path" ]; then
            if bash -n "$script_path" 2>/dev/null; then
                log_test_result "$(basename "$script") syntax" "PASS" "Valid bash syntax"
            else
                log_test_result "$(basename "$script") syntax" "FAIL" "Syntax errors detected"
            fi
        fi
    done
    
    echo
}

# Print test summary
print_test_summary() {
    print_header "📊 Command Test Summary"
    echo
    print_status "Total Tests: $TESTS_RUN"
    print_success "Passed: $TESTS_PASSED"
    print_error "Failed: $TESTS_FAILED"
    
    local success_rate=0
    if [ "$TESTS_RUN" -gt 0 ]; then
        success_rate=$((TESTS_PASSED * 100 / TESTS_RUN))
    fi
    
    print_status "Success Rate: ${success_rate}%"
    echo
    
    if [ "$TESTS_FAILED" -eq 0 ]; then
        print_success "🎉 All command tests passed! Push Node Manager commands are working correctly."
    else
        print_error "⚠️  Some command tests failed. Check the log file for details: $LOG_FILE"
    fi
    
    print_status "Detailed log: $LOG_FILE"
}

# Main execution
main() {
    print_header "🚀 Push Node Manager Command Test Suite"
    print_status "Testing script: $PUSH_NODE_MANAGER"
    print_status "Started at: $(date)"
    print_status "Log file: $LOG_FILE"
    echo
    
    log "Starting Push Node Manager command test suite"
    log "Script path: $PUSH_NODE_MANAGER"
    
    # Check if push-node-manager script exists
    if [ ! -f "$PUSH_NODE_MANAGER" ]; then
        print_error "❌ Push Node Manager script not found: $PUSH_NODE_MANAGER"
        print_status "Make sure you're running this from the correct directory or install Push Node Manager first."
        exit 1
    fi
    
    # Run all test suites
    test_essential_commands
    test_public_setup_commands
    test_error_handling
    test_command_availability
    test_script_dependencies
    test_binary_dependencies
    test_configuration_files
    test_command_syntax
    
    print_test_summary
    
    # Return appropriate exit code
    if [ "$TESTS_FAILED" -eq 0 ]; then
        exit 0
    else
        exit 1
    fi
}

# Handle script arguments
case "${1:-}" in
    --help|-h)
        echo "Usage: $0 [--help]"
        echo ""
        echo "This script tests all Push Node Manager commands for functionality and correctness."
        echo ""
        echo "The script should be run from the push-node-manager directory."
        exit 0
        ;;
    *)
        main "$@"
        ;;
esac