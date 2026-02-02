#!/bin/bash

###############################################
# Push Chain Node Management Script (Cosmovisor)
#
# Unified interface for managing the node using
# systemd with Cosmovisor for automatic binary
# upgrade management.
#
# Usage: ./node.sh {setup|start|stop|restart|status|logs}
###############################################

# Resolve base directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

LOG_FILE="$APP_DIR/logs/pchaind.log"
SERVICE_NAME="pchaind"
BINARY="$APP_DIR/binary/pchaind"
NODE_HOME="$APP_DIR/.pchain"
UPGRADE_NAME="remove-fee-abs-v1"

# Setup cosmovisor and directory structure
do_setup() {
  echo "🔧 Setting up Cosmovisor..."

  # Add Go to PATH
  export PATH="$PATH:/usr/local/go/bin"
  export GOPATH="${GOPATH:-$HOME/go}"
  export PATH="$PATH:$GOPATH/bin"

  # Check if Go is installed
  if ! command -v go &> /dev/null; then
    echo "❌ Go not found. Please install Go first."
    exit 1
  fi

  # Install cosmovisor if not present
  if ! command -v cosmovisor &> /dev/null; then
    echo "📦 Installing cosmovisor..."
    go install cosmossdk.io/tools/cosmovisor/cmd/cosmovisor@latest
    if [ $? -ne 0 ]; then
      echo "❌ Failed to install cosmovisor"
      exit 1
    fi
    echo "✅ Cosmovisor installed"
  else
    echo "✅ Cosmovisor already installed: $(which cosmovisor)"
  fi

  # Setup cosmovisor directory structure
  COSMOVISOR_DIR="$NODE_HOME/cosmovisor"
  GENESIS_BIN_DIR="$COSMOVISOR_DIR/genesis/bin"
  UPGRADE_BIN_DIR="$COSMOVISOR_DIR/upgrades/$UPGRADE_NAME/bin"

  echo "📁 Setting up directory structure..."
  mkdir -p "$GENESIS_BIN_DIR"
  mkdir -p "$UPGRADE_BIN_DIR"

  # Copy binary to genesis and upgrade directories
  if [ -f "$BINARY" ]; then
    cp "$BINARY" "$GENESIS_BIN_DIR/pchaind"
    chmod +x "$GENESIS_BIN_DIR/pchaind"
    cp "$BINARY" "$UPGRADE_BIN_DIR/pchaind"
    chmod +x "$UPGRADE_BIN_DIR/pchaind"
    echo "✅ Binary copied to cosmovisor directories"
  else
    echo "❌ Binary not found at: $BINARY"
    exit 1
  fi

  # Set current symlink to upgrade
  rm -f "$COSMOVISOR_DIR/current"
  ln -s "upgrades/$UPGRADE_NAME" "$COSMOVISOR_DIR/current"
  echo "✅ Symlink created: current -> upgrades/$UPGRADE_NAME"

  # Show cosmovisor path for systemd
  COSMOVISOR_PATH=$(which cosmovisor)
  echo ""
  echo "📋 Setup complete!"
  echo "   Cosmovisor path: $COSMOVISOR_PATH"
  echo ""
  echo "   Update /etc/systemd/system/pchaind.service with:"
  echo "   ExecStart=$COSMOVISOR_PATH run start ..."
}

# Start the node
do_start() {
  # Run setup first to ensure cosmovisor is installed
  do_setup

  echo "🚀 Starting node with Cosmovisor..."
  systemctl start $SERVICE_NAME
  sleep 2
  systemctl status $SERVICE_NAME --no-pager | head -10
}

# Stop the node
do_stop() {
  echo "🛑 Stopping node..."
  systemctl stop $SERVICE_NAME
  echo "✅ Stop command completed."
}

# Show node status
do_status() {
  systemctl status $SERVICE_NAME --no-pager
}

# Show logs
do_logs() {
  if [ -f "$LOG_FILE" ]; then
    tail -f "$LOG_FILE"
  else
    echo "❌ Log file not found: $LOG_FILE"
    echo "Try: journalctl -u $SERVICE_NAME -f"
    exit 1
  fi
}

# Main
case "$1" in
  start)
    do_start
    ;;
  stop)
    do_stop
    ;;
  restart)
    echo "🔄 Restarting node..."
    systemctl restart $SERVICE_NAME
    sleep 2
    systemctl status $SERVICE_NAME --no-pager | head -10
    ;;
  status)
    do_status
    ;;
  logs)
    do_logs
    ;;
  *)
    echo "Push Chain Node Manager (Cosmovisor)"
    echo ""
    echo "Usage: $0 {start|stop|restart|status|logs}"
    echo ""
    echo "Commands:"
    echo "  start   - Setup cosmovisor (if needed) and start the node"
    echo "  stop    - Stop the node"
    echo "  restart - Restart the node"
    echo "  status  - Show node status (systemctl)"
    echo "  logs    - Tail the log file"
    exit 1
    ;;
esac
