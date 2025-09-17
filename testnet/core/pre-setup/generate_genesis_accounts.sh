#!/bin/bash

########################################################
# Generate N Push Chain Accounts using `pchaind keys add`
# Prerequisites:
# - pchaind binary must be in your PATH
# - jq installed
# - This uses `--keyring-backend test`, DO NOT use in prod
########################################################

NUM_ACCOUNTS=${1:-5}
KEYRING="test"  # For dev only

echo "🔐 Generating $NUM_ACCOUNTS Push Chain accounts..."

for ((i=1; i<=NUM_ACCOUNTS; i++)); do
  KEY_NAME="genesis$i"

  # Delete existing key if it exists
  if pchaind keys show "$KEY_NAME" --keyring-backend="$KEYRING" > /dev/null 2>&1; then
    echo "⚠️  Key $KEY_NAME already exists. Overwriting..."
    pchaind keys delete "$KEY_NAME" --keyring-backend="$KEYRING" -y > /dev/null
  fi

  OUTPUT=$(pchaind keys add "$KEY_NAME" --keyring-backend="$KEYRING" --output=json 2>/dev/null)

  MNEMONIC=$(echo "$OUTPUT" | jq -r '.mnemonic')
  ADDRESS=$(echo "$OUTPUT" | jq -r '.address')

  echo "🧾 Account #$i"
  echo "  Name     : $KEY_NAME"
  echo "  Address  : $ADDRESS"
  echo "  Mnemonic : $MNEMONIC"
  echo
done
