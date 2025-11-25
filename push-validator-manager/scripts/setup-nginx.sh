#!/bin/bash

# ---------------------------------------
# Push Chain NGINX + SSL Setup Script
# Native Push Validator Manager Edition
# ---------------------------------------
# - Sets up NGINX to serve Cosmos and EVM RPCs
# - Bootstraps temporary HTTP config to fetch certs
# - Replaces config with SSL-enabled version
# - Adapted for native setup with ~/.pchain paths
# ---------------------------------------

set -euo pipefail

# Colors for output - Standardized palette
GREEN='\033[0;32m'      # Success messages
RED='\033[0;31m'        # Error messages  
YELLOW='\033[0;33m'     # Warning messages
CYAN='\033[0;36m'       # Status/info messages
BLUE='\033[1;94m'       # Headers/titles (bright blue)
MAGENTA='\033[0;35m'    # Accent/highlight data
NC='\033[0m'            # No color/reset
BOLD='\033[1m'          # Emphasis

# Print functions - Unified across all scripts
print_status() { echo -e "${CYAN}$1${NC}"; }
print_header() { echo -e "${BLUE}$1${NC}"; }
print_success() { echo -e "${GREEN}$1${NC}"; }
print_error() { echo -e "${RED}$1${NC}"; }
print_warning() { echo -e "${YELLOW}$1${NC}"; }

# Validate input
if [ -z "${1:-}" ]; then
    print_error "❌ Usage: ./push-validator setup-nginx yourdomain.com"
    echo
    print_status "This sets up public HTTPS endpoints:"
    print_status "  • https://yourdomain.com - Cosmos RPC"
    print_status "  • https://evm.yourdomain.com - EVM RPC"
    echo
    print_warning "⚠️  Requirements:"
    print_status "  • Domain must point to this server's IP"
    print_status "  • Ports 80 and 443 must be open"
    print_status "  • Node must be running"
    exit 1
fi

DOMAIN=$1
EVM_SUBDOMAIN="evm.$DOMAIN"
NGINX_CONFIG="/etc/nginx/sites-available/push-node"
TMP_CONFIG="/tmp/push-node-temp"
FINAL_CONFIG="/tmp/push-node-final"
WEBROOT="/var/www/certbot"

print_header "🌐 Setting up NGINX for $DOMAIN and $EVM_SUBDOMAIN..."
echo

# Check if we're running as root or have sudo
if [ "$EUID" -eq 0 ]; then
    SUDO=""
else
    if ! command -v sudo >/dev/null 2>&1; then
        print_error "❌ This script requires sudo privileges"
        exit 1
    fi
    SUDO="sudo"
fi

# Prerequisites check
print_status "🔍 Checking prerequisites..."

# Check if node is running
if ! pgrep -f "pchaind start" >/dev/null; then
    print_error "❌ Push node is not running"
    print_status "Start your node first: ./push-validator start"
    exit 1
fi

# Check if required ports are accessible
for port in 26657 8545 8546; do
    if ! nc -z localhost "$port" 2>/dev/null; then
        print_warning "⚠️  Port $port not accessible locally"
        print_status "Make sure your node is fully started and synced"
    fi
done

print_success "✅ Prerequisites check passed"
echo

# Install dependencies
print_status "📦 Installing dependencies..."
$SUDO apt update
$SUDO apt install -y nginx certbot python3-certbot-nginx jq

# Configure firewall
print_status "📡 Configuring firewall..."
$SUDO ufw allow 'Nginx Full' || print_warning "UFW not configured (this is okay)"
$SUDO ufw allow 26656/tcp || print_warning "UFW not configured (this is okay)"

# Ensure webroot exists
print_status "📁 Setting up webroot..."
$SUDO mkdir -p "$WEBROOT"
$SUDO chown -R www-data:www-data "$WEBROOT"

print_success "✅ Dependencies installed"
echo

# Write temporary HTTP-only config to serve .well-known
print_status "⚙️  Creating temporary NGINX configuration..."
$SUDO tee "$TMP_CONFIG" > /dev/null <<EOF
server {
    listen 80;
    server_name $DOMAIN $EVM_SUBDOMAIN;

    location /.well-known/acme-challenge/ {
        root $WEBROOT;
    }

    location / {
        return 200 'Push Chain NGINX setup in progress...';
        add_header Content-Type text/plain;
    }
}
EOF

$SUDO cp "$TMP_CONFIG" "$NGINX_CONFIG"
$SUDO ln -sf "$NGINX_CONFIG" /etc/nginx/sites-enabled/push-node

# Remove default nginx site if it exists
$SUDO rm -f /etc/nginx/sites-enabled/default

# Test and reload nginx
if ! $SUDO nginx -t; then
    print_error "❌ NGINX configuration test failed"
    exit 1
fi

$SUDO systemctl reload nginx
print_success "✅ Temporary NGINX configuration active"
echo

# Request SSL certificates
print_status "🔐 Requesting SSL certificates..."
print_warning "⚠️  This will use Let's Encrypt and requires your domain to resolve to this server"

if ! $SUDO certbot certonly --webroot \
    -w "$WEBROOT" \
    -d "$DOMAIN" \
    -d "$EVM_SUBDOMAIN" \
    --non-interactive --agree-tos \
    -m "admin@$DOMAIN"; then
    print_error "❌ SSL certificate generation failed"
    print_status "Common issues:"
    print_status "  • Domain doesn't point to this server"
    print_status "  • Firewall blocking port 80"
    print_status "  • Another service using port 80"
    exit 1
fi

print_success "✅ SSL certificates obtained"
echo

# Write final SSL-enabled configuration
print_status "✅ Creating production NGINX configuration..."
$SUDO tee "$FINAL_CONFIG" > /dev/null <<EOF
# Rate limiting
limit_req_zone \$binary_remote_addr zone=req_limit_per_ip:10m rate=10r/s;
limit_req_status 429;

# Cosmos RPC - HTTP to HTTPS redirect
server {
    listen 80;
    server_name $DOMAIN;
    return 301 https://\$host\$request_uri;
}

# Cosmos RPC - HTTPS
server {
    listen 443 ssl http2;
    server_name $DOMAIN;

    ssl_certificate /etc/letsencrypt/live/$DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$DOMAIN/privkey.pem;

    # Security headers
    add_header X-Frame-Options DENY;
    add_header X-Content-Type-Options nosniff;
    add_header X-XSS-Protection "1; mode=block";

    location / {
        limit_req zone=req_limit_per_ip burst=20 nodelay;
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

# EVM RPC backends
upstream http_backend {
    server 127.0.0.1:8545;
}

upstream ws_backend {
    server 127.0.0.1:8546;
}

# WebSocket connection upgrade mapping
map \$http_upgrade \$connection_upgrade {
    default upgrade;
    '' close;
}

# EVM RPC - HTTP to HTTPS redirect
server {
    listen 80;
    server_name $EVM_SUBDOMAIN;
    return 301 https://\$host\$request_uri;
}

# EVM RPC - HTTPS
server {
    listen 443 ssl http2;
    server_name $EVM_SUBDOMAIN;

    ssl_certificate /etc/letsencrypt/live/$DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$DOMAIN/privkey.pem;

    # Security headers
    add_header X-Frame-Options DENY;
    add_header X-Content-Type-Options nosniff;
    add_header X-XSS-Protection "1; mode=block";

    location / {
        limit_req zone=req_limit_per_ip burst=20 nodelay;
        
        # Route based on upgrade header for WebSocket support
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

$SUDO cp "$FINAL_CONFIG" "$NGINX_CONFIG"

# Test and reload final configuration
if ! $SUDO nginx -t; then
    print_error "❌ Final NGINX configuration test failed"
    exit 1
fi

$SUDO systemctl reload nginx
print_success "✅ Production NGINX configuration active"
echo

# Verify setup
print_status "🧪 Verifying setup..."
sleep 2

# Test Cosmos RPC
if curl -s "https://$DOMAIN/status" | jq -e '.result.sync_info.catching_up' >/dev/null 2>&1; then
    print_success "✅ Cosmos RPC (HTTPS) is working: https://$DOMAIN"
else
    print_warning "⚠️  Cosmos RPC check failed - may need a moment to start"
fi

# Test EVM RPC
EVM_TEST=$(curl -s -X POST "https://$EVM_SUBDOMAIN" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' 2>/dev/null || echo "")

if echo "$EVM_TEST" | jq -e '.result' >/dev/null 2>&1; then
    print_success "✅ EVM RPC (HTTPS) is working: https://$EVM_SUBDOMAIN"
else
    print_warning "⚠️  EVM RPC check failed - may need a moment to start"
fi

echo
print_header "🚀 Setup complete!"
echo
print_success "🔗 Cosmos RPC: https://$DOMAIN"
print_success "🔗 EVM RPC:    https://$EVM_SUBDOMAIN"
echo
print_status "📝 Next steps:"
print_status "  • Test endpoints with your applications"
print_status "  • Monitor logs: sudo journalctl -u nginx -f"
print_status "  • Set up log rotation: ./push-validator setup-logs"
print_status "  • Create backups: ./push-validator backup"
echo
print_warning "🔒 Security notes:"
print_status "  • Rate limiting is set to 10 requests/second per IP"
print_status "  • SSL certificates auto-renew via cron"
print_status "  • Monitor your server for unusual activity"

# Clean up temporary files
rm -f "$TMP_CONFIG" "$FINAL_CONFIG"