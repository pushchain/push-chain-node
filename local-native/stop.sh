#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/env.sh"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

print_status() { echo -e "${YELLOW}$1${NC}"; }
print_success() { echo -e "${GREEN}$1${NC}"; }

# Stop all validators
for i in 1 2 3 4; do
    pid_file="$DATA_DIR/validator$i.pid"
    if [ -f "$pid_file" ]; then
        pid=$(cat "$pid_file")
        if kill -0 "$pid" 2>/dev/null; then
            print_status "Stopping validator $i (PID: $pid)..."
            kill "$pid" 2>/dev/null || true
            rm -f "$pid_file"
        fi
    fi
    
    # Also stop any universal validators
    uv_pid_file="$DATA_DIR/universal$i.pid"
    if [ -f "$uv_pid_file" ]; then
        pid=$(cat "$uv_pid_file")
        if kill -0 "$pid" 2>/dev/null; then
            print_status "Stopping universal validator $i (PID: $pid)..."
            kill "$pid" 2>/dev/null || true
            rm -f "$uv_pid_file"
        fi
    fi
done

print_success "All validators stopped"
