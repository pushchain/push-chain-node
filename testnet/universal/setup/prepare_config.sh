#!/bin/bash
set -e

# Resolve base directory: $HOME/app/
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

BINARY="$APP_DIR/binary/puniversald"


if [ ! -f "$BINARY" ]; then
  echo "❌ Binary not found at: $BINARY"
  exit 1
fi

"$BINARY" init"
echo "✅ Unversal Node setup"
