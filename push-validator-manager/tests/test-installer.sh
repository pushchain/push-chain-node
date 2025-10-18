#!/bin/bash

###############################################
# Push Validator Manager Installer Test Suite
# 
# Comprehensive testing framework for the one-line installer
# Tests multiple environments, commands, and edge cases
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
INSTALLER_URL="https://get.push.network/node/install.sh"
TEST_RESULTS_DIR="./test-results"
TIMESTAMP=$(date +"%Y-%m-%d_%H-%M-%S")
LOG_FILE="$TEST_RESULTS_DIR/test-run-$TIMESTAMP.log"

# Test distributions with their package managers
DISTRIBUTIONS=(
    "ubuntu:22.04:apt"
    "ubuntu:20.04:apt"
    "debian:bookworm:apt"
    "debian:bullseye:apt"
    "rockylinux:9:dnf"
    "centos:7:yum"
    "fedora:38:dnf"
    "almalinux:9:dnf"
)

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
        print_success "✅ $test_name: PASS"
        log "TEST PASS: $test_name - $details"
    else
        ((TESTS_FAILED++))
        print_error "❌ $test_name: FAIL"
        log "TEST FAIL: $test_name - $details"
    fi
}

# Docker helper functions
run_in_container() {
    local image="$1"
    local command="$2"
    local container_name="pnm-test-$(echo "$image" | tr ':/' '-')-$$"
    
    docker run --rm --name "$container_name" \
        -v /var/run/docker.sock:/var/run/docker.sock \
        "$image" bash -c "$command" 2>&1
}

test_basic_installation() {
    local dist_info="$1"
    local image=$(echo "$dist_info" | cut -d':' -f1-2)
    local pkg_manager=$(echo "$dist_info" | cut -d':' -f3)
    local test_name="Basic Installation on $image ($pkg_manager)"
    
    print_status "🧪 Testing: $test_name"
    
    # Only install minimal deps - let the installer handle the rest via setup-dependencies.sh
    local minimal_deps_cmd=$(get_minimal_deps "$dist_info")
    
    local install_command="
        $minimal_deps_cmd && \
        curl -fsSL $INSTALLER_URL | bash -s -- --no-start && \
        ls -la ~/push-validator-manager/ && \
        ~/push-validator-manager/push-validator-manager help
    "
    
    if output=$(run_in_container "$image" "$install_command" 2>&1); then
        if echo "$output" | grep -q "push-validator-manager help" && echo "$output" | grep -q "Essential Commands"; then
            log_test_result "$test_name" "PASS" "Installation completed and help command works"
        else
            log_test_result "$test_name" "FAIL" "Installation completed but help command failed"
        fi
    else
        log_test_result "$test_name" "FAIL" "Installation failed: $output"
    fi
}

test_binary_creation() {
    local dist_info="$1"
    local image=$(echo "$dist_info" | cut -d':' -f1-2)
    local pkg_manager=$(echo "$dist_info" | cut -d':' -f3)
    local test_name="Binary Creation on $image ($pkg_manager)"
    
    print_status "🧪 Testing: $test_name"
    
    # Install minimal deps + file command for testing
    local minimal_deps_cmd=$(get_minimal_deps "$dist_info")
    local file_cmd=""
    case "$pkg_manager" in
        apt) file_cmd="&& apt-get install -y file" ;;
        dnf|yum) file_cmd="&& $pkg_manager install -y file" ;;
    esac
    
    local install_command="
        $minimal_deps_cmd $file_cmd && \
        curl -fsSL $INSTALLER_URL | bash -s -- --no-start && \
        ls -la ~/push-validator-manager/repo/push-validator-manager/build/ && \
        file ~/push-validator-manager/repo/push-validator-manager/build/pchaind
    "
    
    if output=$(run_in_container "$image" "$install_command" 2>&1); then
        if echo "$output" | grep -q "pchaind.*executable"; then
            log_test_result "$test_name" "PASS" "Binary created successfully via setup-dependencies.sh"
        else
            log_test_result "$test_name" "FAIL" "Binary not created or not executable"
        fi
    else
        log_test_result "$test_name" "FAIL" "Installation failed: $output"
    fi
}

test_symlink_creation() {
    local image="$1"
    local test_name="Symlink Creation on $image"
    
    print_status "🧪 Testing: $test_name"
    
    local install_command="
        apt-get update && apt-get install -y curl jq git build-essential golang-go && \
        curl -fsSL $INSTALLER_URL | bash -s -- --no-start && \
        ls -la ~/push-validator-manager/push-validator-manager && \
        readlink ~/push-validator-manager/push-validator-manager
    "
    
    if output=$(run_in_container "$image" "$install_command" 2>&1); then
        if echo "$output" | grep -q "push-validator-manager.*->.*repo/push-validator-manager/push-validator-manager"; then
            log_test_result "$test_name" "PASS" "Symlink created correctly"
        else
            log_test_result "$test_name" "FAIL" "Symlink not created correctly"
        fi
    else
        log_test_result "$test_name" "FAIL" "Installation failed: $output"
    fi
}

test_environment_file() {
    local image="$1"
    local test_name="Environment File on $image"
    
    print_status "🧪 Testing: $test_name"
    
    local install_command="
        apt-get update && apt-get install -y curl jq git build-essential golang-go && \
        MONIKER=test-node KEYRING_BACKEND=test curl -fsSL $INSTALLER_URL | bash -s -- --no-start && \
        cat ~/push-validator-manager/.env
    "
    
    if output=$(run_in_container "$image" "$install_command" 2>&1); then
        if echo "$output" | grep -q "MONIKER=test-node" && echo "$output" | grep -q "KEYRING_BACKEND=test"; then
            log_test_result "$test_name" "PASS" "Environment file created with correct values"
        else
            log_test_result "$test_name" "FAIL" "Environment file missing or incorrect values"
        fi
    else
        log_test_result "$test_name" "FAIL" "Installation failed: $output"
    fi
}

test_command_functionality() {
    local image="$1"
    local test_name="Command Functionality on $image"
    
    print_status "🧪 Testing: $test_name"
    
    local install_command="
        apt-get update && apt-get install -y curl jq git build-essential golang-go && \
        curl -fsSL $INSTALLER_URL | bash -s -- --no-start && \
        cd ~/push-validator-manager && \
        ./push-validator-manager help && \
        ./push-validator-manager status || echo 'Status command ran (expected to show not running)' && \
        ./push-validator-manager validators || echo 'Validators command ran (expected to fail without network)'
    "
    
    if output=$(run_in_container "$image" "$install_command" 2>&1); then
        if echo "$output" | grep -q "Essential Commands" && echo "$output" | grep -q "Status command ran"; then
            log_test_result "$test_name" "PASS" "Basic commands work correctly"
        else
            log_test_result "$test_name" "FAIL" "Commands failed to execute properly"
        fi
    else
        log_test_result "$test_name" "FAIL" "Installation or command execution failed: $output"
    fi
}

test_reinstallation() {
    local image="$1"
    local test_name="Re-installation on $image"
    
    print_status "🧪 Testing: $test_name"
    
    local install_command="
        apt-get update && apt-get install -y curl jq git build-essential golang-go && \
        curl -fsSL $INSTALLER_URL | bash -s -- --no-start && \
        echo 'First installation complete' && \
        curl -fsSL $INSTALLER_URL | bash -s -- --no-start && \
        echo 'Second installation complete' && \
        ~/push-validator-manager/push-validator-manager help
    "
    
    if output=$(run_in_container "$image" "$install_command" 2>&1); then
        if echo "$output" | grep -q "Second installation complete" && echo "$output" | grep -q "Essential Commands"; then
            log_test_result "$test_name" "PASS" "Re-installation works correctly"
        else
            log_test_result "$test_name" "FAIL" "Re-installation failed"
        fi
    else
        log_test_result "$test_name" "FAIL" "Re-installation process failed: $output"
    fi
}

# Helper function to get minimal package installation for testing
get_minimal_deps() {
    local dist_info="$1"
    local pkg_manager=$(echo "$dist_info" | cut -d':' -f3)
    
    # IMPORTANT: Only install minimal dependencies needed for the installer to run
    # The actual build dependencies (Go, build tools, etc.) are handled by:
    # scripts/setup-dependencies.sh which is called by the installer
    # 
    # This separation ensures:
    # 1. Test doesn't duplicate dependency management logic
    # 2. We test the real installation flow 
    # 3. setup-dependencies.sh handles OS detection and proper package installation
    case "$pkg_manager" in
        apt)
            echo "apt-get update && apt-get install -y curl jq git"
            ;;
        dnf)
            echo "dnf install -y curl jq git"
            ;;
        yum)
            echo "yum install -y curl jq git"
            ;;
        *)
            echo "echo 'Unknown package manager: $pkg_manager'; exit 1"
            ;;
    esac
}

# Update remaining test functions to use new format
test_symlink_creation() {
    local dist_info="$1"
    local image=$(echo "$dist_info" | cut -d':' -f1-2)
    local pkg_manager=$(echo "$dist_info" | cut -d':' -f3)
    local test_name="Symlink Creation on $image ($pkg_manager)"
    
    print_status "🧪 Testing: $test_name"
    
    local pkg_install_cmd=$(get_pkg_command "$dist_info")
    local install_command="
        $pkg_install_cmd && \
        curl -fsSL $INSTALLER_URL | bash -s -- --no-start && \
        ls -la ~/push-validator-manager/push-validator-manager && \
        readlink ~/push-validator-manager/push-validator-manager
    "
    
    if output=$(run_in_container "$image" "$install_command" 2>&1); then
        if echo "$output" | grep -q "push-validator-manager.*->.*repo/push-validator-manager/push-validator-manager"; then
            log_test_result "$test_name" "PASS" "Symlink created correctly"
        else
            log_test_result "$test_name" "FAIL" "Symlink not created correctly"
        fi
    else
        log_test_result "$test_name" "FAIL" "Installation failed: $output"
    fi
}

test_environment_file() {
    local dist_info="$1"
    local image=$(echo "$dist_info" | cut -d':' -f1-2)
    local pkg_manager=$(echo "$dist_info" | cut -d':' -f3)
    local test_name="Environment File on $image ($pkg_manager)"
    
    print_status "🧪 Testing: $test_name"
    
    local pkg_install_cmd=$(get_pkg_command "$dist_info")
    local install_command="
        $pkg_install_cmd && \
        MONIKER=test-node KEYRING_BACKEND=test curl -fsSL $INSTALLER_URL | bash -s -- --no-start && \
        cat ~/push-validator-manager/.env
    "
    
    if output=$(run_in_container "$image" "$install_command" 2>&1); then
        if echo "$output" | grep -q "MONIKER=test-node" && echo "$output" | grep -q "KEYRING_BACKEND=test"; then
            log_test_result "$test_name" "PASS" "Environment file created with correct values"
        else
            log_test_result "$test_name" "FAIL" "Environment file missing or incorrect values"
        fi
    else
        log_test_result "$test_name" "FAIL" "Installation failed: $output"
    fi
}

test_command_functionality() {
    local dist_info="$1"
    local image=$(echo "$dist_info" | cut -d':' -f1-2)
    local pkg_manager=$(echo "$dist_info" | cut -d':' -f3)
    local test_name="Command Functionality on $image ($pkg_manager)"
    
    print_status "🧪 Testing: $test_name"
    
    local pkg_install_cmd=$(get_pkg_command "$dist_info")
    local install_command="
        $pkg_install_cmd && \
        curl -fsSL $INSTALLER_URL | bash -s -- --no-start && \
        cd ~/push-validator-manager && \
        ./push-validator-manager help && \
        ./push-validator-manager status || echo 'Status command ran (expected to show not running)' && \
        ./push-validator-manager validators || echo 'Validators command ran (expected to fail without network)'
    "
    
    if output=$(run_in_container "$image" "$install_command" 2>&1); then
        if echo "$output" | grep -q "Essential Commands" && echo "$output" | grep -q "Status command ran"; then
            log_test_result "$test_name" "PASS" "Basic commands work correctly"
        else
            log_test_result "$test_name" "FAIL" "Commands failed to execute properly"
        fi
    else
        log_test_result "$test_name" "FAIL" "Installation or command execution failed: $output"
    fi
}

test_reinstallation() {
    local dist_info="$1"
    local image=$(echo "$dist_info" | cut -d':' -f1-2)
    local pkg_manager=$(echo "$dist_info" | cut -d':' -f3)
    local test_name="Re-installation on $image ($pkg_manager)"
    
    print_status "🧪 Testing: $test_name"
    
    local pkg_install_cmd=$(get_pkg_command "$dist_info")
    local install_command="
        $pkg_install_cmd && \
        curl -fsSL $INSTALLER_URL | bash -s -- --no-start && \
        echo 'First installation complete' && \
        curl -fsSL $INSTALLER_URL | bash -s -- --no-start && \
        echo 'Second installation complete' && \
        ~/push-validator-manager/push-validator-manager help
    "
    
    if output=$(run_in_container "$image" "$install_command" 2>&1); then
        if echo "$output" | grep -q "Second installation complete" && echo "$output" | grep -q "Essential Commands"; then
            log_test_result "$test_name" "PASS" "Re-installation works correctly"
        else
            log_test_result "$test_name" "FAIL" "Re-installation failed"
        fi
    else
        log_test_result "$test_name" "FAIL" "Re-installation process failed: $output"
    fi
}

# Main testing function
run_distribution_tests() {
    local dist_info="$1"
    local image=$(echo "$dist_info" | cut -d':' -f1-2)
    local pkg_manager=$(echo "$dist_info" | cut -d':' -f3)
    
    print_header "📦 Testing distribution: $image (Package Manager: $pkg_manager)"
    
    # Check if Docker is available
    if ! command -v docker >/dev/null 2>&1; then
        print_error "❌ Docker is not available. Cannot run container tests."
        return 1
    fi
    
    # Pull the image
    print_status "📥 Pulling Docker image: $image"
    if ! docker pull "$image" >/dev/null 2>&1; then
        print_error "❌ Failed to pull Docker image: $image"
        return 1
    fi
    
    # Run all tests for this distribution
    test_basic_installation "$dist_info"
    test_binary_creation "$dist_info"
    test_symlink_creation "$dist_info"
    test_environment_file "$dist_info"
    test_command_functionality "$dist_info"
    test_reinstallation "$dist_info"
    
    echo
}

# Test installer URL accessibility
test_installer_accessibility() {
    print_header "🌐 Testing Installer Accessibility"
    
    if curl -fsSL --max-time 10 "$INSTALLER_URL" >/dev/null 2>&1; then
        log_test_result "Installer URL Accessible" "PASS" "URL is reachable and returns content"
    else
        log_test_result "Installer URL Accessible" "FAIL" "URL is not accessible or returns error"
    fi
}

# Test installer script syntax
test_installer_syntax() {
    print_header "📝 Testing Installer Script Syntax"
    
    local temp_file=$(mktemp)
    
    if curl -fsSL "$INSTALLER_URL" > "$temp_file" 2>/dev/null; then
        if bash -n "$temp_file" 2>/dev/null; then
            log_test_result "Installer Script Syntax" "PASS" "Script syntax is valid"
        else
            log_test_result "Installer Script Syntax" "FAIL" "Script has syntax errors"
        fi
    else
        log_test_result "Installer Script Syntax" "FAIL" "Could not download installer script"
    fi
    
    rm -f "$temp_file"
}

# Print test summary
print_test_summary() {
    print_header "📊 Test Summary"
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
        print_success "🎉 All tests passed! The installer is working correctly."
    else
        print_error "⚠️  Some tests failed. Check the log file for details: $LOG_FILE"
    fi
    
    print_status "Detailed log: $LOG_FILE"
}

# Main execution
main() {
    print_header "🚀 Push Validator Manager Installer Test Suite"
    print_status "Started at: $(date)"
    print_status "Installer URL: $INSTALLER_URL"
    print_status "Log file: $LOG_FILE"
    echo
    
    log "Starting Push Validator Manager installer test suite"
    log "Installer URL: $INSTALLER_URL"
    
    # Test installer accessibility and syntax first
    test_installer_accessibility
    test_installer_syntax
    
    # Test on multiple distributions if Docker is available
    if command -v docker >/dev/null 2>&1; then
        for dist in "${DISTRIBUTIONS[@]}"; do
            run_distribution_tests "$dist"
        done
    else
        print_warning "⚠️  Docker not available. Skipping container-based tests."
        print_status "To run full tests, install Docker and re-run this script."
    fi
    
    print_test_summary
}

# Handle script arguments
case "${1:-}" in
    --help|-h)
        echo "Usage: $0 [--help|--quick]"
        echo ""
        echo "Options:"
        echo "  --help    Show this help message"
        echo "  --quick   Run only basic tests (no Docker required)"
        echo ""
        echo "This script tests the Push Validator Manager installer across multiple environments."
        exit 0
        ;;
    --quick)
        print_header "🚀 Quick Push Validator Manager Installer Tests"
        test_installer_accessibility
        test_installer_syntax
        print_test_summary
        exit 0
        ;;
    *)
        main "$@"
        ;;
esac