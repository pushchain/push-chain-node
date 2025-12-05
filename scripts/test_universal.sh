#!/bin/bash
# Universal Validator Setup Script
set -euo pipefail

# ---------- Configurable defaults ----------
export HOME_DIR="${HOME_DIR:-"$HOME/.puniversal"}"
export CLEAN="${CLEAN:-"true"}"

export KEYALGO="${KEYALGO:-eth_secp256k1}"
export KEYRING="${KEYRING:-test}"
export BINARY="${BINARY:-puniversald}"

# Optional chain / fee config for pchaind (used for funding/authz)
export PCHAIN_BIN="${PCHAIN_BIN:-pchaind}"
export CHAIN_ID="${CHAIN_ID:-push-localnet}"
export GAS_PRICES="${GAS_PRICES:-1000000000000upc}"
export FEES="${FEES:-200000000000000upc}"
export BROADCAST_MODE="${BROADCAST_MODE:-async}"
export NODE_RPC="${NODE_RPC:-http://localhost:26657}"

echo "==> Setting up $BINARY with home: $HOME_DIR"

# ---------- Build / install ----------
if [[ "$CLEAN" == "true" ]] || ! command -v "$BINARY" >/dev/null 2>&1; then
  echo "==> Building and installing $BINARY..."
  make install
    if [ -z `which puniversald` ]; then
        echo "Ensure puniversald is installed and in your PATH"
        exit 1
    fi
fi

# ---------- Clean home if requested ----------
if [[ "$CLEAN" != "false" ]]; then
  echo "==> Removing existing home: $HOME_DIR"
  rm -rf "$HOME_DIR"
fi

# ---------- Helper: add key from mnemonic ----------
  add_key() {
    key=$1
    mnemonic=$2
    echo $mnemonic | $BINARY keys add $key --keyring-backend $KEYRING --algo $KEYALGO --recover
  }

# ---------- Create UV hot key ----------
# (address shown in comment is informational; we always derive from keyring below)
UV_HOTKEY_NAME="uv_hotkey"
UV_HOTKEY_MNEMONIC="little flower nasty exhibit exact trap frame fiscal ritual since picnic journey wrist bracket armor advice assume blur cause window open shadow middle owner"

add_key "$UV_HOTKEY_NAME" "$UV_HOTKEY_MNEMONIC"

UV_HOTKEY_ADDR="$("$BINARY" keys show "$UV_HOTKEY_NAME" -a \
  --home "$HOME_DIR" --keyring-backend "$KEYRING")"
echo "==> uv_hotkey address: $UV_HOTKEY_ADDR"


# ---------- (Optional) Grant authz to uv_hotkey via pchaind ----------
if command -v "$PCHAIN_BIN" >/dev/null 2>&1; then
  echo "==> Granting authz (generic: /uexecutor.v1.MsgVoteInbound) to $UV_HOTKEY_ADDR via $PCHAIN_BIN"
  "$PCHAIN_BIN" tx authz grant "$UV_HOTKEY_ADDR" generic \
    --msg-type=/uexecutor.v1.MsgVoteInbound \
    --from acc1 \
    --fees "$FEES" \
    -y
else
  echo "WARN: $PCHAIN_BIN not found; skipping authz grant."
fi

# ---------- Initialize and start ----------
echo "==> Initializing $BINARY..."
"$BINARY" init

# ---------- Enable TSS if requested ----------
TSS_ENABLED="true"
if [[ "$TSS_ENABLED" == "true" ]]; then
  echo "==> Enabling TSS in config..."
  CONFIG_FILE="$HOME_DIR/config/pushuv_config.json"

  # TSS test private key (32-byte hex seed for ed25519)
  # This generates a deterministic libp2p peer ID for testing
  TSS_PRIVATE_KEY="${TSS_PRIVATE_KEY:-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef}"
  TSS_PASSWORD="${TSS_PASSWORD:-testpassword}"
  TSS_P2P_LISTEN="${TSS_P2P_LISTEN:-/ip4/0.0.0.0/tcp/39000}"
  TSS_HOME_DIR="${TSS_HOME_DIR:-$HOME_DIR/tss}"

  # Update config using jq
  if command -v jq >/dev/null 2>&1; then
    jq --arg pk "$TSS_PRIVATE_KEY" \
       --arg pw "$TSS_PASSWORD" \
       --arg listen "$TSS_P2P_LISTEN" \
       --arg home "$TSS_HOME_DIR" \
       '.tss_enabled = true | .tss_private_key_hex = $pk | .tss_password = $pw | .tss_p2p_listen = $listen | .tss_home_dir = $home' \
       "$CONFIG_FILE" > "$CONFIG_FILE.tmp" && mv "$CONFIG_FILE.tmp" "$CONFIG_FILE"
    echo "✅ TSS enabled with P2P listen: $TSS_P2P_LISTEN"
  else
    echo "WARN: jq not found; cannot enable TSS. Install jq to enable TSS."
  fi
fi

echo "==> Starting $BINARY..."
exec "$BINARY" start
