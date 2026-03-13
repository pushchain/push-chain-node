#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd -P "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

ENV_FILE="e2e-tests/.env"
if [[ ! -f "$ENV_FILE" ]]; then
  echo "e2e-tests/.env not found" >&2
  exit 1
fi

PRIVATE_KEY=$(grep '^PRIVATE_KEY=' "$ENV_FILE" | cut -d= -f2 | tr -d '"' | tr -d "'")
if [[ -z "$PRIVATE_KEY" ]]; then
  echo "PRIVATE_KEY not found in $ENV_FILE" >&2
  exit 1
fi

# Derive EVM address
if ! command -v cast >/dev/null 2>&1; then
  echo "cast command not found (install foundry/cast)" >&2
  exit 1
fi
EVM_ADDRESS=$(cast wallet address $PRIVATE_KEY)

# Derive push (cosmos) address
if ! command -v $PWD/build/pchaind >/dev/null 2>&1; then
  echo "pchaind binary not found in build/ (run make build)" >&2
  exit 1
fi
PUSH_ADDRESS=push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20

echo "Replacing with PUSH_ADDRESS: $PUSH_ADDRESS"
echo "Replacing with EVM_ADDRESS: $EVM_ADDRESS"

# Replace Admin in params.go files
for f in x/utss/types/params.go x/uregistry/types/params.go x/uvalidator/types/params.go; do
  if [[ -f "$f" ]]; then
    perl -pi -e "s/Admin: \"push1[0-9a-z]+\"/Admin: \"$PUSH_ADDRESS\"/g" "$f"
    echo "Updated Admin in $f"
  fi
done

# Replace PROXY_ADMIN_OWNER_ADDRESS in constants.go files
for f in x/uexecutor/types/constants.go x/uregistry/types/constants.go; do
  if [[ -f "$f" ]]; then
    perl -pi -e "s/PROXY_ADMIN_OWNER_ADDRESS_HEX = \"0x[a-fA-F0-9]{40}\"/PROXY_ADMIN_OWNER_ADDRESS_HEX = \"$EVM_ADDRESS\"/g" "$f"
    perl -pi -e "s/PROXY_ADMIN_OWNER_ADDRESS = \"0x[a-fA-F0-9]{40}\"/PROXY_ADMIN_OWNER_ADDRESS = \"$EVM_ADDRESS\"/g" "$f"
    echo "Updated PROXY_ADMIN_OWNER_ADDRESS in $f"
  fi
done

echo "Address replacement completed."
