#!/bin/bash

###############################################
# Push Chain GCP Node Setup Script
#
# This script does the following:
# 3. Prepares /home/app and sets correct ownership
# 4. Copies binary/, setup/, post-setup/ to /home/app
#
# Prerequisites:
# - Your IP must be whitelisted in GCP firewall
# - You must be able to SSH/SCP into the instance
# - Local dirs: binary/, setup/, post-setup/ must exist
###############################################

# ---------------
# Input prompts
# ---------------
read -p "🖥️ Enter GCP Instance External IP: " EXTERNAL_IP
read -p "👤 Enter SSH username: " SSH_USER

REMOTE_APP_DIR="/home/universal"

# ---------------
#  prepare REMOTE_APP_DIR
# ---------------
ssh "$SSH_USER@$EXTERNAL_IP" <<EOF
  set -e
  # Prepare REMOTE_APP_DIR
  sudo rm -rf $REMOTE_APP_DIR
  sudo mkdir -p $REMOTE_APP_DIR
  sudo chown -R $SSH_USER:$SSH_USER $REMOTE_APP_DIR
EOF

# ---------------
# Copy local dirs to remote /home/app
# ---------------
echo "📦 Copying binary/, setup/, post-setup/ to $REMOTE_APP_DIR ..."
scp -r binary setup post-setup "$SSH_USER@$EXTERNAL_IP:$REMOTE_APP_DIR"

echo "✅ Setup complete."
echo "👉 SSH into the node with:"
echo "   ssh $SSH_USER@$EXTERNAL_IP"
echo "   cd $REMOTE_APP_DIR"
