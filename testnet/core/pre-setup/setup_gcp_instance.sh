#!/bin/bash

###############################################
# Push Chain GCP Node Setup Script
#
# This script does the following:
# 1. Installs system dependencies on the remote GCP VM
# 2. Installs Go (1.23.7) globally
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

REMOTE_APP_DIR="/home/app"
GO_VERSION="1.23.7"
GO_TAR="go${GO_VERSION}.linux-amd64.tar.gz"

# ---------------
# Install dependencies, install Go, prepare /home/app
# ---------------
echo "🔧 Installing dependencies and Go $GO_VERSION globally on remote VM..."
ssh "$SSH_USER@$EXTERNAL_IP" <<EOF
  set -e

  # Install system packages
  sudo apt-get update
  sudo apt-get install -y \
    build-essential \
    git \
    jq \
    python3 \
    python3-pip \
    curl \
    wget \
    netcat

  # Python dependencies
  pip3 install --upgrade pip
  pip3 install tomlkit

  # Install Go globally
  echo "📦 Installing Go $GO_VERSION globally..."
  wget https://go.dev/dl/${GO_TAR}
  sudo rm -rf /usr/local/go
  sudo tar -C /usr/local -xzf ${GO_TAR}
  rm ${GO_TAR}

  # Set Go env globally
  sudo tee /etc/profile.d/go.sh > /dev/null << 'EOL'
export PATH=/usr/local/go/bin:\$PATH
EOL
  sudo chmod +x /etc/profile.d/go.sh

  # Source immediately for current session
  export PATH=/usr/local/go/bin:\$PATH

  # Check version
  go version

  # Prepare /home/app
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
