#!/bin/bash

# ---------------------------
# Push Chain NGINX + SSL Setup Script
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

# 🔧 Input validation
if [ -z "$1" ]; then
    echo "❌ Usage: ./setup_nginx.sh yourdomain.com"
    exit 1
fi

DOMAIN=$1
EVM_SUBDOMAIN="evm.$DOMAIN"
NGINX_CONFIG="/etc/nginx/sites-available/push-node"

echo "🌐 Setting up NGINX for $DOMAIN and $EVM_SUBDOMAIN..."

# 📡 Allow HTTP/HTTPS
sudo ufw allow 'Nginx Full'
sudo ufw allow 26656/tcp

# 🛠️ Install dependencies
sudo apt update
sudo apt install -y nginx certbot python3-certbot-nginx jq

# 🧾 Write NGINX config
sudo tee "$NGINX_CONFIG" > /dev/null <<EOF
limit_req_zone \$binary_remote_addr zone=req_limit_per_ip:10m rate=5r/s;

# Redirect HTTP → HTTPS (Cosmos RPC)
server {
    listen 80;
    server_name $DOMAIN;
    return 301 https://\$host\$request_uri;
}

# Cosmos RPC over HTTPS
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

# Redirect HTTP → HTTPS (EVM RPC)
server {
    listen 80;
    server_name $EVM_SUBDOMAIN;
    return 301 https://\$host\$request_uri;
}

# EVM upstreams
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

# EVM RPC over HTTPS
server {
    listen 443 ssl http2;
    server_name $EVM_SUBDOMAIN;

    ssl_certificate /etc/letsencrypt/live/$DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$DOMAIN/privkey.pem;

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

# 🔗 Enable and reload NGINX
sudo ln -sf "$NGINX_CONFIG" /etc/nginx/sites-enabled/push-node
sudo nginx -t && sudo systemctl reload nginx

# 🔐 Request SSL cert
echo "🔐 Requesting SSL certificates for $DOMAIN and $EVM_SUBDOMAIN..."
sudo certbot --nginx -d "$DOMAIN" -d "$EVM_SUBDOMAIN" --non-interactive --agree-tos -m admin@$DOMAIN

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
  echo "⚠️ Cosmos RPC not responding or still syncing: https://$DOMAIN/status"
fi

echo ""
echo "🔎 Verifying EVM RPC (HTTPS + WebSocket)..."

# -- JSON-RPC check via HTTPS (8545) --
EVM_RPC_CHECK=$(curl -s -X POST https://evm.$DOMAIN \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}')

if echo "$EVM_RPC_CHECK" | jq -e '.result' &>/dev/null; then
  echo "✅ EVM RPC (HTTPS) is live: https://evm.$DOMAIN"
else
  echo "⚠️ EVM RPC (HTTPS) not responding correctly. Check if node is running and port 8545 is open."
fi


echo ""
echo "🚀 Setup complete!"
echo "🔗 Cosmos RPC: https://$DOMAIN"
echo "🔗 EVM RPC:    https://$EVM_SUBDOMAIN"
