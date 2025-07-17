#!/bin/bash
# Push Chain Public Validator Setup Script
# Configures Nginx reverse proxy with SSL for public HTTPS access

set -e

# Source common functions if available
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PARENT_DIR="$(dirname "$SCRIPT_DIR")"

if [ -f "$SCRIPT_DIR/common.sh" ]; then
    source "$SCRIPT_DIR/common.sh"
else
    # Define colors locally if common.sh not available
    GREEN='\033[0;32m'
    BLUE='\033[0;34m'
    RED='\033[0;31m'
    YELLOW='\033[0;33m'
    CYAN='\033[0;36m'
    MAGENTA='\033[0;35m'
    WHITE='\033[1;37m'
    NC='\033[0m'
    BOLD='\033[1m'
    
    print_status() { echo -e "${BLUE}$1${NC}"; }
    print_success() { echo -e "${GREEN}$1${NC}"; }
    print_error() { echo -e "${RED}$1${NC}"; }
    print_warning() { echo -e "${YELLOW}$1${NC}"; }
fi

# Configuration
NGINX_SITES_AVAILABLE="/etc/nginx/sites-available"
NGINX_SITES_ENABLED="/etc/nginx/sites-enabled"
NGINX_CONFIG_NAME="push-node-manager"

# Banner
show_public_setup_banner() {
    echo -e "${BOLD}${CYAN}"
    echo "╔═══════════════════════════════════════════════════╗"
    echo "║      🌐 Push Chain Public Validator Setup 🌐      ║"
    echo "║                                                   ║"
    echo "║    Configure HTTPS endpoints for your validator   ║"
    echo "╚═══════════════════════════════════════════════════╝"
    echo -e "${NC}"
}

# Check prerequisites
check_prerequisites() {
    print_status "Checking prerequisites..."
    
    # Check OS
    if [[ "$OSTYPE" != "linux-gnu"* ]]; then
        print_error "This setup requires Ubuntu/Debian Linux"
        return 1
    fi
    
    # Check if running as root or with sudo
    if [ "$EUID" -ne 0 ] && ! sudo -n true 2>/dev/null; then 
        print_error "This script requires sudo privileges"
        echo "Please run with: sudo bash $0"
        return 1
    fi
    
    # Check if validator is running
    if ! docker ps | grep -q "push-node-manager"; then
        print_error "Validator container is not running!"
        echo "Please start your validator first: ./push-node-manager start"
        return 1
    fi
    
    # Check if ports are exposed
    local required_ports=("26657" "8545" "8546")
    for port in "${required_ports[@]}"; do
        if ! docker port $(docker ps -q -f name=push-node-manager) | grep -q "$port"; then
            print_warning "Port $port is not exposed by the container"
        fi
    done
    
    print_success "✓ Prerequisites check passed"
    return 0
}

# Install required packages
install_packages() {
    print_status "Installing required packages..."
    
    # Update package list
    sudo apt update
    
    # Install nginx if not present
    if ! command -v nginx &> /dev/null; then
        print_status "Installing nginx..."
        sudo apt install -y nginx
    else
        print_success "✓ Nginx already installed"
    fi
    
    # Install certbot if not present
    if ! command -v certbot &> /dev/null; then
        print_status "Installing certbot..."
        sudo apt install -y certbot python3-certbot-nginx
    else
        print_success "✓ Certbot already installed"
    fi
    
    # Install other utilities
    sudo apt install -y curl jq
    
    print_success "✓ All packages installed"
}

# Get domain configuration
get_domain_config() {
    echo
    print_status "Domain Configuration"
    echo "════════════════════════════════════════════════"
    echo
    
    # Get base domain
    read -p "Enter your base domain (e.g., example.com): " BASE_DOMAIN
    
    # Validate domain format
    if [[ ! "$BASE_DOMAIN" =~ ^[a-zA-Z0-9][a-zA-Z0-9-]{0,61}[a-zA-Z0-9]\.[a-zA-Z]{2,}$ ]]; then
        print_error "Invalid domain format"
        return 1
    fi
    
    # Show subdomain configuration
    echo
    print_status "The following subdomains will be configured:"
    echo "  • rpc.$BASE_DOMAIN  - Cosmos RPC (port 26657)"
    echo "  • evm.$BASE_DOMAIN  - EVM RPC (HTTP port 8545, WebSocket port 8546)"
    echo
    
    read -p "Continue with these subdomains? (yes/no): " confirm
    if [[ ! "$confirm" =~ ^[Yy][Ee][Ss]$ ]]; then
        return 1
    fi
    
    # Export for use in other functions
    export BASE_DOMAIN
    export RPC_DOMAIN="rpc.$BASE_DOMAIN"
    export EVM_DOMAIN="evm.$BASE_DOMAIN"
    
    return 0
}

# Check DNS configuration
check_dns() {
    print_status "Checking DNS configuration..."
    
    local server_ip=$(curl -s https://api.ipify.org 2>/dev/null || curl -s https://ifconfig.me 2>/dev/null)
    if [ -z "$server_ip" ]; then
        print_warning "Could not determine server IP address"
        read -p "Enter your server's public IP address: " server_ip
    fi
    
    echo "Server IP: $server_ip"
    echo
    
    local all_good=true
    for domain in "$RPC_DOMAIN" "$EVM_DOMAIN"; do
        print_status "Checking $domain..."
        
        local dns_ip=$(dig +short A "$domain" @8.8.8.8 2>/dev/null | tail -1)
        if [ -z "$dns_ip" ]; then
            print_error "✗ $domain - No DNS record found"
            all_good=false
        elif [ "$dns_ip" = "$server_ip" ]; then
            print_success "✓ $domain → $dns_ip"
        else
            print_error "✗ $domain → $dns_ip (expected: $server_ip)"
            all_good=false
        fi
    done
    
    echo
    if [ "$all_good" = false ]; then
        print_warning "DNS records are not properly configured"
        echo "Please add A records for all subdomains pointing to: $server_ip"
        echo
        read -p "Continue anyway? (yes/no): " continue_anyway
        if [[ ! "$continue_anyway" =~ ^[Yy][Ee][Ss]$ ]]; then
            return 1
        fi
    else
        print_success "✓ All DNS records properly configured"
    fi
    
    return 0
}

# Configure firewall
configure_firewall() {
    print_status "Configuring firewall..."
    
    # Check if ufw is installed
    if command -v ufw &> /dev/null; then
        # Allow SSH (important!)
        sudo ufw allow 22/tcp
        
        # Allow HTTP and HTTPS
        sudo ufw allow 80/tcp
        sudo ufw allow 443/tcp
        
        # Allow P2P port
        sudo ufw allow 26656/tcp
        
        # Enable firewall if not already enabled
        if ! sudo ufw status | grep -q "Status: active"; then
            print_warning "Enabling UFW firewall..."
            echo "y" | sudo ufw enable
        fi
        
        print_success "✓ Firewall configured"
    else
        print_warning "UFW not installed. Please configure your firewall manually."
    fi
}

# Create nginx configuration
create_nginx_config() {
    print_status "Creating nginx configuration..."
    
    # Backup existing config if present
    if [ -f "$NGINX_SITES_AVAILABLE/$NGINX_CONFIG_NAME" ]; then
        sudo cp "$NGINX_SITES_AVAILABLE/$NGINX_CONFIG_NAME" "$NGINX_SITES_AVAILABLE/$NGINX_CONFIG_NAME.backup.$(date +%Y%m%d_%H%M%S)"
        print_status "Backed up existing configuration"
    fi
    
    # Create the nginx configuration
    cat <<EOF | sudo tee "$NGINX_SITES_AVAILABLE/$NGINX_CONFIG_NAME" > /dev/null
# Push Node Manager Nginx Configuration
# Generated on $(date)

# Cosmos RPC
server {
    listen 80;
    server_name $RPC_DOMAIN;
    return 301 https://\$host\$request_uri;
}

server {
    listen 443 ssl http2;
    server_name $RPC_DOMAIN;
    
    # SSL configuration will be added by certbot
    
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
        
        # CORS headers
        add_header 'Access-Control-Allow-Origin' '*' always;
        add_header 'Access-Control-Allow-Methods' 'GET, POST, OPTIONS' always;
        add_header 'Access-Control-Allow-Headers' 'DNT,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range' always;
    }
}

# EVM RPC
upstream evm_http_backend {
    server 127.0.0.1:8545;
}

upstream evm_ws_backend {
    server 127.0.0.1:8546;
}

map \$http_upgrade \$connection_upgrade {
    default upgrade;
    '' close;
}

server {
    listen 80;
    server_name $EVM_DOMAIN;
    return 301 https://\$host\$request_uri;
}

server {
    listen 443 ssl http2;
    server_name $EVM_DOMAIN;
    
    # SSL configuration will be added by certbot
    
    location / {
        set \$backend http://evm_http_backend;
        
        # Route WebSocket connections to the WebSocket port
        if (\$http_upgrade = "websocket") {
            set \$backend http://evm_ws_backend;
        }
        
        proxy_pass \$backend;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection \$connection_upgrade;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
        
        # CORS headers
        add_header 'Access-Control-Allow-Origin' '*' always;
        add_header 'Access-Control-Allow-Methods' 'GET, POST, OPTIONS' always;
        add_header 'Access-Control-Allow-Headers' 'DNT,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Content-Type' always;
    }
}

EOF

    # Enable the configuration
    sudo ln -sf "$NGINX_SITES_AVAILABLE/$NGINX_CONFIG_NAME" "$NGINX_SITES_ENABLED/"
    
    # Test nginx configuration
    if sudo nginx -t; then
        print_success "✓ Nginx configuration created successfully"
        
        # Reload nginx
        sudo systemctl reload nginx
        print_success "✓ Nginx reloaded"
    else
        print_error "Nginx configuration test failed!"
        return 1
    fi
    
    return 0
}

# Setup SSL certificates
setup_ssl() {
    print_status "Setting up SSL certificates with Let's Encrypt..."
    
    # Get email for Let's Encrypt
    echo
    read -p "Enter email for Let's Encrypt notifications (or press Enter to skip): " LE_EMAIL
    
    local email_flag=""
    if [ -n "$LE_EMAIL" ]; then
        email_flag="--email $LE_EMAIL"
    else
        email_flag="--register-unsafely-without-email"
    fi
    
    # Run certbot for all domains
    print_status "Obtaining SSL certificates..."
    
    if sudo certbot --nginx \
        -d "$RPC_DOMAIN" \
        -d "$EVM_DOMAIN" \
        --non-interactive \
        --agree-tos \
        $email_flag \
        --redirect; then
        
        print_success "✓ SSL certificates obtained successfully"
        
        # Setup auto-renewal
        print_status "Setting up automatic certificate renewal..."
        
        # Check if systemd timer exists (newer systems)
        if systemctl list-unit-files | grep -q "certbot.timer"; then
            # Enable certbot timer if it exists
            sudo systemctl enable certbot.timer
            sudo systemctl start certbot.timer
            print_success "✓ Certbot systemd timer enabled"
        elif systemctl list-unit-files | grep -q "snap.certbot.renew.timer"; then
            # For snap installations
            sudo systemctl enable snap.certbot.renew.timer
            sudo systemctl start snap.certbot.renew.timer
            print_success "✓ Certbot snap timer enabled"
        else
            # Fall back to cron job
            if ! crontab -l 2>/dev/null | grep -q "certbot renew"; then
                # Add cron job for renewal twice daily
                (crontab -l 2>/dev/null; echo "0 0,12 * * * /usr/bin/certbot renew --quiet --post-hook 'systemctl reload nginx'") | crontab -
                print_success "✓ Certbot cron job added"
            else
                print_success "✓ Certbot renewal already configured"
            fi
        fi
        
        # Test renewal process
        print_status "Testing certificate renewal..."
        if sudo certbot renew --dry-run; then
            print_success "✓ Automatic renewal test passed"
        else
            print_warning "⚠ Renewal test failed - check certbot configuration"
        fi
    else
        print_error "Failed to obtain SSL certificates"
        echo
        echo "Common issues:"
        echo "- DNS records not properly configured"
        echo "- Ports 80/443 not accessible"
        echo "- Rate limits (try again later)"
        return 1
    fi
    
    return 0
}

# Add security enhancements
add_security_config() {
    print_status "Adding security enhancements..."
    
    # Create security snippet
    cat <<EOF | sudo tee /etc/nginx/snippets/push-node-security.conf > /dev/null
# Security headers
add_header Strict-Transport-Security "max-age=63072000; includeSubDomains; preload" always;
add_header X-Frame-Options "DENY" always;
add_header X-Content-Type-Options "nosniff" always;
add_header X-XSS-Protection "1; mode=block" always;
add_header Referrer-Policy "no-referrer-when-downgrade" always;

# SSL configuration
ssl_protocols TLSv1.2 TLSv1.3;
ssl_prefer_server_ciphers off;
ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384;
ssl_session_cache shared:SSL:10m;
ssl_session_timeout 10m;
ssl_stapling on;
ssl_stapling_verify on;
EOF

    # Create rate limiting configuration
    cat <<EOF | sudo tee /etc/nginx/snippets/push-node-ratelimit.conf > /dev/null
# Rate limiting zones
limit_req_zone \$binary_remote_addr zone=api_limit:10m rate=10r/s;
limit_req_zone \$binary_remote_addr zone=rpc_limit:10m rate=30r/s;
limit_req_zone \$binary_remote_addr zone=evm_limit:10m rate=50r/s;
EOF

    # Include security config in main nginx.conf
    if ! grep -q "push-node-security.conf" /etc/nginx/sites-available/$NGINX_CONFIG_NAME; then
        # Add include statements to each server block
        sudo sed -i '/listen 443 ssl http2;/a\    include /etc/nginx/snippets/push-node-security.conf;' "$NGINX_SITES_AVAILABLE/$NGINX_CONFIG_NAME"
    fi
    
    # Reload nginx
    sudo nginx -t && sudo systemctl reload nginx
    
    print_success "✓ Security enhancements added"
}

# Test endpoints
test_endpoints() {
    print_status "Testing configured endpoints..."
    echo
    
    local all_good=true
    
    # Test Cosmos RPC
    print_status "Testing Cosmos RPC..."
    if curl -s --connect-timeout 5 "https://$RPC_DOMAIN/status" | jq -e '.result.node_info' > /dev/null 2>&1; then
        print_success "✓ Cosmos RPC: https://$RPC_DOMAIN"
    else
        print_error "✗ Cosmos RPC not responding"
        all_good=false
    fi
    
    # Test EVM RPC
    print_status "Testing EVM RPC..."
    if curl -s --connect-timeout 5 -X POST "https://$EVM_DOMAIN" \
        -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | \
        jq -e '.result' > /dev/null 2>&1; then
        print_success "✓ EVM RPC: https://$EVM_DOMAIN"
    else
        print_error "✗ EVM RPC not responding"
        all_good=false
    fi
    
    
    echo
    if [ "$all_good" = true ]; then
        print_success "✓ All endpoints are working!"
    else
        print_warning "Some endpoints are not responding. This might be normal if your node is still syncing."
    fi
    
    return 0
}

# Show summary
show_summary() {
    echo
    echo -e "${BOLD}${GREEN}🎉 Public validator setup complete!${NC}"
    echo
    echo -e "${BOLD}${CYAN}Your HTTPS endpoints:${NC}"
    echo "════════════════════════════════════════════════"
    echo -e "Cosmos RPC:  ${GREEN}https://$RPC_DOMAIN${NC}"
    echo -e "EVM RPC:     ${GREEN}https://$EVM_DOMAIN${NC} (HTTP & WebSocket)"
    echo
    echo -e "${BOLD}${BLUE}Next steps:${NC}"
    echo "1. Test your endpoints with the URLs above"
    echo "2. Monitor nginx logs: sudo tail -f /var/log/nginx/access.log"
    echo "3. Check SSL renewal: sudo certbot renew --dry-run"
    echo "4. View renewal status: sudo systemctl status certbot.timer (or crontab -l)"
    echo
    echo -e "${YELLOW}Security recommendations:${NC}"
    echo "- Consider adding IP whitelisting if not public"
    echo "- Monitor logs for suspicious activity"
    echo "- Keep your system and nginx updated"
    echo "- Use a CDN for additional protection"
    echo
}

# Main setup flow
main() {
    show_public_setup_banner
    
    # Check prerequisites
    if ! check_prerequisites; then
        exit 1
    fi
    
    # Get domain configuration
    if ! get_domain_config; then
        print_error "Domain configuration cancelled"
        exit 1
    fi
    
    # Check DNS
    if ! check_dns; then
        print_error "DNS check failed"
        exit 1
    fi
    
    # Install required packages
    if ! install_packages; then
        print_error "Package installation failed"
        exit 1
    fi
    
    # Configure firewall
    configure_firewall
    
    # Create nginx configuration
    if ! create_nginx_config; then
        print_error "Nginx configuration failed"
        exit 1
    fi
    
    # Setup SSL
    if ! setup_ssl; then
        print_error "SSL setup failed"
        # Continue anyway - user can set up SSL manually
    fi
    
    # Add security enhancements
    add_security_config
    
    # Test endpoints
    test_endpoints
    
    # Show summary
    show_summary
}

# Run main function
main "$@"