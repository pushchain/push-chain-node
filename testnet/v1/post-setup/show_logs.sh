#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
LOG_FILE="$APP_DIR/logs/pchaind.log"

if [ ! -f "$LOG_FILE" ]; then
  echo "❌ Log file not found at: $LOG_FILE"
  exit 1
fi

tail -f "$LOG_FILE"
