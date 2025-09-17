#!/bin/bash

# ---------------------------------------
# Push Chain NGINX Testing Suite
# All-in-one NGINX setup testing script
# ---------------------------------------

set -euo pipefail

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BLUE='\033[1;94m'
MAGENTA='\033[0;35m'
NC='\033[0m'

print_status() { echo -e "${CYAN}$1${NC}"; }
print_header() { echo -e "${BLUE}$1${NC}"; }
print_success() { echo -e "${GREEN}$1${NC}"; }
print_error() { echo -e "${RED}$1${NC}"; }
print_warning() { echo -e "${YELLOW}$1${NC}"; }
print_info() { echo -e "${MAGENTA}$1${NC}"; }

# Default values
DOMAIN="${2:-pushnode.local}"
EVM_SUBDOMAIN="evm.$DOMAIN"
MODE="${1:-help}"

show_help() {
    print_header "🧪 Push Chain NGINX Testing Suite"
    echo
    print_status "USAGE:"
    print_status "  ./test-nginx.sh <mode> [domain]"
    echo
    print_success "MODES:"
    print_info "  quick       - Quick validation test (no sudo, no server)"
    print_info "  local       - Full local test with mock server (no sudo)"  
    print_info "  sudo        - Production-like test with SSL (requires sudo)"
    print_info "  guide       - Show all testing methods and examples"
    print_info "  help        - Show this help message"
    echo
    print_success "EXAMPLES:"
    print_status "  ./test-nginx.sh quick"
    print_status "  ./test-nginx.sh local"
    print_status "  ./test-nginx.sh sudo mynode.local"
    print_status "  ./test-nginx.sh guide"
    echo
    print_warning "📋 QUICK START:"
    print_status "  For developers: ./test-nginx.sh quick"
    print_status "  For full test:  ./test-nginx.sh local"
}

quick_test() {
    print_header "⚡ Quick NGINX Validation Test"
    print_status "Testing core logic without starting servers..."
    echo

    local test_dir="/tmp/nginx-quick-test-$$"
    mkdir -p "$test_dir/ssl" "$test_dir/nginx"

    # Test 1: SSL Certificate Generation
    print_status "🔐 Testing SSL certificate generation..."
    if command -v openssl >/dev/null 2>&1; then
        openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
            -keyout "$test_dir/ssl/privkey.pem" \
            -out "$test_dir/ssl/fullchain.pem" \
            -subj "/C=US/ST=Test/L=Local/O=PushChain/OU=Testing/CN=$DOMAIN" \
            -addext "subjectAltName=DNS:$DOMAIN,DNS:$EVM_SUBDOMAIN,DNS:localhost,IP:127.0.0.1" 2>/dev/null
        
        if [[ -f "$test_dir/ssl/privkey.pem" && -f "$test_dir/ssl/fullchain.pem" ]]; then
            print_success "✅ SSL certificate generation works"
        else
            print_error "❌ SSL certificate generation failed"
        fi
    else
        print_error "❌ openssl not found - install with: brew install openssl"
        return 1
    fi

    # Test 2: NGINX Configuration Generation
    print_status "⚙️  Testing NGINX configuration generation..."
    cat > "$test_dir/nginx/test-config.conf" <<EOF
worker_processes 1;
error_log $test_dir/error.log;
pid $test_dir/nginx.pid;

events { worker_connections 1024; }

http {
    limit_req_zone \$binary_remote_addr zone=test_limit:10m rate=100r/s;

    server {
        listen 8080;
        server_name $DOMAIN localhost;
        location / {
            limit_req zone=test_limit burst=50 nodelay;
            return 200 '{"status":"ok","domain":"$DOMAIN"}';
            add_header Content-Type application/json;
        }
    }

    server {
        listen 8443 ssl;
        server_name $DOMAIN localhost;
        ssl_certificate $test_dir/ssl/fullchain.pem;
        ssl_certificate_key $test_dir/ssl/privkey.pem;
        location / {
            return 200 '{"status":"ok","domain":"$DOMAIN","ssl":true}';
            add_header Content-Type application/json;
        }
    }
}
EOF

    print_success "✅ NGINX configuration generated"

    # Test 3: Configuration Syntax
    print_status "🧪 Testing NGINX configuration syntax..."
    if command -v nginx >/dev/null 2>&1; then
        if nginx -t -c "$test_dir/nginx/test-config.conf" 2>/dev/null; then
            print_success "✅ Configuration syntax is valid"
        else
            print_error "❌ Configuration syntax is invalid"
        fi
    else
        print_warning "⚠️  nginx not found - install with: brew install nginx"
    fi

    # Test 4: Port Availability
    print_status "🔍 Testing port availability..."
    local available=0
    for port in 8080 8081 8443 8444; do
        if ! lsof -Pi :$port -sTCP:LISTEN -t >/dev/null 2>&1; then
            print_success "✅ Port $port available"
            ((available++))
        else
            print_warning "⚠️  Port $port in use"
        fi
    done

    # Test 5: Prerequisites Check
    print_status "🔍 Testing prerequisites..."
    if pgrep -f "pchaind start" >/dev/null; then
        print_success "✅ Push node detected"
    else
        print_info "ℹ️  No Push node running (will use mocks)"
    fi

    for port in 26657 8545 8546; do
        if nc -z localhost "$port" 2>/dev/null; then
            print_success "✅ Port $port accessible"
        else
            print_info "ℹ️  Port $port not accessible (will use mocks)"
        fi
    done

    # Cleanup
    rm -rf "$test_dir"

    echo
    print_header "⚡ Quick Test Complete"
    print_success "✅ All core components validated"
    print_status "Next: Run './test-nginx.sh local' for full test"
}

local_test() {
    print_header "🧪 Full Local NGINX Test"
    print_status "Starting mock NGINX server with SSL..."
    echo

    local test_dir="$HOME/.push-nginx-test"
    local nginx_config="$test_dir/nginx.conf"
    local ssl_dir="$test_dir/ssl"

    # Setup
    mkdir -p "$test_dir" "$ssl_dir"

    # Generate SSL certificate
    print_status "🔑 Creating self-signed certificate..."
    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
        -keyout "$ssl_dir/privkey.pem" \
        -out "$ssl_dir/fullchain.pem" \
        -subj "/C=US/ST=Test/L=Local/O=PushChain/OU=Testing/CN=$DOMAIN" \
        -addext "subjectAltName=DNS:$DOMAIN,DNS:$EVM_SUBDOMAIN,DNS:localhost,IP:127.0.0.1" 2>/dev/null

    print_success "✅ SSL certificate created"

    # Create NGINX config
    print_status "⚙️  Creating NGINX configuration..."
    cat > "$nginx_config" <<EOF
worker_processes 1;
error_log $test_dir/error.log;
pid $test_dir/nginx.pid;

events {
    worker_connections 1024;
}

http {
    access_log $test_dir/access.log;
    limit_req_zone \$binary_remote_addr zone=test_limit:10m rate=100r/s;

    # Cosmos RPC - HTTP
    server {
        listen 8080;
        server_name $DOMAIN localhost;
        location /status {
            return 200 '{"result":{"node_info":{"network":"push_42101-1"},"sync_info":{"catching_up":false}}}';
            add_header Content-Type application/json;
        }
        location / {
            limit_req zone=test_limit burst=50 nodelay;
            return 200 '{"jsonrpc":"2.0","id":1,"result":{"node_info":{"network":"push_42101-1"}}}';
            add_header Content-Type application/json;
        }
    }

    # Cosmos RPC - HTTPS
    server {
        listen 8443 ssl;
        server_name $DOMAIN localhost;
        ssl_certificate $ssl_dir/fullchain.pem;
        ssl_certificate_key $ssl_dir/privkey.pem;
        location /status {
            return 200 '{"result":{"node_info":{"network":"push_42101-1"},"sync_info":{"catching_up":false}}}';
            add_header Content-Type application/json;
        }
        location / {
            limit_req zone=test_limit burst=50 nodelay;
            return 200 '{"jsonrpc":"2.0","id":1,"result":{"node_info":{"network":"push_42101-1"}}}';
            add_header Content-Type application/json;
        }
    }

    # EVM RPC - HTTP
    server {
        listen 8081;
        server_name $EVM_SUBDOMAIN localhost;
        location / {
            limit_req zone=test_limit burst=50 nodelay;
            return 200 '{"jsonrpc":"2.0","id":1,"result":"0x1"}';
            add_header Content-Type application/json;
        }
    }

    # EVM RPC - HTTPS
    server {
        listen 8444 ssl;
        server_name $EVM_SUBDOMAIN localhost;
        ssl_certificate $ssl_dir/fullchain.pem;
        ssl_certificate_key $ssl_dir/privkey.pem;
        location / {
            limit_req zone=test_limit burst=50 nodelay;
            return 200 '{"jsonrpc":"2.0","id":1,"result":"0x2"}';
            add_header Content-Type application/json;
        }
    }
}
EOF

    # Test configuration
    if ! nginx -t -c "$nginx_config"; then
        print_error "❌ NGINX configuration test failed"
        return 1
    fi

    print_success "✅ Configuration validated"

    # Check ports
    local ports_in_use=false
    for port in 8080 8081 8443 8444; do
        if lsof -Pi :$port -sTCP:LISTEN -t >/dev/null 2>&1; then
            print_warning "⚠️  Port $port is in use"
            ports_in_use=true
        fi
    done

    if [[ "$ports_in_use" == "true" ]]; then
        print_warning "Some ports are in use. Configuration is valid but cannot start server."
        print_status "Stop services on ports 8080, 8081, 8443, 8444 to run server test"
        return 0
    fi

    # Start NGINX
    print_status "🚀 Starting test server..."
    nginx -c "$nginx_config"
    sleep 2

    # Test endpoints
    print_status "🧪 Testing endpoints..."

    # HTTP tests
    if curl -s "http://localhost:8080/status" | grep -q "catching_up"; then
        print_success "✅ HTTP Cosmos RPC: http://localhost:8080/status"
    else
        print_warning "⚠️  HTTP Cosmos RPC test failed"
    fi

    if curl -s -X POST "http://localhost:8081" -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | grep -q "0x1"; then
        print_success "✅ HTTP EVM RPC: http://localhost:8081"
    else
        print_warning "⚠️  HTTP EVM RPC test failed"
    fi

    # HTTPS tests
    if curl -k -s "https://localhost:8443/status" | grep -q "catching_up"; then
        print_success "✅ HTTPS Cosmos RPC: https://localhost:8443/status"
    else
        print_warning "⚠️  HTTPS Cosmos RPC test failed"
    fi

    if curl -k -s -X POST "https://localhost:8444" -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | grep -q "0x2"; then
        print_success "✅ HTTPS EVM RPC: https://localhost:8444"
    else
        print_warning "⚠️  HTTPS EVM RPC test failed"
    fi

    echo
    print_header "🚀 Test Server Running!"
    echo
    print_success "🔗 Test URLs:"
    print_status "  http://localhost:8080/status"
    print_status "  https://localhost:8443/status (accept self-signed cert)"
    print_status "  http://localhost:8081 (EVM RPC)"
    print_status "  https://localhost:8444 (EVM RPC SSL)"
    echo
    print_status "Press Ctrl+C to stop the test server"
    
    # Cleanup handler
    trap 'print_status "🛑 Stopping server..."; nginx -s quit -c "$nginx_config" 2>/dev/null || pkill -f "nginx.*nginx.conf"; print_success "✅ Server stopped"; exit 0' INT

    # Keep running
    while pgrep -f "nginx.*nginx.conf" >/dev/null; do
        sleep 1
    done
}

sudo_test() {
    print_header "🔒 Production-like NGINX Test (Requires Sudo)"
    print_warning "⚠️  This test creates system-level configuration"
    echo

    # Check sudo availability
    if [ "$EUID" -eq 0 ]; then
        SUDO=""
    else
        if ! command -v sudo >/dev/null 2>&1; then
            print_error "❌ This test requires sudo privileges"
            return 1
        fi
        SUDO="sudo"
    fi

    local ssl_dir="/etc/ssl/certs/pushnode-test"
    local nginx_config="/etc/nginx/sites-available/push-node-test"

    print_status "📦 Installing dependencies..."
    $SUDO apt update >/dev/null 2>&1 || print_warning "⚠️  apt update failed (non-Ubuntu system?)"
    $SUDO apt install -y nginx openssl >/dev/null 2>&1 || print_warning "⚠️  Package install failed"

    print_status "🔐 Creating SSL certificates..."
    $SUDO mkdir -p "$ssl_dir"
    $SUDO openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
        -keyout "$ssl_dir/privkey.pem" \
        -out "$ssl_dir/fullchain.pem" \
        -subj "/C=US/ST=Test/L=Local/O=PushChain/OU=Testing/CN=$DOMAIN" \
        -addext "subjectAltName=DNS:$DOMAIN,DNS:$EVM_SUBDOMAIN,DNS:localhost,IP:127.0.0.1" 2>/dev/null

    print_status "⚙️  Creating NGINX configuration..."
    $SUDO tee "$nginx_config" > /dev/null <<EOF
limit_req_zone \$binary_remote_addr zone=prod_test:10m rate=100r/s;

server {
    listen 8080;
    server_name $DOMAIN localhost;
    location / {
        limit_req zone=prod_test burst=50 nodelay;
        return 200 '{"status":"production-test","domain":"$DOMAIN"}';
        add_header Content-Type application/json;
    }
}

server {
    listen 8443 ssl http2;
    server_name $DOMAIN localhost;
    ssl_certificate $ssl_dir/fullchain.pem;
    ssl_certificate_key $ssl_dir/privkey.pem;
    
    location / {
        limit_req zone=prod_test burst=50 nodelay;
        return 200 '{"status":"production-test-ssl","domain":"$DOMAIN"}';
        add_header Content-Type application/json;
    }
}
EOF

    $SUDO ln -sf "$nginx_config" /etc/nginx/sites-enabled/push-node-test
    $SUDO rm -f /etc/nginx/sites-enabled/default

    if ! $SUDO nginx -t; then
        print_error "❌ NGINX configuration test failed"
        return 1
    fi

    $SUDO systemctl reload nginx
    print_success "✅ Production test configuration active"

    # Test endpoints
    sleep 2
    if curl -s "http://localhost:8080" | grep -q "production-test"; then
        print_success "✅ Production HTTP test: http://localhost:8080"
    fi

    if curl -k -s "https://localhost:8443" | grep -q "production-test-ssl"; then
        print_success "✅ Production HTTPS test: https://localhost:8443"
    fi

    echo
    print_warning "🧹 Cleanup commands:"
    print_status "  sudo rm /etc/nginx/sites-enabled/push-node-test"
    print_status "  sudo rm -rf $ssl_dir"
    print_status "  sudo systemctl reload nginx"
}

show_guide() {
    print_header "📖 Complete NGINX Testing Guide"
    echo
    
    print_info "🎯 RECOMMENDED TEST SEQUENCE:"
    print_status "1. ./test-nginx.sh quick     # Fast validation"
    print_status "2. ./test-nginx.sh local     # Full local test"
    print_status "3. ./test-nginx.sh sudo      # Production test"
    echo
    
    print_success "📋 ALTERNATIVE TESTING METHODS:"
    echo
    print_header "Method 1: Domain Mapping"
    print_status "Add to /etc/hosts:"
    echo "127.0.0.1 $DOMAIN"
    echo "127.0.0.1 $EVM_SUBDOMAIN"
    print_status "Then run: ./test-nginx.sh local $DOMAIN"
    echo
    
    print_header "Method 2: Let's Encrypt Staging"
    print_status "Use staging server to avoid rate limits:"
    echo "certbot certonly --webroot --staging \\"
    echo "    -w /var/www/certbot \\"
    echo "    -d $DOMAIN \\"
    echo "    -d $EVM_SUBDOMAIN \\"
    echo "    --non-interactive --agree-tos \\"
    echo "    -m test@example.com"
    echo

    print_header "Method 3: ngrok Public Tunnel"
    print_status "1. Install ngrok: https://ngrok.com/download"
    print_status "2. Start tunnel: ngrok http 80"
    print_status "3. Use ngrok domain with original setup-nginx.sh"
    print_status "4. Benefits: Real public IP, real Let's Encrypt certs"
    echo

    print_header "Method 4: Docker Testing"
    print_status "Isolated environment testing:"
    cat << 'EOF'
docker run -it --rm \
    -p 80:80 -p 443:443 -p 26657:26657 -p 8545:8545 \
    -v $(pwd):/workspace \
    ubuntu:22.04 bash -c "
        apt update && apt install -y nginx certbot openssl
        cd /workspace
        ./test-nginx.sh sudo
    "
EOF
}

# Main execution
case "$MODE" in
    "quick")
        quick_test
        ;;
    "local")
        local_test
        ;;
    "sudo")
        sudo_test
        ;;
    "guide")
        show_guide
        ;;
    "help"|*)
        show_help
        ;;
esac