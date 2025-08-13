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

# Use XDG Base Directory or fallback to hidden directory
if [[ -n "${XDG_DATA_HOME:-}" ]]; then
    ROOT_DIR="$XDG_DATA_HOME/push-node-manager"
else
    ROOT_DIR="$HOME/.local/share/push-node-manager"
fi
REPO_DIR="$ROOT_DIR/repo"
INSTALL_DIR="$ROOT_DIR/app"
MANAGER_LINK="$HOME/.local/bin/push-node-manager"

mkdir -p "$ROOT_DIR"
mkdir -p "$HOME/.local/bin"
cd "$ROOT_DIR"

echo "Installing Push Node Manager into $ROOT_DIR"

# Always clone fresh repository to ensure latest version
echo "Cloning repository..."
rm -rf "$REPO_DIR"
git clone --depth 1 --branch feature/validator-node-setup https://github.com/pushchain/push-chain-node "$REPO_DIR"

# Build native binary and ensure manager script
echo "Building native binary and setting up manager..."

# Copy manager to a stable install directory so we can delete the repo later
rm -rf "$INSTALL_DIR"
mkdir -p "$INSTALL_DIR"
if [[ ! -d "$REPO_DIR/push-node-manager" ]]; then
  echo "Error: missing source at $REPO_DIR/push-node-manager"
  exit 1
fi
cp -a "$REPO_DIR/push-node-manager/." "$INSTALL_DIR/"

cd "$INSTALL_DIR"
bash scripts/setup-dependencies.sh

# Ensure the push-node-manager script is executable
chmod +x "$INSTALL_DIR/push-node-manager"

# Create symlink for binary in expected location
mkdir -p build
ln -sf scripts/build/pchaind build/pchaind

# Remove any existing symlink/script and install a small launcher script
rm -f "$MANAGER_LINK"
cat > "$MANAGER_LINK" <<EOF
#!/usr/bin/env bash
exec "$INSTALL_DIR/push-node-manager" "\$@"
EOF
chmod +x "$MANAGER_LINK"

# Verify the script exists and is executable
if [[ ! -f "$INSTALL_DIR/push-node-manager" ]]; then
  echo "Error: push-node-manager script not found in $INSTALL_DIR"
  exit 1
fi

if [[ ! -x "$INSTALL_DIR/push-node-manager" ]]; then
  echo "Error: push-node-manager script is not executable"
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
        echo "export PATH=\"$HOME/.local/bin:\$PATH\"" >> "$SHELL_CONFIG"
        echo "Added push-node-manager to PATH in $SHELL_CONFIG"
    fi
    # Load the PATH immediately for this session
    export PATH="$HOME/.local/bin:$PATH"
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

# Run auto-start before cleanup to ensure wrapper script is available
if [[ "$AUTO_START" = "yes" ]]; then
  "$MANAGER_LINK" start || true
  echo "Use: push-node-manager status"
  echo
  echo "💡 Quick commands:"
  echo "  push-node-manager status    📊 Check node status"
  echo "  push-node-manager sync      📈 Monitor sync progress"
  echo "  push-node-manager help      ❓ Show all commands"
fi

# Optional: Clean up the cloned repository to save space (keep only push-node-manager)
cd "$ROOT_DIR"
if [[ -d "$REPO_DIR" ]]; then
    # Remove the temporary clone
    rm -rf "$REPO_DIR"
fi


