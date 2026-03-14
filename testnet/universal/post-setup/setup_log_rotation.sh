#!/bin/bash

###############################################
# Push Chain Log Rotation Setup Script
#
# - Rotates logs under /home/universal/logs/
# - Uses copytruncate (preserves systemd fd)
# - Size-based rotation (2G) with hourly cron
# - Keeps 3 compressed rotated files (~0.6GB)
###############################################

LOG_DIR="/home/universal/logs"
LOGROTATE_CONF="/etc/logrotate.d/push-universal"
LOGROTATE_STATE="/var/lib/logrotate/push-universal-state"
CRON_FILE="/etc/cron.d/push-universal-logrotate"

# ---------------
# Install logrotate if missing
# ---------------
if ! command -v logrotate &> /dev/null; then
  echo "Installing logrotate..."
  sudo apt-get update && sudo apt-get install -y logrotate
fi

# ---------------
# Create logrotate config
# ---------------
echo "Creating logrotate config at $LOGROTATE_CONF..."

sudo tee "$LOGROTATE_CONF" > /dev/null <<EOF
$LOG_DIR/pchaind.log {
    rotate 3
    size 2G
    compress
    missingok
    notifempty
    copytruncate
    su root root
}
EOF

# ---------------
# Add hourly cron to check rotation
# ---------------
echo "Setting up hourly cron at $CRON_FILE..."

sudo tee "$CRON_FILE" > /dev/null <<EOF
0 * * * * root /usr/sbin/logrotate $LOGROTATE_CONF --state $LOGROTATE_STATE
EOF

sudo chmod 644 "$CRON_FILE"

# ---------------
# Force test
# ---------------
echo "Log rotation setup complete. Running dry run to test..."
sudo logrotate --debug "$LOGROTATE_CONF"
