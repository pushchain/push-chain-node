#!/bin/bash

# ---------------------------------------
# Push Chain NGINX + SSL Setup Script
# One main domain for Cosmos RPC, REST (/rest), gRPC (/grpc), gRPC-Web (/grpc-web)
# Separate subdomain only for EVM RPC (evm.$DOMAIN)
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

echo "🌐 NGINX will serve:"
echo "   - Cosmos RPC:  https://$DOMAIN/          -> 127.0.0.1:26657"
echo "   - REST (LCD):  https://$DOMAIN/rest/...  -> 127.0.0.1:1317"
echo "   - gRPC:        https://$DOMAIN/grpc      -> 127.0.0.1:9090 (HTTP/2)"
echo "   - gRPC-Web:    https://$DOMAIN/grpc-web/ -> 127.0.0.1:9091 (optional)"
echo "   - EVM RPC:     https://$EVM_SUBDOMAIN/   -> 127.0.0.1:{8545,8546}"

# 📦 Install deps
sudo apt update
sudo apt install -y nginx certbot python3-certbot-nginx jq

# 📡 Firewall (public 80/443 + P2P 26656)
sudo ufw allow 'Nginx Full'
sudo ufw allow 26656/tcp

# 📁 Webroot
sudo mkdir -p "$WEBROOT"
sudo chown -R www-data:www-data "$WEBROOT"

# ⚙️ Temporary HTTP-only config for cert issuance
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

# 🔐 Certs for main + evm subdomain
sudo certbot certonly --webroot \
  -w "$WEBROOT" \
  -d "$DOMAIN" \
  -d "$EVM_SUBDOMAIN" \
  --non-interactive --agree-tos -m admin@$DOMAIN

# ✅ Final SSL config (single vhost for main domain with path prefixes)
sudo tee "$FINAL_CONFIG" > /dev/null <<EOF
# ---------------- Rate limiting ----------------
limit_req_zone \$binary_remote_addr zone=req_limit_per_ip:10m rate=30r/s;
limit_req_status 429;

# ---------------- EVM upstreams ----------------
upstream evm_http_backend { server 127.0.0.1:8545; }
upstream evm_ws_backend   { server 127.0.0.1:8546; }

# ---------------- Upgrade helper ---------------
map \$http_upgrade \$connection_upgrade {
    default upgrade;
    ''      close;
}

# ============== MAIN DOMAIN ====================
# Redirect HTTP -> HTTPS
server {
    listen 80;
    server_name $DOMAIN;
    return 301 https://\$host\$request_uri;
}

server {
    listen 443 ssl http2;
    server_name $DOMAIN;

    ssl_certificate     /etc/letsencrypt/live/$DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$DOMAIN/privkey.pem;

    # ---- REST (LCD) under /rest/ (strip prefix) ----
    # Example: /rest/cosmos/base/tendermint/v1beta1/node_info
    location ^~ /rest/ {
        # CORS for browser clients
        add_header Access-Control-Allow-Origin "*" always;
        add_header Access-Control-Allow-Methods "GET, POST, OPTIONS" always;
        add_header Access-Control-Allow-Headers "Content-Type, Authorization" always;

        if (\$request_method = OPTIONS) { return 204; }

        limit_req zone=req_limit_per_ip burst=30 nodelay;

        # Strip /rest/ prefix when proxying to LCD
        proxy_pass http://127.0.0.1:1317/;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
    }

    # ---- gRPC on /grpc (native HTTP/2) ----
    # Use grpc:// if backend is plaintext; use grpcs:// if your node enables TLS on 9090
    location = /grpc {
        limit_req zone=req_limit_per_ip burst=30 nodelay;
        grpc_read_timeout 86400s;
        grpc_send_timeout 86400s;
        grpc_set_header Host \$host;
        grpc_set_header X-Real-IP \$remote_addr;
        grpc_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        grpc_pass grpc://127.0.0.1:9090;
    }

    # ---- gRPC-Web under /grpc-web/ (optional) ----
    location ^~ /grpc-web/ {
        # CORS for browsers
        add_header Access-Control-Allow-Origin "*" always;
        add_header Access-Control-Allow-Methods "GET, POST, OPTIONS" always;
        add_header Access-Control-Allow-Headers "Content-Type, X-Grpc-Web, Authorization" always;
        if (\$request_method = OPTIONS) { return 204; }

        limit_req zone=req_limit_per_ip burst=30 nodelay;

        # If you run a grpc-web proxy on 9091:
        proxy_pass http://127.0.0.1:9091/;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
    }

    # ---- Cosmos RPC (root / ) ----
    location / {
        limit_req zone=req_limit_per_ip burst=30 nodelay;
        proxy_pass http://127.0.0.1:26657;
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

# ============== EVM SUBDOMAIN ==================
server {
    listen 80;
    server_name $EVM_SUBDOMAIN;
    return 301 https://\$host\$request_uri;
}

server {
    listen 443 ssl http2;
    server_name $EVM_SUBDOMAIN;

    ssl_certificate     /etc/letsencrypt/live/$DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$DOMAIN/privkey.pem;

    location / {
        limit_req zone=req_limit_per_ip burst=30 nodelay;

        # Switch to WS backend on Upgrade
        set \$backend http://evm_http_backend;
        if (\$http_upgrade = "websocket") { set \$backend http://evm_ws_backend; }

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

# 🧪 Quick checks
echo "🔎 Health checks:"
# Cosmos RPC
curl -s https://$DOMAIN/status | jq '.result.sync_info.catching_up' || echo "⚠️ Cosmos RPC check failed"

# REST
REST_OK=$(curl -s https://$DOMAIN/rest/cosmos/base/tendermint/v1beta1/node_info | jq -r '.default_node_info.network' || true)
if [ -n "$REST_OK" ] && [ "$REST_OK" != "null" ]; then
  echo "✅ REST (LCD) is live: https://$DOMAIN/rest/..."
else
  echo "⚠️ REST (LCD) not responding"
fi

# EVM
EVM_RPC=$(curl -s -X POST https://$EVM_SUBDOMAIN -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}')
if echo "$EVM_RPC" | jq -e '.result' &>/dev/null; then
  echo "✅ EVM RPC (HTTPS) is live: https://$EVM_SUBDOMAIN"
else
  echo "⚠️ EVM RPC (HTTPS) not responding"
fi

echo
echo "🚀 Setup complete!"
echo "🔗 Cosmos RPC:  https://$DOMAIN/"
echo "🔗 REST (LCD):  https://$DOMAIN/rest/..."
echo "🔗 gRPC:        https://$DOMAIN/grpc"
echo "🔗 gRPC-Web:    https://$DOMAIN/grpc-web/  (if enabled)"
echo "🔗 EVM RPC:     https://$EVM_SUBDOMAIN/"
