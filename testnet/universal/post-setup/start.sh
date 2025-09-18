#!/bin/bash
set -e

# Resolve base directory: $HOME/universal/
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

BINARY="$APP_DIR/binary/puniversald"
LOG_DIR="$APP_DIR/logs"
LOG_FILE="$LOG_DIR/puniversald.log"

mkdir -p "$LOG_DIR"


if [ ! -f "$BINARY" ]; then
  echo "❌ Binary not found at: $BINARY"
  exit 1
fi

"$BINARY" start  > "$LOG_FILE" 2>&1 &
echo "✅ Unversal Node started" 
