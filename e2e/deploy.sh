#!/bin/bash

set -e

# Usage: ./deploy.sh devnet
CLUSTER="$1"

if [[ -z "$CLUSTER" ]]; then
  echo "❌ Please specify the cluster to deploy to (e.g., devnet, testnet, localnet, mainnet-beta)"
  exit 1
fi

# Optional: Custom keypair path (change this if needed)
KEYPAIR_PATH="$HOME/.config/solana/id.json"

echo "🔐 Using keypair: $KEYPAIR_PATH"

echo "📦 Building program..."
anchor build

echo "🚀 Deploying to $CLUSTER..."
solana config set --keypair "$KEYPAIR_PATH" --url "http://localhost:8899"

# Capture the output of anchor deploy
DEPLOY_OUTPUT=$(anchor deploy --provider.cluster "$CLUSTER" --provider.wallet "$KEYPAIR_PATH")

# Extract the program ID from the output
PROGRAM_ID=$(echo "$DEPLOY_OUTPUT" | grep "Program Id" | awk '{print $3}')

if [[ -z "$PROGRAM_ID" ]]; then
  echo "❌ Failed to extract Program ID from deploy output!"
  exit 1
fi

echo "🆔 Program ID: $PROGRAM_ID"

# Append program ID to .env files
echo "SOLANA_PROGRAM_ID=$PROGRAM_ID" >> ../../../.env
echo "SOLANA_PROGRAM_ID=$PROGRAM_ID" >> ../../../solana-setup/.env

cp target/idl/pushsolanalocker.json  ../../../solana-setup/pushsolanalocker.json
cp target/types/pushsolanalocker.ts  ../../../solana-setup/type_pushsolanalocker.ts

jq --arg addr "$PROGRAM_ID" '.gateway_address = $addr' ../../../solana_localchain_chain_config.json > tmp.json && mv tmp.json ../../../solana_localchain_chain_config.json


echo "✅ Successfully deployed to $CLUSTER"
