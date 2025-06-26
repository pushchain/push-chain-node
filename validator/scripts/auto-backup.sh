#!/bin/bash
# Automatic backup script for validator keys

set -e

BACKUP_DIR="/backups"
PCHAIN_HOME="/root/.pchain"
RETENTION_DAYS=7

# Create backup directory
mkdir -p "$BACKUP_DIR"

# Create timestamp
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="$BACKUP_DIR/validator-backup-$TIMESTAMP.tar.gz"

# Files to backup
BACKUP_FILES=(
    "$PCHAIN_HOME/config/priv_validator_key.json"
    "$PCHAIN_HOME/config/node_key.json"
    "$PCHAIN_HOME/data/priv_validator_state.json"
)

# Check if files exist
for file in "${BACKUP_FILES[@]}"; do
    if [ ! -f "$file" ]; then
        echo "Warning: $file not found, skipping..."
    fi
done

# Create backup
echo "Creating backup at $BACKUP_FILE..."
tar -czf "$BACKUP_FILE" -C / \
    $(printf "%s " "${BACKUP_FILES[@]#/}") 2>/dev/null || true

# Verify backup
if [ -f "$BACKUP_FILE" ]; then
    echo "Backup created successfully: $BACKUP_FILE"
    echo "Size: $(du -h "$BACKUP_FILE" | cut -f1)"
else
    echo "Failed to create backup"
    exit 1
fi

# Clean old backups
echo "Cleaning backups older than $RETENTION_DAYS days..."
find "$BACKUP_DIR" -name "validator-backup-*.tar.gz" -mtime +$RETENTION_DAYS -delete

echo "Backup complete!"