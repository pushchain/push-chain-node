#!/bin/bash
# Daemon launcher script - creates a truly detached process
# This script double-forks to create an orphaned process that won't receive signals

BINARY="$1"
HOME_DIR="$2"
PID_FILE="$3"
LOG_FILE="$4"

# First fork - parent exits immediately
(
    # Detach from terminal and create new session
    trap '' HUP INT TERM
    
    # Second fork - this becomes the daemon
    (
        # Close all file descriptors
        exec 0<&-
        exec 1>"$LOG_FILE"
        exec 2>&1
        
        # Start the actual process
        exec "$BINARY" start --home "$HOME_DIR"
    ) &
    
    # Save PID of the daemon
    echo $! > "$PID_FILE"
    
    # Exit the first fork
    exit 0
) &

# Parent exits immediately
exit 0