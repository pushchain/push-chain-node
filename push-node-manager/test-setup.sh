#!/bin/bash
# Quick test of the validator setup

set -e

GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m'

echo -e "${BLUE}🧪 Testing Push Chain Validator Setup${NC}"
echo "====================================="
echo ""

# Check files
echo -e "${BLUE}1. Checking required files...${NC}"
FILES=(
    ".env"
    "docker-compose.yml"
    "Dockerfile"
    "push-node-manager"
    "scripts/entrypoint.sh"
    "scripts/health-check.sh"
    "configs/networks.json"
)

for file in "${FILES[@]}"; do
    if [ -f "$file" ]; then
        echo -e "${GREEN}✓${NC} $file"
    else
        echo -e "${RED}✗${NC} $file missing"
    fi
done

# Check .env configuration
echo ""
echo -e "${BLUE}2. Checking configuration...${NC}"
if [ -f .env ]; then
    source .env
    echo -e "${GREEN}✓${NC} Network: $NETWORK"
    echo -e "${GREEN}✓${NC} Moniker: $MONIKER"
else
    echo -e "${RED}✗${NC} .env file missing"
fi

# Check binary
echo ""
echo -e "${BLUE}3. Checking pchaind binary...${NC}"
if [ -f ../build/pchaind ]; then
    echo -e "${GREEN}✓${NC} Local binary found at ../build/pchaind"
    ls -lh ../build/pchaind
else
    echo -e "${YELLOW}⚠${NC} Local binary not found, will use download method"
fi

# Check Docker
echo ""
echo -e "${BLUE}4. Checking Docker...${NC}"
if docker compose version >/dev/null 2>&1; then
    echo -e "${GREEN}✓${NC} Docker Compose is available"
else
    echo -e "${RED}✗${NC} Docker Compose not found"
fi

# Check if image is built
echo ""
echo -e "${BLUE}5. Checking Docker image...${NC}"
if docker images | grep -q "push-node-manager.*local"; then
    echo -e "${GREEN}✓${NC} Docker image is built"
else
    echo -e "${YELLOW}⚠${NC} Docker image not built yet"
fi

echo ""
echo -e "${GREEN}✅ Setup verification complete!${NC}"
echo ""
echo "Next steps:"
echo "1. Build image: docker compose build"
echo "2. Start validator: ./push-node-manager start"