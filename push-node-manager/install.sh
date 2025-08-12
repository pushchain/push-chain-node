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
  git clone --depth 1 https://github.com/pushchain/push-chain-node "$REPO_DIR"
else
  echo "Repository already present; leaving as-is (no update)."
fi

# Build native binary and ensure manager script
echo "Building native binary and setting up manager..."
cd "$REPO_DIR/push-node-manager"
bash scripts/setup-dependencies.sh

# Link manager script to a stable path in $HOME
ln -sf "$PWD/push-node-manager" "$MANAGER_LINK"
chmod +x "$MANAGER_LINK"

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

echo "Installed. To manage the node, use: $MANAGER_LINK <command>"
echo "Examples:"
echo "  $MANAGER_LINK start"
echo "  $MANAGER_LINK status"

if [[ "$AUTO_START" = "yes" ]]; then
  "$MANAGER_LINK" start || true
  echo "Use: $MANAGER_LINK status"
fi


