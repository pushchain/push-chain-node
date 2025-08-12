#!/bin/bash

###############################################
# Push Chain Node Backup Script
# Native Push Node Manager Edition
#
# Archives the full ~/.pchain directory and stores
# it in ~/push-node-backups with a timestamp.
# Includes configuration, keys, and blockchain data.
###############################################

set -euo pipefail

# Colors for output - Standardized palette
GREEN='\033[0;32m'      # Success messages
RED='\033[0;31m'        # Error messages  
YELLOW='\033[0;33m'     # Warning messages
CYAN='\033[0;36m'       # Status/info messages
BLUE='\033[1;94m'       # Headers/titles (bright blue)
MAGENTA='\033[0;35m'    # Accent/highlight data
NC='\033[0m'            # No color/reset
BOLD='\033[1m'          # Emphasis

# Print functions - Unified across all scripts
print_status() { echo -e "${CYAN}$1${NC}"; }
print_header() { echo -e "${BLUE}$1${NC}"; }
print_success() { echo -e "${GREEN}$1${NC}"; }
print_error() { echo -e "${RED}$1${NC}"; }
print_warning() { echo -e "${YELLOW}$1${NC}"; }

# Configuration
NODE_HOME="$HOME/.pchain"
BACKUP_DIR="$HOME/push-node-backups"
TIMESTAMP=$(date +"%Y-%m-%d_%H-%M-%S")
ARCHIVE_NAME="pchain-backup-$TIMESTAMP.tar.gz"
BACKUP_PATH="$BACKUP_DIR/$ARCHIVE_NAME"

print_header "📦 Creating Push Chain backup"
echo

# Validate that .pchain directory exists
if [ ! -d "$NODE_HOME" ]; then
    print_error "❌ Node directory not found: $NODE_HOME"
    print_status "Make sure you have initialized the node first:"
    print_status "  ./push-node-manager start"
    exit 1
fi

# Create backup directory if it doesn't exist
print_status "📁 Preparing backup directory: $BACKUP_DIR"
mkdir -p "$BACKUP_DIR"

# Check available disk space
print_status "💾 Checking disk space..."
NODE_SIZE=$(du -sh "$NODE_HOME" 2>/dev/null | cut -f1 || echo "unknown")
AVAILABLE_SPACE=$(df -h "$BACKUP_DIR" | tail -1 | awk '{print $4}')

print_status "  • Node data size: ${MAGENTA}$NODE_SIZE${NC}"
print_status "  • Available space: ${MAGENTA}$AVAILABLE_SPACE${NC}"

# Show what will be backed up
print_status "📋 Backup contents:"
if [ -d "$NODE_HOME/config" ]; then
    print_status "  ✅ Configuration files"
else
    print_warning "  ⚠️  No config directory found"
fi

if [ -d "$NODE_HOME/data" ]; then
    print_status "  ✅ Blockchain data"
else
    print_warning "  ⚠️  No data directory found"
fi

if [ -d "$NODE_HOME/keyring-test" ] || [ -d "$NODE_HOME/keyring-file" ] || [ -d "$NODE_HOME/keyring-os" ]; then
    print_status "  ✅ Validator keys"
else
    print_warning "  ⚠️  No keyring found"
fi

if [ -d "$NODE_HOME/logs" ]; then
    print_status "  ✅ Log files"
else
    print_status "  ℹ️  No logs directory"
fi

echo

# Warning about sensitive data
print_warning "🔒 Security Notice:"
print_status "This backup contains sensitive validator keys and should be:"
print_status "  • Stored securely"
print_status "  • Never shared publicly"
print_status "  • Protected with appropriate file permissions"
echo

# Confirm backup
read -p "Continue with backup? (y/N): " -r
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    print_status "Backup cancelled by user"
    exit 0
fi

echo
print_status "📦 Creating compressed archive..."
print_status "Source: ${MAGENTA}$NODE_HOME${NC}"
print_status "Target: ${MAGENTA}$BACKUP_PATH${NC}"
echo

# Create the backup with progress indication
if command -v pv >/dev/null 2>&1; then
    # Use pv for progress if available
    tar -cf - -C "$(dirname "$NODE_HOME")" "$(basename "$NODE_HOME")" | pv -s "$(du -sb "$NODE_HOME" | cut -f1)" | gzip > "$BACKUP_PATH"
else
    # Fallback without progress
    tar -czf "$BACKUP_PATH" -C "$(dirname "$NODE_HOME")" "$(basename "$NODE_HOME")"
fi

# Verify backup was created successfully
if [ -f "$BACKUP_PATH" ]; then
    BACKUP_SIZE=$(du -sh "$BACKUP_PATH" | cut -f1)
    print_success "✅ Backup created successfully!"
    echo
    print_header "📊 Backup Summary"
    print_status "📁 Location: ${BOLD}$BACKUP_PATH${NC}"
    print_status "📦 Size: ${BOLD}$BACKUP_SIZE${NC}"
    print_status "🕐 Created: ${BOLD}$(date)${NC}"
    print_status "🔒 Permissions: ${BOLD}$(ls -la "$BACKUP_PATH" | awk '{print $1" "$3":"$4}')${NC}"
else
    print_error "❌ Backup creation failed!"
    exit 1
fi

echo

# Test backup integrity
print_status "🧪 Testing backup integrity..."
if tar -tzf "$BACKUP_PATH" >/dev/null 2>&1; then
    print_success "✅ Backup integrity verified"
else
    print_error "❌ Backup integrity check failed!"
    exit 1
fi

echo
print_header "📝 What's included in this backup:"

# List backup contents with details
CONTENTS=$(tar -tzf "$BACKUP_PATH" | head -20)
echo "$CONTENTS" | while IFS= read -r line; do
    if [[ "$line" == *"keyring"* ]]; then
        print_status "  🔑 $line"
    elif [[ "$line" == *"config"* ]]; then
        print_status "  ⚙️  $line"
    elif [[ "$line" == *"data"* ]]; then
        print_status "  💾 $line"
    else
        print_status "  📄 $line"
    fi
done

TOTAL_FILES=$(tar -tzf "$BACKUP_PATH" | wc -l)
if [ "$TOTAL_FILES" -gt 20 ]; then
    print_status "  ... and $((TOTAL_FILES - 20)) more files"
fi

echo
print_header "🔄 Restore Instructions"
print_status "To restore this backup:"
print_status "  1. Stop the node: ${BOLD}./push-node-manager stop${NC}"
print_status "  2. Backup current data: ${BOLD}mv ~/.pchain ~/.pchain.old${NC}"
print_status "  3. Extract backup: ${BOLD}tar -xzf $BACKUP_PATH -C ~${NC}"
print_status "  4. Start the node: ${BOLD}./push-node-manager start${NC}"

echo
print_header "🗂️  Backup Management"

# Show existing backups
EXISTING_BACKUPS=$(find "$BACKUP_DIR" -name "pchain-backup-*.tar.gz" 2>/dev/null | wc -l)
if [ "$EXISTING_BACKUPS" -gt 1 ]; then
    print_status "📚 You now have $EXISTING_BACKUPS backups:"
    find "$BACKUP_DIR" -name "pchain-backup-*.tar.gz" -exec ls -lh {} \; | \
        sort -k9 | tail -5 | while read -r line; do
        backup_file=$(echo "$line" | awk '{print $9}')
        backup_size=$(echo "$line" | awk '{print $5}')
        backup_date=$(echo "$line" | awk '{print $6" "$7" "$8}')
        backup_name=$(basename "$backup_file")
        print_status "  📦 $backup_name (${backup_size}, $backup_date)"
    done
    
    if [ "$EXISTING_BACKUPS" -gt 5 ]; then
        print_status "  ... showing 5 most recent backups"
    fi
    
    echo
    print_status "💡 Cleanup old backups to save space:"
    print_status "  • Manual: ${BOLD}rm $BACKUP_DIR/pchain-backup-YYYY-MM-DD_*.tar.gz${NC}"
    print_status "  • Auto cleanup (keep 7 days): ${BOLD}find $BACKUP_DIR -name 'pchain-backup-*.tar.gz' -mtime +7 -delete${NC}"
fi

echo
print_success "🚀 Backup complete!"
print_status "Your Push Chain node data is safely backed up to:"
print_status "${BOLD}$BACKUP_PATH${NC}"