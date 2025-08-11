#!/bin/bash

###############################################
# Push Chain Log Rotation Setup Script
# Native Push Node Manager Edition
#
# - Rotates logs under ~/.pchain/logs/
# - Uses logrotate (daily, compress, 14-day retention)
# - Target: ~/.pchain/logs/*.log
# - Adapted for native setup paths
###############################################

set -euo pipefail

# Colors for output - Standardized palette
GREEN='\033[0;32m'      # Success messages
RED='\033[0;31m'        # Error messages  
YELLOW='\033[0;33m'     # Warning messages
CYAN='\033[0;36m'       # Status/info messages
BLUE='\033[1;94m'       # Headers/titles (bright blue)
NC='\033[0m'            # No color/reset
BOLD='\033[1m'          # Emphasis

# Print functions - Unified across all scripts
print_status() { echo -e "${CYAN}$1${NC}"; }
print_header() { echo -e "${BLUE}$1${NC}"; }
print_success() { echo -e "${GREEN}$1${NC}"; }
print_error() { echo -e "${RED}$1${NC}"; }
print_warning() { echo -e "${YELLOW}$1${NC}"; }

# Configuration
LOG_DIR="$HOME/.pchain/logs"
LOGROTATE_CONF="/etc/logrotate.d/push-chain-$USER"

print_header "🗂️  Setting up log rotation for Push Chain"
echo

# Detect operating system
OS=$(uname -s)
case "$OS" in
    Linux*)
        MACHINE="Linux"
        ;;
    Darwin*)
        MACHINE="macOS"
        ;;
    *)
        MACHINE="Unknown"
        ;;
esac

print_status "🖥️  Detected OS: $MACHINE"

# Handle macOS differently
if [ "$MACHINE" = "macOS" ]; then
    print_status "🍎 macOS detected - using native log management"
    print_status "ℹ️  macOS automatically manages log rotation via ASL/Unified Logging"
    print_status "📁 Your logs are in: $LOG_DIR"
    print_status "🔍 View logs with: ./push-node-manager logs"
    echo
    print_success "✅ Log management configured for macOS"
    print_status "💡 Manual cleanup command if needed:"
    print_status "  find $LOG_DIR -name '*.log' -mtime +14 -delete"
    exit 0
fi

# Linux-specific setup
print_status "🐧 Linux detected - setting up logrotate"

# Check if we're running as root or have sudo
if [ "$EUID" -eq 0 ]; then
    SUDO=""
    print_warning "⚠️  Running as root - this is not recommended for normal operation"
else
    if ! command -v sudo >/dev/null 2>&1; then
        print_error "❌ This script requires sudo privileges to configure system log rotation"
        exit 1
    fi
    SUDO="sudo"
fi

# Check if log directory exists
if [ ! -d "$LOG_DIR" ]; then
    print_status "📁 Creating log directory: $LOG_DIR"
    mkdir -p "$LOG_DIR"
fi

# Install logrotate if missing
print_status "🔍 Checking for logrotate..."
if ! command -v logrotate >/dev/null 2>&1; then
    print_status "📦 Installing logrotate..."
    
    # Detect package manager
    if command -v apt-get >/dev/null 2>&1; then
        $SUDO apt-get update && $SUDO apt-get install -y logrotate
    elif command -v yum >/dev/null 2>&1; then
        $SUDO yum install -y logrotate
    elif command -v dnf >/dev/null 2>&1; then
        $SUDO dnf install -y logrotate
    elif command -v pacman >/dev/null 2>&1; then
        $SUDO pacman -S --noconfirm logrotate
    else
        print_error "❌ Could not detect package manager. Please install logrotate manually."
        exit 1
    fi
    
    print_success "✅ Logrotate installed"
else
    print_success "✅ Logrotate is available"
fi

echo

# Create logrotate configuration
print_status "🛠️  Creating logrotate configuration at $LOGROTATE_CONF..."

# Get the current user for proper permissions
CURRENT_USER=$(whoami)
CURRENT_GROUP=$(id -gn)

$SUDO tee "$LOGROTATE_CONF" > /dev/null <<EOF
# Push Chain log rotation configuration
# Rotates logs for user: $CURRENT_USER
$LOG_DIR/*.log {
    # Rotate daily
    daily
    
    # Keep 14 days worth of backlogs
    rotate 14
    
    # Compress old logs (saves space)
    compress
    
    # Don't compress the first rotated log (allows immediate access)
    delaycompress
    
    # Don't error if log files are missing
    missingok
    
    # Don't rotate empty files
    notifempty
    
    # Create new log files with these permissions
    create 0644 $CURRENT_USER $CURRENT_GROUP
    
    # Run as the specified user/group
    su $CURRENT_USER $CURRENT_GROUP
    
    # Run postrotate script only once for all logs
    sharedscripts
    
    # Commands to run after rotation
    postrotate
        # Send HUP signal to pchaind process to reopen log files
        # This ensures the process starts writing to the new log file
        pkill -HUP -f "pchaind start" || true
        
        # If nginx is running, reload it too (for public setups)
        systemctl is-active nginx >/dev/null 2>&1 && systemctl reload nginx >/dev/null 2>&1 || true
    endscript
}
EOF

print_success "✅ Log rotation configuration created"
echo

# Test the configuration
print_status "🧪 Testing logrotate configuration..."
if $SUDO logrotate --debug "$LOGROTATE_CONF" 2>/dev/null; then
    print_success "✅ Configuration test passed"
else
    print_warning "⚠️  Configuration test had warnings (this may be normal)"
fi

echo

# Show configuration details
print_header "📋 Log Rotation Summary"
echo
print_status "🗂️  Log directory: ${BOLD}$LOG_DIR${NC}"
print_status "⚙️  Configuration: ${BOLD}$LOGROTATE_CONF${NC}"
print_status "📅 Rotation schedule: ${BOLD}Daily${NC}"
print_status "🗄️  Retention period: ${BOLD}14 days${NC}"
print_status "📦 Compression: ${BOLD}Enabled${NC}"
print_status "👤 File owner: ${BOLD}$CURRENT_USER:$CURRENT_GROUP${NC}"

echo
print_success "🚀 Log rotation setup complete!"
echo

print_status "📝 What happens now:"
print_status "  • Logs will be rotated daily at system-defined time"
print_status "  • Old logs will be compressed to save space"
print_status "  • Logs older than 14 days will be automatically deleted"
print_status "  • Process will be signaled to reopen log files"

echo
print_status "🔍 Useful commands:"
print_status "  • Check logs: ls -la $LOG_DIR"
print_status "  • Manual rotation: sudo logrotate -f $LOGROTATE_CONF"
print_status "  • View configuration: cat $LOGROTATE_CONF"
print_status "  • System log status: sudo systemctl status logrotate"

# Check if any log files currently exist
if ls "$LOG_DIR"/*.log >/dev/null 2>&1; then
    echo
    print_status "📄 Current log files:"
    ls -lh "$LOG_DIR"/*.log | while read -r line; do
        print_status "    $line"
    done
else
    echo
    print_status "📄 No log files found yet - they will be created when the node runs"
fi