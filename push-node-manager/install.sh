#!/usr/bin/env bash

# Push Node Manager one-liner installer (source-build only, no CI required)
# Usage examples:
#   curl -fsSL https://get.push.network/pnm/install.sh | bash
#   MONIKER=my-node GENESIS_DOMAIN=rpc-testnet-donut-node1.push.org KEYRING_BACKEND=os \
#     curl -fsSL https://get.push.network/pnm/install.sh | bash
#   curl -fsSL https://get.push.network/pnm/install.sh | bash -s -- --no-start

set -euo pipefail
IFS=$'\n\t'

# Read env or defaults
MONIKER="${MONIKER:-push-validator}"
GENESIS_DOMAIN="${GENESIS_DOMAIN:-rpc-testnet-donut-node1.push.org}"
KEYRING_BACKEND="${KEYRING_BACKEND:-test}"
AUTO_START="yes"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-start) AUTO_START="no"; shift ;;
    --start) AUTO_START="yes"; shift ;;
    --moniker) MONIKER="$2"; shift 2 ;;
    --genesis) GENESIS_DOMAIN="$2"; shift 2 ;;
    --keyring) KEYRING_BACKEND="$2"; shift 2 ;;
    *) echo "Unknown flag: $1" >&2; exit 1 ;;
  esac
done

require_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "Missing dependency: $1" >&2; exit 1; }; }
for c in curl jq git tar; do require_cmd "$c"; done

ROOT_DIR="$HOME/push-node-manager"
REPO_DIR="$ROOT_DIR/repo"
MANAGER_LINK="$ROOT_DIR/push-node-manager"

mkdir -p "$ROOT_DIR"
cd "$ROOT_DIR"

echo "Installing Push Node Manager into $ROOT_DIR"

# Shallow clone if repo missing
if [[ ! -d "$REPO_DIR/.git" ]]; then
  echo "Cloning repository..."
  git clone --depth 1 --branch feature/validator-node-setup https://github.com/pushchain/push-chain-node "$REPO_DIR"
else
  echo "Repository already present; leaving as-is (no update)."
fi

# Build native binary and ensure manager script
echo "Building native binary and setting up manager..."
cd "$REPO_DIR/push-node-manager"
bash scripts/setup-dependencies.sh

# Create symlink for binary in expected location
mkdir -p build
ln -sf scripts/build/pchaind build/pchaind

# Link manager script to a stable path in $HOME
ln -sf "$PWD/push-node-manager" "$MANAGER_LINK"
chmod +x "$MANAGER_LINK"

# Verify the script exists
if [[ ! -f "$PWD/push-node-manager" ]]; then
  echo "Error: push-node-manager script not found in $PWD"
  exit 1
fi

# Add to PATH if not already there
SHELL_CONFIG=""
if [[ -f "$HOME/.zshrc" ]]; then
    SHELL_CONFIG="$HOME/.zshrc"
elif [[ -f "$HOME/.bashrc" ]]; then
    SHELL_CONFIG="$HOME/.bashrc"
elif [[ -f "$HOME/.bash_profile" ]]; then
    SHELL_CONFIG="$HOME/.bash_profile"
fi

if [[ -n "$SHELL_CONFIG" ]]; then
    if ! grep -q "push-node-manager" "$SHELL_CONFIG" 2>/dev/null; then
        echo "" >> "$SHELL_CONFIG"
        echo "# Push Node Manager" >> "$SHELL_CONFIG"
        echo "export PATH=\"$ROOT_DIR:\$PATH\"" >> "$SHELL_CONFIG"
        echo "Added push-node-manager to PATH in $SHELL_CONFIG"
        echo "Run: source $SHELL_CONFIG  (or restart terminal)"
    fi
fi

# Persist configuration
ENV_FILE="$ROOT_DIR/.env"
tmp="$ENV_FILE.tmp"; : > "$tmp"
{ grep -v -e '^GENESIS_DOMAIN=' -e '^MONIKER=' -e '^KEYRING_BACKEND=' "$ENV_FILE" 2>/dev/null || true; } >> "$tmp"
mv "$tmp" "$ENV_FILE"
{
  echo "GENESIS_DOMAIN=$GENESIS_DOMAIN"
  echo "MONIKER=$MONIKER"
  echo "KEYRING_BACKEND=$KEYRING_BACKEND"
} >> "$ENV_FILE"

echo "Installed. To manage the node, use: push-node-manager <command>"
echo "Examples:"
echo "  push-node-manager start"
echo "  push-node-manager status"
if [[ -n "$SHELL_CONFIG" ]]; then
    echo ""
    echo "Note: If 'push-node-manager' command not found, restart your terminal or run:"
    echo "  source $SHELL_CONFIG"
fi

if [[ "$AUTO_START" = "yes" ]]; then
  "$MANAGER_LINK" start || true
  echo "Use: push-node-manager status"
fi

# Optional: Clean up the cloned repository to save space (keep only push-node-manager)
echo "Cleaning up temporary build files..."
cd "$ROOT_DIR"
if [[ -d "$REPO_DIR" ]]; then
    # Copy essential files only (avoid copying broken symlinks)
    cp "$REPO_DIR/push-node-manager/push-node-manager" ./
    cp -r "$REPO_DIR/push-node-manager/scripts" ./
    cp -r "$REPO_DIR/push-node-manager/tests" ./
    # Copy binary to expected location
    mkdir -p build
    cp "$REPO_DIR/push-node-manager/scripts/build/pchaind" build/pchaind
    # Update symlink to point to new location
    ln -sf "$ROOT_DIR/push-node-manager" "$MANAGER_LINK"
    # Remove the temporary clone
    rm -rf "$REPO_DIR"
    echo "Repository cleanup complete"
fi


