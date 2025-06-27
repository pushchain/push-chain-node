#!/bin/bash
# Secure backup script for validator keys

set -e

# Source common functions
source /scripts/common.sh

# Check if running inside container
check_container "/scripts/backup-keys.sh"

# Configuration
BACKUP_DIR="/backup"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Create secure backup
create_backup() {
    local backup_name="node-keys-${TIMESTAMP}"
    local temp_dir="/tmp/${backup_name}"
    
    log_info "Creating secure backup of validator keys..."
    
    # Create temporary directory
    mkdir -p "$temp_dir"
    chmod 700 "$temp_dir"
    
    # Copy important files
    if [ -f "$CONFIG_DIR/priv_validator_key.json" ]; then
        cp "$CONFIG_DIR/priv_validator_key.json" "$temp_dir/"
    else
        log_warning "Validator key not found"
    fi
    
    if [ -f "$CONFIG_DIR/node_key.json" ]; then
        cp "$CONFIG_DIR/node_key.json" "$temp_dir/"
    fi
    
    if [ -f "$DATA_DIR/priv_validator_state.json" ]; then
        cp "$DATA_DIR/priv_validator_state.json" "$temp_dir/"
    fi
    
    # Copy keyring if using file backend
    if [ "$KEYRING" = "file" ] && [ -d "$PCHAIN_HOME/keyring-file" ]; then
        cp -r "$PCHAIN_HOME/keyring-file" "$temp_dir/"
    fi
    
    # Create info file
    cat > "$temp_dir/backup-info.txt" << EOF
Push Chain Validator Backup
Created: $(date)
Moniker: $MONIKER
Network: $NETWORK
Chain ID: $CHAIN_ID
Keyring: $KEYRING
EOF
    
    # Prompt for encryption
    echo ""
    read -p "Encrypt backup with password? (highly recommended) [Y/n]: " -n 1 -r
    echo ""
    
    if [[ ! $REPLY =~ ^[Nn]$ ]]; then
        # Create encrypted archive
        log_info "Creating encrypted backup..."
        
        # Use openssl for encryption
        cd /tmp
        tar -czf - "$backup_name" | \
            openssl enc -aes-256-cbc -salt -pbkdf2 -out "$BACKUP_DIR/${backup_name}.tar.gz.enc"
        
        log_success "Encrypted backup created: $BACKUP_DIR/${backup_name}.tar.gz.enc"
        log_warning "⚠️  IMPORTANT: Remember your password! It cannot be recovered."
        
        # Create decryption instructions
        cat > "$BACKUP_DIR/${backup_name}-decrypt-instructions.txt" << EOF
To decrypt this backup:

openssl enc -aes-256-cbc -d -pbkdf2 -in ${backup_name}.tar.gz.enc -out ${backup_name}.tar.gz
tar -xzf ${backup_name}.tar.gz

IMPORTANT: Store this backup and your password securely!
EOF
    else
        # Create unencrypted archive (not recommended)
        cd /tmp
        tar -czf "$BACKUP_DIR/${backup_name}.tar.gz" "$backup_name"
        log_warning "Created UNENCRYPTED backup: $BACKUP_DIR/${backup_name}.tar.gz"
        log_warning "⚠️  This backup is not encrypted! Store it very securely."
    fi
    
    # Clean up
    rm -rf "$temp_dir"
    
    # Set secure permissions
    chmod 600 "$BACKUP_DIR"/${backup_name}*
    
    log_success "Backup complete!"
    echo "Files backed up:"
    echo "  - Validator key"
    echo "  - Node key"
    echo "  - Validator state"
    if [ "$KEYRING" = "file" ]; then
        echo "  - Keyring files"
    fi
}

# Restore from backup
restore_backup() {
    local backup_file="$1"
    
    if [ -z "$backup_file" ]; then
        log_error "Please specify backup file to restore"
        echo "Usage: $0 restore <backup-file>"
        exit 1
    fi
    
    if [ ! -f "$backup_file" ]; then
        log_error "Backup file not found: $backup_file"
        exit 1
    fi
    
    log_warning "⚠️  This will overwrite existing keys!"
    read -p "Are you sure you want to restore from backup? (yes/no): " -r
    if [[ ! $REPLY == "yes" ]]; then
        log_info "Restore cancelled"
        exit 0
    fi
    
    # Create temporary directory
    local temp_dir="/tmp/restore-$$"
    mkdir -p "$temp_dir"
    cd "$temp_dir"
    
    # Check if encrypted
    if [[ "$backup_file" == *.enc ]]; then
        log_info "Decrypting backup..."
        openssl enc -aes-256-cbc -d -pbkdf2 -in "$backup_file" | tar -xz
    else
        log_info "Extracting backup..."
        tar -xzf "$backup_file"
    fi
    
    # Find extracted directory
    local backup_dir=$(find . -maxdepth 1 -type d -name "node-keys-*" | head -1)
    
    if [ -z "$backup_dir" ]; then
        log_error "Invalid backup file format"
        exit 1
    fi
    
    # Restore files
    log_info "Restoring validator keys..."
    
    if [ -f "$backup_dir/priv_validator_key.json" ]; then
        cp "$backup_dir/priv_validator_key.json" "$CONFIG_DIR/"
        chmod 600 "$CONFIG_DIR/priv_validator_key.json"
    fi
    
    if [ -f "$backup_dir/node_key.json" ]; then
        cp "$backup_dir/node_key.json" "$CONFIG_DIR/"
        chmod 600 "$CONFIG_DIR/node_key.json"
    fi
    
    if [ -f "$backup_dir/priv_validator_state.json" ]; then
        cp "$backup_dir/priv_validator_state.json" "$DATA_DIR/"
    fi
    
    if [ -d "$backup_dir/keyring-file" ]; then
        rm -rf "$PCHAIN_HOME/keyring-file"
        cp -r "$backup_dir/keyring-file" "$PCHAIN_HOME/"
    fi
    
    # Clean up
    rm -rf "$temp_dir"
    
    log_success "Restore complete!"
    log_warning "Please restart your validator"
}

# Main
case "${1:-backup}" in
    backup)
        create_backup
        ;;
    restore)
        restore_backup "$2"
        ;;
    *)
        echo "Usage: $0 {backup|restore <file>}"
        exit 1
        ;;
esac