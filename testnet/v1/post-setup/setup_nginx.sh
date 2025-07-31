#!/bin/bash

# ---------------------------
# ✅ Prerequisites (System Requirements)
# ---------------------------
# - Ubuntu/Debian-based Linux OS
# - DNS A records set:
#     - YOUR_DOMAIN → your VM's public IP
#     - evm.YOUR_DOMAIN → your VM's public IP
# - Node already running with:
#     - Tendermint RPC on port 26657
#     - EVM HTTP RPC on port 8545
#     - EVM WebSocket RPC on port 8546
# - jq installed: `sudo apt install jq`

# ---------------------------
# 🔧 Usage
# ---------------------------
#   bash setup_nginx.sh your-domain.com
# ---------------------------

if [ -z "$1" ]; then
    echo "❌ Usage: ./setup_nginx.sh your-domain.com"
    exit 1
fi

DOMAIN=$1

# 📡 Allow HTTP/HTTPS traffic
sudo ufw allow 80
sudo ufw allow 443

# 🛠️ Install dependencies
sudo apt update
sudo apt install -y nginx certbot python3-certbot-nginx jq

# 🧾 Create Nginx config
cat > /tmp/push-node-nginx << EOF
limit_req_zone \$binary_remote_addr zone=req_limit_per_ip:10m rate=5r/s;

# Redirect HTTP to HTTPS for main domain
server {
    listen 80;
    server_name $DOMAIN;
    return 301 https://\$host\$request_uri;
}

# Serve Cosmos RPC at https://<domain>
server {
    listen 443 ssl http2;
    server_name $DOMAIN;

    ssl_certificate /etc/letsencrypt/live/$DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$DOMAIN/privkey.pem;

    location / {
        limit_req zone=req_limit_per_ip burst=10 nodelay;

        proxy_pass http://localhost:26657;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
    }
}

# Redirect HTTP to HTTPS for EVM
server {
    listen 80;
    server_name evm.$DOMAIN;
    return 301 https://\$host\$request_uri;
}

# Serve EVM RPC at https://evm.<domain>
upstream http_backend {
    server 127.0.0.1:8545;
}

upstream ws_backend {
    server 127.0.0.1:8546;
}

map \$http_upgrade \$connection_upgrade {
    default upgrade;
    '' close;
}

server {
    listen 443 ssl http2;
    server_name evm.$DOMAIN;

    ssl_certificate /etc/letsencrypt/live/evm.$DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/evm.$DOMAIN/privkey.pem;

    location / {
        limit_req zone=req_limit_per_ip burst=10 nodelay;

        set \$backend http://http_backend;
        if (\$http_upgrade = "websocket") {
            set \$backend http://ws_backend;
        }

        proxy_pass \$backend;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection \$connection_upgrade;
        proxy_set_header Host localhost;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
    }
}
EOF

# 🔁 Copy config and enable site
sudo cp /tmp/push-node-nginx /etc/nginx/sites-available/push-node
sudo ln -sf /etc/nginx/sites-available/push-node /etc/nginx/sites-enabled/

# 🔄 Reload nginx
sudo nginx -t && sudo systemctl reload nginx

# 🔐 Setup HTTPS with Let's Encrypt
sudo certbot --nginx -d "$DOMAIN" -d "evm.$DOMAIN" --non-interactive --agree-tos -m admin@"$DOMAIN"

# 🔁 Check certbot auto-renewal
echo ""
echo "⏰ Checking certbot auto-renewal..."
sudo systemctl list-timers | grep certbot || echo "⚠️ Certbot auto-renewal timer not found."

# 🔍 Verify endpoints
echo ""
echo "🔎 Verifying endpoint availability..."

RPC_STATUS=$(curl -s https://$DOMAIN/status | jq '.result.sync_info.catching_up' 2>/dev/null || echo "unreachable")
if [[ "$RPC_STATUS" == "false" ]]; then
  echo "✅ Cosmos RPC is synced and reachable: https://$DOMAIN/status"
else
  echo "⚠️ Cosmos RPC not responding correctly or still syncing: https://$DOMAIN/status"
fi

EVM_STATUS=$(curl -s https://evm.$DOMAIN -m 5)
if echo "$EVM_STATUS" | grep -q "jsonrpc"; then
  echo "✅ EVM RPC is live: https://evm.$DOMAIN"
else
  echo "⚠️ EVM RPC not responding correctly. Check if port 8545/8546 is open and node is running."
fi

echo ""
echo "🚀 Setup complete!"
echo "🔗 Cosmos RPC: https://$DOMAIN"
echo "🔗 EVM RPC:    https://evm.$DOMAIN"
