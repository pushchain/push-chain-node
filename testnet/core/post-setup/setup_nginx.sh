#!/bin/bash

# ---------------------------------------
# Push Chain NGINX + SSL Setup Script
# Main domain: Cosmos RPC (/) + REST (/rest)
# Subdomains:
#   - EVM RPC:  evm.$DOMAIN  -> 127.0.0.1:{8545,8546}
#   - gRPC:     grpc.$DOMAIN -> 127.0.0.1:9090 (HTTP/2, native gRPC)
# ---------------------------------------

if [ -z "$1" ]; then
  echo "❌ Usage: ./setup_nginx.sh yourdomain.com"
  exit 1
fi

DOMAIN=$1
EVM_SUBDOMAIN="evm.$DOMAIN"
GRPC_SUBDOMAIN="grpc.$DOMAIN"

NGINX_CONFIG="/etc/nginx/sites-available/push-node"
TMP_CONFIG="/tmp/push-node-temp"
FINAL_CONFIG="/tmp/push-node-final"
WEBROOT="/var/www/certbot"

set -e

echo "🌐 NGINX will serve:"
echo "   - Cosmos RPC:  https://$DOMAIN/              -> 127.0.0.1:26657"
echo "   - REST (LCD):  https://$DOMAIN/rest/...      -> 127.0.0.1:1317"
echo "   - gRPC:        https://$GRPC_SUBDOMAIN       -> 127.0.0.1:9090 (HTTP/2)"
echo "   - EVM RPC:     https://$EVM_SUBDOMAIN        -> 127.0.0.1:{8545,8546}"

# 📦 Install deps
sudo apt update
sudo apt install -y nginx certbot python3-certbot-nginx jq

# 📡 Firewall (public 80/443 + P2P 26656)
sudo ufw allow 'Nginx Full'
sudo ufw allow 26656/tcp

# 📁 Webroot for ACME challenges
sudo mkdir -p "$WEBROOT"
sudo chown -R www-data:www-data "$WEBROOT"

# ⚙️ Temporary HTTP-only config for cert issuance
sudo tee "$TMP_CONFIG" > /dev/null <<EOF
server {
    listen 80;
    server_name $DOMAIN $EVM_SUBDOMAIN $GRPC_SUBDOMAIN;

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

# 🔐 Certs for main + evm + grpc subdomains
sudo certbot certonly --webroot \
  -w "$WEBROOT" \
  --cert-name "$DOMAIN" \
  --expand \
  -d "$DOMAIN" \
  -d "evm.$DOMAIN" \
  -d "grpc.$DOMAIN" \
  --non-interactive --agree-tos -m admin@$DOMAIN


# ✅ Final SSL config (main host + evm + grpc subdomain)
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

# ============== gRPC SUBDOMAIN =================
server {
    listen 80;
    server_name $GRPC_SUBDOMAIN;
    return 301 https://\$host\$request_uri;
}

server {
    listen 443 ssl http2;
    server_name $GRPC_SUBDOMAIN;

    ssl_certificate     /etc/letsencrypt/live/$DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$DOMAIN/privkey.pem;

    # Optional: reuse rate-limit zone
    limit_req zone=req_limit_per_ip burst=30 nodelay;

    # Native gRPC (plaintext upstream on 9090)
    location / {
        grpc_read_timeout 86400s;
        grpc_send_timeout 86400s;

        grpc_set_header Host \$host;
        grpc_set_header X-Real-IP \$remote_addr;
        grpc_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;

        grpc_pass grpc://127.0.0.1:9090;  # use grpcs://127.0.0.1:9090 if your node has TLS
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

# gRPC (optional): try if grpcurl is installed
if command -v grpcurl >/dev/null 2>&1; then
  echo "➡️  gRPC GetNodeInfo via https://$GRPC_SUBDOMAIN"
  grpcurl -insecure -d '{}' $GRPC_SUBDOMAIN:443 cosmos.base.tendermint.v1beta1.Service/GetNodeInfo \
    | jq '.default_node_info.network,.application_version.version' || echo "⚠️ gRPC test failed (grpcurl)"
else
  echo "ℹ️ Install grpcurl to test gRPC:"
  echo "   curl -sSL https://github.com/fullstorydev/grpcurl/releases/latest/download/grpcurl_linux_x86_64.tar.gz | sudo tar -xz -C /usr/local/bin grpcurl"
  echo "   grpcurl -insecure -d '{}' $GRPC_SUBDOMAIN:443 cosmos.base.tendermint.v1beta1.Service/GetNodeInfo | jq"
fi

echo
echo "🚀 Setup complete!"
echo "🔗 Cosmos RPC:  https://$DOMAIN/"
echo "🔗 REST (LCD):  https://$DOMAIN/rest/..."
echo "🔗 gRPC:        https://$GRPC_SUBDOMAIN"
echo "🔗 EVM RPC:     https://$EVM_SUBDOMAIN"
