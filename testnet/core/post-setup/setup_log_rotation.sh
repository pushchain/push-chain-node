#!/bin/bash

###############################################
# Push Chain Log Rotation Setup Script
#
# - Rotates logs under /home/app/logs/
# - Uses logrotate (daily, compress, 14-day retention)
# - Target: /home/app/logs/*.log
###############################################

LOG_DIR="/home/app/logs"
LOGROTATE_CONF="/etc/logrotate.d/push-chain"

# ---------------
# Install logrotate if missing
# ---------------
if ! command -v logrotate &> /dev/null; then
  echo "📦 Installing logrotate..."
  sudo apt-get update && sudo apt-get install -y logrotate
fi

# ---------------
# Create logrotate config
# ---------------
echo "🛠️ Creating logrotate config at $LOGROTATE_CONF..."

sudo tee "$LOGROTATE_CONF" > /dev/null <<EOF
$LOG_DIR/*.log {
    daily
    rotate 14
    compress
    delaycompress
    missingok
    notifempty
    create 0644 root root
    su root root
    sharedscripts
    postrotate
        systemctl reload nginx >/dev/null 2>&1 || true
    endscript
}
EOF

# ---------------
# Force test
# ---------------
echo "✅ Log rotation setup complete. Running dry run to test..."
sudo logrotate --debug "$LOGROTATE_CONF"
