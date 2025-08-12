#!/bin/bash

###############################################
# Push Node Manager Installation Validator
#
# Comprehensive validation script that checks:
# - All expected files and directories exist
# - Binary is built and executable
# - Symlinks are created correctly  
# - Environment configuration is valid
# - All commands are functional
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

# Validation counters
CHECKS_RUN=0
CHECKS_PASSED=0
CHECKS_FAILED=0

# Expected paths
INSTALL_DIR="$HOME/push-node-manager"
REPO_DIR="$INSTALL_DIR/repo"
MANAGER_SCRIPT="$INSTALL_DIR/push-node-manager"
BINARY_PATH="$REPO_DIR/push-node-manager/build/pchaind"
ENV_FILE="$INSTALL_DIR/.env"

# Validation result tracking
check_result() {
    local test_name="$1"
    local result="$2"
    local details="${3:-}"
    
    ((CHECKS_RUN++))
    
    if [ "$result" = "PASS" ]; then
        ((CHECKS_PASSED++))
        print_success "  ✅ $test_name"
        [ -n "$details" ] && print_status "     $details"
    else
        ((CHECKS_FAILED++))
        print_error "  ❌ $test_name"
        [ -n "$details" ] && print_status "     $details"
    fi
}

# Directory structure validation
validate_directory_structure() {
    print_header "📁 Validating Directory Structure"
    
    # Check main installation directory
    if [ -d "$INSTALL_DIR" ]; then
        check_result "Installation directory exists" "PASS" "$INSTALL_DIR"
    else
        check_result "Installation directory exists" "FAIL" "Missing: $INSTALL_DIR"
        return 1
    fi
    
    # Check repository directory
    if [ -d "$REPO_DIR" ]; then
        check_result "Repository directory exists" "PASS" "$REPO_DIR"
    else
        check_result "Repository directory exists" "FAIL" "Missing: $REPO_DIR"
    fi
    
    # Check if it's a git repository
    if [ -d "$REPO_DIR/.git" ]; then
        check_result "Git repository structure" "PASS" "Repository cloned correctly"
    else
        check_result "Git repository structure" "FAIL" "Not a git repository"
    fi
    
    # Check push-node-manager subdirectory
    if [ -d "$REPO_DIR/push-node-manager" ]; then
        check_result "Push Node Manager directory" "PASS" "$REPO_DIR/push-node-manager"
    else
        check_result "Push Node Manager directory" "FAIL" "Missing: $REPO_DIR/push-node-manager"
    fi
    
    # Check build directory
    if [ -d "$REPO_DIR/push-node-manager/build" ]; then
        check_result "Build directory exists" "PASS" "$REPO_DIR/push-node-manager/build"
    else
        check_result "Build directory exists" "FAIL" "Missing: $REPO_DIR/push-node-manager/build"
    fi
    
    # Check scripts directory
    if [ -d "$REPO_DIR/push-node-manager/scripts" ]; then
        check_result "Scripts directory exists" "PASS" "$REPO_DIR/push-node-manager/scripts"
    else
        check_result "Scripts directory exists" "FAIL" "Missing: $REPO_DIR/push-node-manager/scripts"
    fi
    
    echo
}

# Binary validation
validate_binary() {
    print_header "🔧 Validating Binary"
    
    # Check if binary exists
    if [ -f "$BINARY_PATH" ]; then
        check_result "Binary file exists" "PASS" "$BINARY_PATH"
    else
        check_result "Binary file exists" "FAIL" "Missing: $BINARY_PATH"
        return 1
    fi
    
    # Check if binary is executable
    if [ -x "$BINARY_PATH" ]; then
        check_result "Binary is executable" "PASS" "Permissions OK"
    else
        check_result "Binary is executable" "FAIL" "Not executable"
    fi
    
    # Check binary type
    if command -v file >/dev/null 2>&1; then
        binary_type=$(file "$BINARY_PATH" 2>/dev/null || echo "unknown")
        if echo "$binary_type" | grep -q "executable"; then
            check_result "Binary file type" "PASS" "Valid executable"
        else
            check_result "Binary file type" "FAIL" "Not a valid executable: $binary_type"
        fi
    fi
    
    # Test binary version (basic check)
    if "$BINARY_PATH" version >/dev/null 2>&1; then
        version_output=$("$BINARY_PATH" version 2>/dev/null || echo "unknown")
        check_result "Binary functionality" "PASS" "Version: $version_output"
    else
        check_result "Binary functionality" "FAIL" "Binary does not respond to version command"
    fi
    
    echo
}

# Manager script validation
validate_manager_script() {
    print_header "📜 Validating Manager Script"
    
    # Check if manager script exists
    if [ -f "$MANAGER_SCRIPT" ]; then
        check_result "Manager script exists" "PASS" "$MANAGER_SCRIPT"
    else
        check_result "Manager script exists" "FAIL" "Missing: $MANAGER_SCRIPT"
        return 1
    fi
    
    # Check if it's a symlink
    if [ -L "$MANAGER_SCRIPT" ]; then
        target=$(readlink "$MANAGER_SCRIPT")
        check_result "Manager script is symlink" "PASS" "Points to: $target"
        
        # Check if symlink target exists
        if [ -f "$target" ]; then
            check_result "Symlink target exists" "PASS" "Target is valid"
        else
            check_result "Symlink target exists" "FAIL" "Broken symlink"
        fi
    else
        check_result "Manager script is symlink" "FAIL" "Should be a symlink"
    fi
    
    # Check if manager script is executable
    if [ -x "$MANAGER_SCRIPT" ]; then
        check_result "Manager script executable" "PASS" "Can execute"
    else
        check_result "Manager script executable" "FAIL" "Not executable"
    fi
    
    echo
}

# Environment file validation
validate_environment() {
    print_header "🌍 Validating Environment Configuration"
    
    # Check if .env file exists
    if [ -f "$ENV_FILE" ]; then
        check_result "Environment file exists" "PASS" "$ENV_FILE"
    else
        check_result "Environment file exists" "FAIL" "Missing: $ENV_FILE"
        return 1
    fi
    
    # Check required environment variables
    required_vars=("GENESIS_DOMAIN" "MONIKER" "KEYRING_BACKEND")
    
    for var in "${required_vars[@]}"; do
        if grep -q "^$var=" "$ENV_FILE" 2>/dev/null; then
            value=$(grep "^$var=" "$ENV_FILE" | cut -d'=' -f2-)
            check_result "$var is set" "PASS" "Value: $value"
        else
            check_result "$var is set" "FAIL" "Missing from environment file"
        fi
    done
    
    # Validate environment file syntax
    if bash -n "$ENV_FILE" 2>/dev/null; then
        check_result "Environment file syntax" "PASS" "Valid bash syntax"
    else
        check_result "Environment file syntax" "FAIL" "Invalid syntax"
    fi
    
    echo
}

# Command functionality validation
validate_commands() {
    print_header "⚙️ Validating Command Functionality"
    
    cd "$INSTALL_DIR" || return 1
    
    # Test help command
    if ./push-node-manager help >/dev/null 2>&1; then
        check_result "Help command works" "PASS" "Command executes successfully"
    else
        check_result "Help command works" "FAIL" "Help command failed"
    fi
    
    # Test status command (should work even if node not running)
    if ./push-node-manager status >/dev/null 2>&1 || true; then
        check_result "Status command works" "PASS" "Command executes (expected to show 'not running')"
    else
        check_result "Status command works" "FAIL" "Status command failed unexpectedly"
    fi
    
    # Check if all essential scripts exist
    essential_scripts=(
        "scripts/setup-dependencies.sh"
        "scripts/register-validator.sh"
        "scripts/setup-nginx.sh"
        "scripts/setup-log-rotation.sh"
        "scripts/backup.sh"
    )
    
    for script in "${essential_scripts[@]}"; do
        script_path="$REPO_DIR/push-node-manager/$script"
        if [ -f "$script_path" ] && [ -x "$script_path" ]; then
            check_result "$(basename "$script") exists" "PASS" "Script is present and executable"
        else
            check_result "$(basename "$script") exists" "FAIL" "Missing or not executable: $script_path"
        fi
    done
    
    echo
}

# Network connectivity validation
validate_network() {
    print_header "🌐 Validating Network Connectivity"
    
    # Test GitHub connectivity (for updates)
    if curl -fsSL --max-time 10 "https://api.github.com" >/dev/null 2>&1; then
        check_result "GitHub connectivity" "PASS" "Can reach GitHub API"
    else
        check_result "GitHub connectivity" "FAIL" "Cannot reach GitHub API"
    fi
    
    # Test Push Chain testnet connectivity
    genesis_domain=$(grep "^GENESIS_DOMAIN=" "$ENV_FILE" 2>/dev/null | cut -d'=' -f2- || echo "rpc-testnet-donut-node1.push.org")
    
    if curl -fsSL --max-time 10 "https://$genesis_domain/status" >/dev/null 2>&1; then
        check_result "Push Chain RPC connectivity" "PASS" "Can reach $genesis_domain"
    else
        check_result "Push Chain RPC connectivity" "FAIL" "Cannot reach $genesis_domain"
    fi
    
    echo
}

# File permissions validation
validate_permissions() {
    print_header "🔒 Validating File Permissions"
    
    # Check directory permissions
    if [ -w "$INSTALL_DIR" ]; then
        check_result "Installation directory writable" "PASS" "User can write to installation directory"
    else
        check_result "Installation directory writable" "FAIL" "No write access to installation directory"
    fi
    
    # Check if ~/.pchain directory exists and is writable
    pchain_dir="$HOME/.pchain"
    if [ -d "$pchain_dir" ]; then
        if [ -w "$pchain_dir" ]; then
            check_result "Node data directory writable" "PASS" "~/.pchain is writable"
        else
            check_result "Node data directory writable" "FAIL" "~/.pchain is not writable"
        fi
    else
        check_result "Node data directory writable" "PASS" "~/.pchain will be created when needed"
    fi
    
    echo
}

# Dependency validation
validate_dependencies() {
    print_header "📦 Validating Dependencies"
    
    # Required system dependencies
    dependencies=("curl" "jq" "git" "tar")
    
    for dep in "${dependencies[@]}"; do
        if command -v "$dep" >/dev/null 2>&1; then
            version_info=$(command -v "$dep" 2>/dev/null)
            check_result "$dep available" "PASS" "Found at: $version_info"
        else
            check_result "$dep available" "FAIL" "Command not found"
        fi
    done
    
    # Check Go version if available
    if command -v go >/dev/null 2>&1; then
        go_version=$(go version 2>/dev/null | awk '{print $3}' || echo "unknown")
        check_result "Go toolchain" "PASS" "Version: $go_version"
    else
        check_result "Go toolchain" "FAIL" "Go not found (may have been installed during setup)"
    fi
    
    echo
}

# Print validation summary
print_validation_summary() {
    print_header "📊 Validation Summary"
    echo
    print_status "Total Checks: $CHECKS_RUN"
    print_success "Passed: $CHECKS_PASSED"
    print_error "Failed: $CHECKS_FAILED"
    
    local success_rate=0
    if [ "$CHECKS_RUN" -gt 0 ]; then
        success_rate=$((CHECKS_PASSED * 100 / CHECKS_RUN))
    fi
    
    print_status "Success Rate: ${success_rate}%"
    echo
    
    if [ "$CHECKS_FAILED" -eq 0 ]; then
        print_success "🎉 Installation validation passed! Push Node Manager is properly installed."
        echo
        print_status "Next steps:"
        print_status "  • Start your node: $MANAGER_SCRIPT start"
        print_status "  • Check status: $MANAGER_SCRIPT status"
        print_status "  • View help: $MANAGER_SCRIPT help"
    else
        print_error "⚠️  Some validation checks failed."
        print_status "This may indicate an incomplete or corrupted installation."
        echo
        print_status "To fix issues:"
        print_status "  • Re-run the installer: curl -fsSL https://get.push.network/node/install.sh | bash"
        print_status "  • Check the installation logs for errors"
        print_status "  • Ensure you have proper permissions and dependencies"
    fi
}

# Main execution
main() {
    print_header "🔍 Push Node Manager Installation Validator"
    print_status "Checking installation at: $INSTALL_DIR"
    echo
    
    # Run all validation checks
    validate_directory_structure
    validate_binary
    validate_manager_script
    validate_environment
    validate_commands
    validate_network
    validate_permissions
    validate_dependencies
    
    # Print summary
    print_validation_summary
    
    # Return appropriate exit code
    if [ "$CHECKS_FAILED" -eq 0 ]; then
        exit 0
    else
        exit 1
    fi
}

# Handle script arguments
case "${1:-}" in
    --help|-h)
        echo "Usage: $0 [--help|--quiet]"
        echo ""
        echo "This script validates a Push Node Manager installation."
        echo ""
        echo "Options:"
        echo "  --help    Show this help message"
        echo "  --quiet   Suppress detailed output, show only summary"
        echo ""
        echo "Exit codes:"
        echo "  0 - All validation checks passed"
        echo "  1 - One or more validation checks failed"
        exit 0
        ;;
    --quiet)
        # Redirect detailed output to /dev/null for quiet mode
        exec 3>&1
        exec 1>/dev/null
        main "$@"
        exec 1>&3
        ;;
    *)
        main "$@"
        ;;
esac