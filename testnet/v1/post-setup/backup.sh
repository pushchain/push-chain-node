#!/bin/bash

###############################################
# Push Chain Node Backup Script (Manual)
#
# Archives the full .pchain directory and stores
# it in /home/app/backups with a timestamp.
###############################################

set -e

NODE_HOME="/home/app/.pchain"
BACKUP_DIR="/home/app/backups"
TIMESTAMP=$(date +"%Y-%m-%d_%H-%M-%S")
ARCHIVE_NAME="pchain-backup-$TIMESTAMP.tar.gz"

echo "📦 Creating backup: $ARCHIVE_NAME"
mkdir -p "$BACKUP_DIR"

tar -czvf "$BACKUP_DIR/$ARCHIVE_NAME" "$NODE_HOME"

echo "✅ Backup saved at: $BACKUP_DIR/$ARCHIVE_NAME"
