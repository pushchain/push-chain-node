#!/bin/bash

# ---------------------------------------
# Push Chain NGINX + SSL Setup Script
# ---------------------------------------
# - Sets up NGINX to serve Cosmos and EVM RPCs
# - Bootstraps temporary HTTP config to fetch certs
# - Replaces config with SSL-enabled version
# ---------------------------------------

if [ -z "$1" ]; then
  echo "❌ Usage: ./setup_nginx.sh yourdomain.com"
  exit 1
fi

DOMAIN=$1
EVM_SUBDOMAIN="evm.$DOMAIN"
NGINX_CONFIG="/etc/nginx/sites-available/push-node"
TMP_CONFIG="/tmp/push-node-temp"
FINAL_CONFIG="/tmp/push-node-final"
WEBROOT="/var/www/certbot"

set -e

echo "🌐 Setting up NGINX for $DOMAIN and $EVM_SUBDOMAIN..."

# 📦 Install dependencies
sudo apt update
sudo apt install -y nginx certbot python3-certbot-nginx jq

# 📡 Allow required ports
sudo ufw allow 'Nginx Full'
sudo ufw allow 26656/tcp

# 📁 Ensure webroot exists
sudo mkdir -p "$WEBROOT"
sudo chown -R www-data:www-data "$WEBROOT"

# ⚙️ Write temporary HTTP-only config to serve .well-known
sudo tee "$TMP_CONFIG" > /dev/null <<EOF
server {
    listen 80;
    server_name $DOMAIN $EVM_SUBDOMAIN;

    location /.well-known/acme-challenge/ {
        root $WEBROOT;
    }

    location / {
        return 200 'Temporary NGINX setup for Certbot';
        add_header Content-Type text/plain;
    }
}
EOF

sudo cp "$TMP_CONFIG" "$NGINX_CONFIG"
sudo ln -sf "$NGINX_CONFIG" /etc/nginx/sites-enabled/push-node
sudo nginx -t && sudo systemctl reload nginx

# 🔐 Request certs via webroot method
sudo certbot certonly --webroot \
  -w "$WEBROOT" \
  -d "$DOMAIN" \
  -d "$EVM_SUBDOMAIN" \
  --non-interactive --agree-tos -m admin@$DOMAIN

# ✅ Write final SSL-enabled config
sudo tee "$FINAL_CONFIG" > /dev/null <<EOF
limit_req_zone \$binary_remote_addr zone=req_limit_per_ip:10m rate=5r/s;

# Cosmos RPC
server {
    listen 80;
    server_name $DOMAIN;
    return 301 https://\$host\$request_uri;
}

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

# EVM RPC
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
    listen 80;
    server_name $EVM_SUBDOMAIN;
    return 301 https://\$host\$request_uri;
}

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

sudo cp "$FINAL_CONFIG" "$NGINX_CONFIG"
sudo nginx -t && sudo systemctl reload nginx

# 🧪 Verify setup
curl -s https://$DOMAIN/status | jq '.result.sync_info.catching_up' || echo "⚠️ Cosmos RPC check failed"

EVM_RPC=$(curl -s -X POST https://$EVM_SUBDOMAIN -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}')
if echo "$EVM_RPC" | jq -e '.result' &>/dev/null; then
  echo "✅ EVM RPC (HTTPS) is live: https://$EVM_SUBDOMAIN"
else
  echo "⚠️ EVM RPC (HTTPS) not responding"
fi

echo "\n🚀 Setup complete!"
echo "🔗 Cosmos RPC: https://$DOMAIN"
echo "🔗 EVM RPC:    https://$EVM_SUBDOMAIN"
