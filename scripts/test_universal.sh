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

sleep 1

if command -v "$PCHAIN_BIN" >/dev/null 2>&1; then
  echo "==> Granting authz (generic: /uexecutor.v1.MsgVoteGasPrice) to $UV_HOTKEY_ADDR via $PCHAIN_BIN"
  "$PCHAIN_BIN" tx authz grant "$UV_HOTKEY_ADDR" generic \
    --msg-type=/uexecutor.v1.MsgVoteGasPrice \
    --from acc1 \
    --fees "$FEES" \
    -y
else
  echo "WARN: $PCHAIN_BIN not found; skipping authz grant."
fi

sleep 1

if command -v "$PCHAIN_BIN" >/dev/null 2>&1; then
  echo "==> Granting authz (generic: /uexecutor.v1.MsgVoteOutbound) to $UV_HOTKEY_ADDR via $PCHAIN_BIN"
  "$PCHAIN_BIN" tx authz grant "$UV_HOTKEY_ADDR" generic \
    --msg-type=/uexecutor.v1.MsgVoteOutbound \
    --from acc1 \
    --fees "$FEES" \
    -y
else
  echo "WARN: $PCHAIN_BIN not found; skipping authz grant."
fi

sleep 1

if command -v "$PCHAIN_BIN" >/dev/null 2>&1; then
  echo "==> Granting authz (generic: /utss.v1.MsgVoteTssKeyProcess) to $UV_HOTKEY_ADDR via $PCHAIN_BIN"
  "$PCHAIN_BIN" tx authz grant "$UV_HOTKEY_ADDR" generic \
    --msg-type=/utss.v1.MsgVoteTssKeyProcess \
    --from acc1 \
    --fees "$FEES" \
    -y
else
  echo "WARN: $PCHAIN_BIN not found; skipping authz grant."
fi

sleep 1

# ---------- Initialize and start ----------
echo "==> Initializing $BINARY..."
"$BINARY" init

echo "==> Starting $BINARY..."
exec "$BINARY" start
