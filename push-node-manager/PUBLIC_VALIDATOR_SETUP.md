# Public Validator Setup Guide (Optional)

This guide helps you make your validator publicly accessible with HTTPS endpoints. This is **completely optional** - your validator works perfectly fine on localhost.

## Prerequisites

- A domain name pointing to your server
- Ubuntu/Debian server with public IP
- Ports 80 and 443 open in firewall
- Your validator already running locally

## Quick Setup

### 1. Install Nginx and Certbot

```bash
sudo apt update
sudo apt install -y nginx certbot python3-certbot-nginx
```

### 2. Configure Firewall

```bash
# Allow Nginx
sudo ufw allow 'Nginx Full'

# Allow validator P2P port
sudo ufw allow 26656/tcp

# Check status
sudo ufw status
```

## Nginx Configuration

Create `/etc/nginx/sites-available/push-node-manager` with all services:

```nginx
# Cosmos RPC
server {
    listen 80;
    server_name rpc.your-domain.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name rpc.your-domain.com;
    
    location / {
        proxy_pass http://localhost:26657;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
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

map $http_upgrade $connection_upgrade {
    default upgrade;
    '' close;
}

server {
    listen 80;
    server_name evm.your-domain.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name evm.your-domain.com;
    
    location / {
        set $backend http://http_backend;
        if ($http_upgrade = "websocket") {
            set $backend http://ws_backend;
        }
        
        proxy_pass $backend;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_set_header Host localhost;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
    }
}

# REST API
server {
    listen 80;
    server_name api.your-domain.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name api.your-domain.com;
    
    location / {
        proxy_pass http://localhost:1317;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}

# gRPC Web
server {
    listen 80;
    server_name grpc.your-domain.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name grpc.your-domain.com;
    
    location / {
        grpc_pass grpc://localhost:9090;
        grpc_set_header Host $host;
        grpc_set_header X-Real-IP $remote_addr;
    }
}
```

## Enable Configuration

```bash
# Enable the configuration
sudo ln -s /etc/nginx/sites-available/push-node-manager /etc/nginx/sites-enabled/

# Test configuration
sudo nginx -t

# Reload nginx
sudo systemctl reload nginx
```

## Setup SSL with Let's Encrypt

```bash
# Setup SSL for all domains
sudo certbot --nginx -d rpc.your-domain.com -d evm.your-domain.com -d api.your-domain.com -d grpc.your-domain.com

# Follow the prompts and certbot will automatically configure SSL
```

## Verify Setup

Test your endpoints:

```bash
# Cosmos RPC
curl https://rpc.your-domain.com/status

# EVM RPC
curl -X POST https://evm.your-domain.com \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'

# WebSocket
wscat -c wss://evm.your-domain.com
```

## Security Hardening (Recommended)

### 1. Add rate limiting to prevent abuse:

```nginx
# Add to http block in /etc/nginx/nginx.conf
limit_req_zone $binary_remote_addr zone=api_limit:10m rate=10r/s;
limit_req_zone $binary_remote_addr zone=rpc_limit:10m rate=30r/s;

# Add to location blocks
limit_req zone=api_limit burst=20 nodelay;
```

### 2. Add IP whitelisting (if needed):

```nginx
# In location block
allow 1.2.3.4;  # Your IP
allow 5.6.7.0/24;  # Your subnet
deny all;
```

### 3. Enhanced SSL configuration:

```nginx
# Add to server blocks
ssl_protocols TLSv1.2 TLSv1.3;
ssl_prefer_server_ciphers on;
ssl_ciphers EECDH+AESGCM:EDH+AESGCM;
ssl_ecdh_curve secp384r1;
ssl_session_cache shared:SSL:10m;
ssl_session_tickets off;
ssl_stapling on;
ssl_stapling_verify on;

# Security headers
add_header Strict-Transport-Security "max-age=63072000; includeSubDomains; preload";
add_header X-Frame-Options DENY;
add_header X-Content-Type-Options nosniff;
```

## Alternative: Using Scripts

For quick setup, save this as `setup-public-validator.sh`:

```bash
#!/bin/bash

# Check if domain is provided
if [ -z "$1" ]; then
    echo "Usage: ./setup-public-validator.sh your-domain.com"
    exit 1
fi

DOMAIN=$1

# Install dependencies
sudo apt update
sudo apt install -y nginx certbot python3-certbot-nginx

# Create nginx config
cat > /tmp/push-node-manager-nginx << EOF
server {
    listen 80;
    server_name rpc.$DOMAIN;
    return 301 https://\$host\$request_uri;
}

server {
    listen 443 ssl http2;
    server_name rpc.$DOMAIN;
    
    location / {
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
    server_name evm.$DOMAIN;
    return 301 https://\$host\$request_uri;
}

server {
    listen 443 ssl http2;
    server_name evm.$DOMAIN;
    
    location / {
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

# Copy config
sudo cp /tmp/push-node-manager-nginx /etc/nginx/sites-available/push-node-manager
sudo ln -s /etc/nginx/sites-available/push-node-manager /etc/nginx/sites-enabled/

# Test and reload
sudo nginx -t && sudo systemctl reload nginx

# Setup SSL
sudo certbot --nginx -d rpc.$DOMAIN -d evm.$DOMAIN

echo "Setup complete! Your validator is now accessible at:"
echo "  Cosmos RPC: https://rpc.$DOMAIN"
echo "  EVM RPC: https://evm.$DOMAIN"
```

## Troubleshooting

1. **502 Bad Gateway**: Validator not running or wrong port
   ```bash
   ./push-node-manager status
   docker ps
   ```

2. **SSL Certificate Issues**: Re-run certbot
   ```bash
   sudo certbot renew --dry-run
   ```

3. **WebSocket Connection Failed**: Check nginx error logs
   ```bash
   sudo tail -f /var/log/nginx/error.log
   ```

4. **Rate Limiting**: Adjust limits in nginx config

## Monitoring

Add monitoring to `/etc/nginx/sites-available/monitoring`:

```nginx
server {
    listen 127.0.0.1:8080;
    server_name localhost;
    
    location /nginx_status {
        stub_status on;
        access_log off;
    }
}
```

## Notes

- This setup is optional - validators work fine on localhost
- Always backup your validator keys before making changes
- Consider using a CDN like Cloudflare for additional protection
- Monitor your logs for suspicious activity
- Set up fail2ban for additional security

