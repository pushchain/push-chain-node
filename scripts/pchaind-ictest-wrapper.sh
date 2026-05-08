#!/bin/sh
# pchaind-ictest-wrapper
#
# A thin wrapper around `pchaind` used ONLY by interchaintest e2e tests.
# Production never invokes this script.
#
# Why it exists:
#   uregistry/utss/uvalidator have empty Admin in DefaultParams (audit fix
#   F-2026-16648). Their genesis Validate() rejects empty admin so mainnet
#   operators must explicitly set it via genesis script — they cannot
#   accidentally ship without an admin.
#
#   `pchaind genesis gentx` calls validate-genesis internally as its first
#   step, so any genesis-modify hook strangelove exposes (ModifyGenesis,
#   PreGenesis, etc.) all run AFTER gentx has already failed. The only
#   place we can safely inject admin is right BEFORE gentx runs.
#
# What it does:
#   Intercepts `pchaind genesis gentx <key_name> ...`. Before delegating to
#   the real pchaind, it resolves the validator key's bech32 address and
#   patches the three modules' admin field in genesis.json with it. Then it
#   runs the real gentx, which now passes validate-genesis.
#
#   Every other command is a transparent passthrough.

set -e

PCHAIND=/usr/bin/pchaind

# Only `genesis gentx` needs special handling.
if [ "$1" != "genesis" ] || [ "$2" != "gentx" ]; then
    exec "$PCHAIND" "$@"
fi

# `pchaind genesis gentx <key_name> <amount> [flags...]`
KEY_NAME="$3"

# Extract --home and --keyring-backend from the gentx args (strangelove
# always passes both).
HOME_DIR=""
KEYRING="test"
prev=""
for arg in "$@"; do
    case "$prev" in
        --home) HOME_DIR="$arg" ;;
        --keyring-backend) KEYRING="$arg" ;;
    esac
    case "$arg" in
        --home=*) HOME_DIR="${arg#--home=}" ;;
        --keyring-backend=*) KEYRING="${arg#--keyring-backend=}" ;;
    esac
    prev="$arg"
done
if [ -z "$HOME_DIR" ]; then
    HOME_DIR="${HOME:-/root}/.pchain"
fi

GENESIS="$HOME_DIR/config/genesis.json"

# Resolve the validator key's bech32 address. If anything goes wrong (key
# missing, jq missing, genesis missing) we just fall through and let the
# real gentx run — it will produce its own error.
ADMIN_ADDR=$("$PCHAIND" keys show "$KEY_NAME" --address \
    --keyring-backend "$KEYRING" --home "$HOME_DIR" 2>/dev/null || true)

if [ -n "$ADMIN_ADDR" ] && [ -f "$GENESIS" ]; then
    TMP=$(mktemp)
    jq --arg admin "$ADMIN_ADDR" \
        '.app_state.uregistry.params.admin = $admin
        | .app_state.utss.params.admin = $admin
        | .app_state.uvalidator.params.admin = $admin' \
        "$GENESIS" > "$TMP"
    mv "$TMP" "$GENESIS"
fi

exec "$PCHAIND" "$@"
