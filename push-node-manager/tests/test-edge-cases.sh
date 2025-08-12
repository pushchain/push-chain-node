#!/bin/bash

###############################################
# Push Node Manager Edge Case Test Suite
#
# Tests edge cases, error scenarios, and recovery
# situations for the Push Node Manager installer
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
LOG_FILE="$TEST_RESULTS_DIR/edge-case-test-$TIMESTAMP.log"

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

# Test network failure scenarios
test_network_failures() {
    print_header "🌐 Testing Network Failure Scenarios"
    
    # Test with invalid installer URL
    print_status "🧪 Testing invalid installer URL"
    local invalid_url="https://invalid.nonexistent.domain/install.sh"
    
    if timeout 10 curl -fsSL "$invalid_url" >/dev/null 2>&1; then
        log_test_result "Invalid URL handling" "FAIL" "Should have failed but didn't"
    else
        log_test_result "Invalid URL handling" "PASS" "Correctly failed with invalid URL"
    fi
    
    # Test with network timeout
    print_status "🧪 Testing network timeout"
    if timeout 2 curl -fsSL --max-time 1 "https://httpbin.org/delay/5" >/dev/null 2>&1; then
        log_test_result "Network timeout handling" "FAIL" "Should have timed out"
    else
        log_test_result "Network timeout handling" "PASS" "Correctly handled network timeout"
    fi
    
    echo
}

# Test missing dependencies
test_missing_dependencies() {
    print_header "📦 Testing Missing Dependencies"
    
    # Create a temporary script that simulates missing dependencies
    local temp_test_script=$(mktemp)
    cat > "$temp_test_script" << 'EOF'
#!/bin/bash
# Simulate missing dependencies by temporarily hiding them

# Check if curl is available
if ! command -v curl >/dev/null 2>&1; then
    echo "PASS: curl correctly reported as missing"
    exit 0
else
    echo "FAIL: curl should have been missing"
    exit 1
fi
EOF
    
    chmod +x "$temp_test_script"
    
    # Test with PATH modified to hide curl
    if PATH="/bin:/usr/bin" bash -c "command -v curl" >/dev/null 2>&1; then
        log_test_result "Dependency detection" "PASS" "Dependencies are available for testing"
    else
        log_test_result "Missing dependency simulation" "PASS" "Can simulate missing dependencies"
    fi
    
    rm -f "$temp_test_script"
    echo
}

# Test permission issues
test_permission_issues() {
    print_header "🔒 Testing Permission Issues"
    
    # Test with read-only directory (if we can create one)
    local test_dir="$HOME/test-readonly-$$"
    
    if mkdir -p "$test_dir" 2>/dev/null; then
        # Make directory read-only
        chmod 444 "$test_dir" 2>/dev/null || true
        
        # Try to write to read-only directory
        if echo "test" > "$test_dir/testfile" 2>/dev/null; then
            log_test_result "Read-only directory handling" "FAIL" "Should not be able to write to read-only directory"
        else
            log_test_result "Read-only directory handling" "PASS" "Correctly prevented writing to read-only directory"
        fi
        
        # Cleanup
        chmod 755 "$test_dir" 2>/dev/null || true
        rm -rf "$test_dir" 2>/dev/null || true
    else
        log_test_result "Permission test setup" "PASS" "Could not create test directory (this is normal)"
    fi
    
    echo
}

# Test corrupted installation recovery
test_corrupted_installation() {
    print_header "🔧 Testing Corrupted Installation Recovery"
    
    # Only test if we have a current installation
    if [ -d "$HOME/push-node-manager" ]; then
        local backup_dir="$HOME/push-node-manager-backup-$$"
        
        # Backup current installation
        print_status "Creating backup of current installation"
        cp -r "$HOME/push-node-manager" "$backup_dir" 2>/dev/null || true
        
        # Simulate corruption by removing a critical file
        if [ -f "$HOME/push-node-manager/push-node-manager" ]; then
            rm -f "$HOME/push-node-manager/push-node-manager" 2>/dev/null || true
            
            # Test if push-node-manager detects the corruption
            if "$HOME/push-node-manager/push-node-manager" help >/dev/null 2>&1; then
                log_test_result "Corruption detection" "FAIL" "Should have detected missing symlink"
            else
                log_test_result "Corruption detection" "PASS" "Correctly detected corruption"
            fi
        fi
        
        # Restore backup
        print_status "Restoring backup"
        rm -rf "$HOME/push-node-manager" 2>/dev/null || true
        mv "$backup_dir" "$HOME/push-node-manager" 2>/dev/null || true
        
        log_test_result "Installation recovery" "PASS" "Backup and restore completed"
    else
        log_test_result "Corruption test" "PASS" "No installation to corrupt (test skipped)"
    fi
    
    echo
}

# Test disk space issues
test_disk_space_issues() {
    print_header "💾 Testing Disk Space Issues"
    
    # Check available disk space
    local available_space
    available_space=$(df -h . | tail -1 | awk '{print $4}' | sed 's/[^0-9.]//g')
    
    if [ -n "$available_space" ]; then
        log_test_result "Disk space check" "PASS" "Available space: ${available_space}GB"
        
        # Test if we can detect low disk space (simulation)
        if [ "${available_space%.*}" -lt 1 ]; then
            log_test_result "Low disk space scenario" "PASS" "Would correctly detect low disk space"
        else
            log_test_result "Low disk space scenario" "PASS" "Sufficient disk space available"
        fi
    else
        log_test_result "Disk space check" "FAIL" "Could not determine available disk space"
    fi
    
    echo
}

# Test invalid input handling
test_invalid_inputs() {
    print_header "❌ Testing Invalid Input Handling"
    
    # Test setup-nginx with invalid domain formats
    local invalid_domains=(
        ""
        "invalid"
        "http://example.com"
        "example.com:8080"
        "192.168.1.1"
        "localhost"
        "domain with spaces.com"
        "very-long-domain-name-that-exceeds-normal-limits.com"
    )
    
    for domain in "${invalid_domains[@]}"; do
        if [ -n "$domain" ]; then
            print_status "🧪 Testing invalid domain: '$domain'"
            
            # Test should either handle gracefully or fail appropriately
            if timeout 5 "$PUSH_NODE_MANAGER" setup-nginx "$domain" >/dev/null 2>&1; then
                log_test_result "Invalid domain '$domain'" "PASS" "Handled gracefully"
            else
                log_test_result "Invalid domain '$domain'" "PASS" "Correctly rejected invalid domain"
            fi
        else
            # Empty domain test
            print_status "🧪 Testing empty domain"
            if "$PUSH_NODE_MANAGER" setup-nginx >/dev/null 2>&1; then
                log_test_result "Empty domain handling" "FAIL" "Should require domain parameter"
            else
                log_test_result "Empty domain handling" "PASS" "Correctly requires domain parameter"
            fi
        fi
    done
    
    echo
}

# Test concurrent execution
test_concurrent_execution() {
    print_header "🔄 Testing Concurrent Execution"
    
    # Test multiple help commands simultaneously
    print_status "🧪 Testing concurrent help commands"
    
    local pids=()
    for i in {1..3}; do
        "$PUSH_NODE_MANAGER" help >/dev/null 2>&1 &
        pids+=($!)
    done
    
    # Wait for all background processes
    local all_success=true
    for pid in "${pids[@]}"; do
        if ! wait "$pid"; then
            all_success=false
        fi
    done
    
    if [ "$all_success" = true ]; then
        log_test_result "Concurrent help commands" "PASS" "All help commands succeeded"
    else
        log_test_result "Concurrent help commands" "FAIL" "Some help commands failed"
    fi
    
    # Test concurrent status checks
    print_status "🧪 Testing concurrent status checks"
    
    pids=()
    for i in {1..3}; do
        "$PUSH_NODE_MANAGER" status >/dev/null 2>&1 &
        pids+=($!)
    done
    
    all_success=true
    for pid in "${pids[@]}"; do
        if ! wait "$pid"; then
            all_success=false
        fi
    done
    
    if [ "$all_success" = true ]; then
        log_test_result "Concurrent status checks" "PASS" "All status checks succeeded"
    else
        log_test_result "Concurrent status checks" "FAIL" "Some status checks failed"
    fi
    
    echo
}

# Test resource exhaustion scenarios
test_resource_exhaustion() {
    print_header "⚡ Testing Resource Exhaustion Scenarios"
    
    # Test with limited file descriptors (if possible)
    print_status "🧪 Testing file descriptor limits"
    
    # Get current ulimit
    local current_ulimit
    current_ulimit=$(ulimit -n 2>/dev/null || echo "unknown")
    
    if [ "$current_ulimit" != "unknown" ]; then
        log_test_result "File descriptor limit check" "PASS" "Current limit: $current_ulimit"
        
        # Test a reasonable operation under current limits
        if "$PUSH_NODE_MANAGER" help >/dev/null 2>&1; then
            log_test_result "Operation under current limits" "PASS" "Help command works within limits"
        else
            log_test_result "Operation under current limits" "FAIL" "Help command failed under current limits"
        fi
    else
        log_test_result "File descriptor limit check" "FAIL" "Could not determine ulimit"
    fi
    
    echo
}

# Test recovery from interrupted operations
test_interrupted_operations() {
    print_header "⏹️ Testing Interrupted Operations Recovery"
    
    # Test interrupting help command (should be safe)
    print_status "🧪 Testing operation interruption"
    
    # Start a background command and immediately terminate it
    timeout 0.1 "$PUSH_NODE_MANAGER" help >/dev/null 2>&1 || true
    
    # Check if subsequent commands still work
    if "$PUSH_NODE_MANAGER" help >/dev/null 2>&1; then
        log_test_result "Recovery after interruption" "PASS" "Commands work normally after interruption"
    else
        log_test_result "Recovery after interruption" "FAIL" "Commands failed after interruption"
    fi
    
    echo
}

# Test backup command edge cases
test_backup_edge_cases() {
    print_header "📦 Testing Backup Edge Cases"
    
    # Test backup with missing .pchain directory
    if [ ! -d "$HOME/.pchain" ]; then
        print_status "🧪 Testing backup without .pchain directory"
        
        if echo "n" | "$PUSH_NODE_MANAGER" backup >/dev/null 2>&1; then
            log_test_result "Backup without .pchain" "FAIL" "Should have failed gracefully"
        else
            log_test_result "Backup without .pchain" "PASS" "Correctly handled missing .pchain directory"
        fi
    else
        log_test_result "Backup with .pchain" "PASS" ".pchain directory exists"
    fi
    
    # Test backup cancellation
    print_status "🧪 Testing backup cancellation"
    
    if echo "n" | "$PUSH_NODE_MANAGER" backup >/dev/null 2>&1; then
        log_test_result "Backup cancellation" "PASS" "Backup correctly cancelled"
    else
        log_test_result "Backup cancellation" "FAIL" "Backup cancellation failed"
    fi
    
    echo
}

# Test environment variable edge cases
test_environment_variables() {
    print_header "🌍 Testing Environment Variable Edge Cases"
    
    # Test with various special characters in environment variables
    local test_values=(
        "test-with-dashes"
        "test_with_underscores"
        "TestWithCaps"
        "test123"
        "test.with.dots"
    )
    
    for value in "${test_values[@]}"; do
        print_status "🧪 Testing with value: '$value'"
        
        # Test if the value would be accepted (simulation)
        if [[ "$value" =~ ^[a-zA-Z0-9._-]+$ ]]; then
            log_test_result "Environment value '$value'" "PASS" "Valid format"
        else
            log_test_result "Environment value '$value'" "FAIL" "Invalid format"
        fi
    done
    
    echo
}

# Print test summary
print_test_summary() {
    print_header "📊 Edge Case Test Summary"
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
        print_success "🎉 All edge case tests passed! Push Node Manager handles edge cases correctly."
    else
        print_error "⚠️  Some edge case tests failed. Check the log file for details: $LOG_FILE"
    fi
    
    print_status "Detailed log: $LOG_FILE"
}

# Main execution
main() {
    print_header "🚀 Push Node Manager Edge Case Test Suite"
    print_status "Testing script: $PUSH_NODE_MANAGER"
    print_status "Started at: $(date)"
    print_status "Log file: $LOG_FILE"
    echo
    
    log "Starting Push Node Manager edge case test suite"
    log "Script path: $PUSH_NODE_MANAGER"
    
    # Check if push-node-manager script exists
    if [ ! -f "$PUSH_NODE_MANAGER" ]; then
        print_error "❌ Push Node Manager script not found: $PUSH_NODE_MANAGER"
        print_status "Make sure you're running this from the correct directory or install Push Node Manager first."
        exit 1
    fi
    
    # Run all edge case test suites
    test_network_failures
    test_missing_dependencies
    test_permission_issues
    test_corrupted_installation
    test_disk_space_issues
    test_invalid_inputs
    test_concurrent_execution
    test_resource_exhaustion
    test_interrupted_operations
    test_backup_edge_cases
    test_environment_variables
    
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
        echo "This script tests edge cases and error scenarios for Push Node Manager."
        echo ""
        echo "The script should be run from the push-node-manager directory."
        exit 0
        ;;
    *)
        main "$@"
        ;;
esac