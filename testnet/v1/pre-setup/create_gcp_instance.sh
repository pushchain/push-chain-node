#!/bin/bash

###############################################
# Push Chain GCP Node Provisioning Script
#
# This script does the following:
# 1. Prompts for instance name, SSH user, and key
# 2. Ensures required network and subnet exist, if not then create it
# 4. Creates the VM instance
# 5. Adds your SSH key to the instance
#
# Prerequisites:
# - gcloud CLI authenticated (`gcloud auth login`)
# - SSH keypair generated locally
# - Project: push-chain-testnet
###############################################

# ---------------
# Input prompts
# ---------------
read -p "🖥️ Enter instance name (e.g. donut-node1): " INSTANCE_NAME
read -p "👤 Enter SSH username to use: " SSH_USER
read -p "🔑 Enter SSH Public key: " SSH_PUB_KEY

# ---------------
# Constants
# ---------------
PROJECT_ID="push-chain-testnet"
ZONE="us-central1-a"
MACHINE_TYPE="custom-4-10496"
IMAGE_FAMILY="ubuntu-2204-lts"
IMAGE_PROJECT="ubuntu-os-cloud"
DISK_SIZE="200"
DISK_TYPE="pd-ssd"
NETWORK="cosmos-network"
SUBNET="cosmos-network"
REMOTE_HOME="/home/app"

# ---------------
# Ensure network exists
# ---------------
if ! gcloud compute networks describe "$NETWORK" --project="$PROJECT_ID" &>/dev/null; then
  echo "🌐 Creating VPC network: $NETWORK"
  gcloud compute networks create "$NETWORK" \
    --subnet-mode=custom \
    --project="$PROJECT_ID"
fi

if ! gcloud compute networks subnets describe "$SUBNET" --region=us-central1 --project="$PROJECT_ID" &>/dev/null; then
  echo "📡 Creating subnet: $SUBNET"
  gcloud compute networks subnets create "$SUBNET" \
    --network="$NETWORK" \
    --range="10.128.0.0/20" \
    --region=us-central1 \
    --project="$PROJECT_ID"
fi

# ---------------
# Create instance
# ---------------
echo "🚀 Creating GCP instance: $INSTANCE_NAME ..."
gcloud compute instances create "$INSTANCE_NAME" \
  --project="$PROJECT_ID" \
  --zone="$ZONE" \
  --machine-type="$MACHINE_TYPE" \
  --image-family="$IMAGE_FAMILY" \
  --image-project="$IMAGE_PROJECT" \
  --boot-disk-size="$DISK_SIZE" \
  --boot-disk-type="$DISK_TYPE" \
  --network="$NETWORK" \
  --subnet="$SUBNET" \
  --tags=cosmos-p2p,http-server,https-server \
  --maintenance-policy=MIGRATE \
  --shielded-vtpm \
  --shielded-integrity-monitoring \
  --scopes=https://www.googleapis.com/auth/cloud-platform

# ---------------
# Whitelist IPv4 public IP (append to list)
# ---------------
MY_IP=$(curl -s https://ipv4.icanhazip.com)
MY_IP=$(echo "$MY_IP" | tr -d '\n')

if [ -z "$MY_IP" ]; then
  echo "❌ Failed to detect public IPv4 address"
  exit 1
fi

echo "🔐 Whitelisting IP $MY_IP for SSH..."

# Fetch current source ranges
EXISTING_RANGES=$(gcloud compute firewall-rules describe allow-ssh \
  --project="$PROJECT_ID" \
  --format="value(sourceRanges)" | tr ';' '\n')

# Append new IP if not already present
if echo "$EXISTING_RANGES" | grep -q "$MY_IP/32"; then
  echo "ℹ️ IP $MY_IP/32 already whitelisted"
else
  UPDATED_RANGES=$(echo -e "$EXISTING_RANGES\n$MY_IP/32" | sort -u | paste -sd "," -)

  gcloud compute firewall-rules update allow-ssh \
    --source-ranges="$UPDATED_RANGES" \
    --project="$PROJECT_ID" \
    --quiet

  echo "✅ IP $MY_IP/32 added to allow-ssh"
fi

# ---------------
# Add SSH key to instance metadata
# ---------------
echo "🔑 Adding SSH key for $SSH_USER to instance metadata..."
gcloud compute instances add-metadata "$INSTANCE_NAME" \
  --zone="$ZONE" \
  --metadata=ssh-keys="$SSH_USER:$SSH_PUB_KEY" \
  --project="$PROJECT_ID"

# ---------------
# Done
# ---------------
echo "✅ Instance '$INSTANCE_NAME' setup complete."
