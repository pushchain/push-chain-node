#!/bin/bash

# Push Chain Validator Auto-Installer
# Downloads and sets up the validator in one command

set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
NC='\033[0m'
BOLD='\033[1m'

echo -e "${BOLD}${BLUE}Push Chain Validator Installer${NC}"
echo "================================="
echo

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo -e "${RED}❌ Docker is not installed!${NC}"
    echo "Please install Docker first: https://docs.docker.com/get-docker/"
    exit 1
fi

# Check if Docker is running
if ! docker info &> /dev/null; then
    echo -e "${RED}❌ Docker is not running!${NC}"
    echo "Please start Docker and try again."
    exit 1
fi

echo -e "${GREEN}✓ Docker is installed and running${NC}"

# Set the correct branch and repo
REPO="pushchain/push-chain-node"
BRANCH="feature/validator-node-setup"
BASE_URL="https://raw.githubusercontent.com/${REPO}/${BRANCH}/validator"

# Create directory
echo -e "\n${BLUE}Creating validator directory...${NC}"
mkdir -p push-node-manager
cd push-node-manager

# Download required files
echo -e "\n${BLUE}Downloading validator files...${NC}"

# Download main files
echo "Downloading push-node-manager script..."
curl -sSL "${BASE_URL}/push-node-manager" -o push-node-manager || { echo -e "${RED}Failed to download push-node-manager${NC}"; exit 1; }

echo "Downloading docker-compose.yml..."
curl -sSL "${BASE_URL}/docker-compose.yml" -o docker-compose.yml || { echo -e "${RED}Failed to download docker-compose.yml${NC}"; exit 1; }

echo "Downloading Dockerfile..."
curl -sSL "${BASE_URL}/Dockerfile" -o Dockerfile || { echo -e "${RED}Failed to download Dockerfile${NC}"; exit 1; }

echo "Downloading README.md..."
curl -sSL "${BASE_URL}/README.md" -o README.md || true

# Create scripts directory
mkdir -p scripts
cd scripts

# Download all script files
for script in common.sh entrypoint.sh setup-node-registration.sh import-wallet.sh; do
    echo "Downloading $script..."
    curl -sSL "${BASE_URL}/scripts/$script" -o $script || { echo -e "${RED}Failed to download $script${NC}"; exit 1; }
done

cd ..

# Create configs directory and download networks.json
mkdir -p configs
echo "Downloading networks.json..."
curl -sSL "${BASE_URL}/configs/networks.json" -o configs/networks.json || { echo -e "${RED}Failed to download networks.json${NC}"; exit 1; }

# Make scripts executable
chmod +x push-node-manager
chmod +x scripts/*.sh

echo -e "\n${GREEN}✓ Downloaded all files successfully!${NC}"

# Build the Docker image
echo -e "\n${BLUE}Building validator Docker image...${NC}"
if command -v docker-compose &> /dev/null; then
    docker-compose build
else
    docker compose build
fi

echo -e "\n${GREEN}✓ Installation complete!${NC}"
echo
echo -e "${BOLD}${BLUE}🎉 Ready to start!${NC}"
echo
echo "Run the setup wizard:"
echo -e "  ${BOLD}./push-node-manager setup${NC}"
echo
echo "This will guide you through:"
echo "  1. Creating a wallet"
echo "  2. Getting test tokens" 
echo "  3. Registering as a validator"
echo
echo -e "${YELLOW}Note: The setup process takes about 2-3 minutes${NC}"
echo
read -p "Start the setup wizard now? (yes/no): " start_now
if [[ "$start_now" =~ ^[Yy][Ee][Ss]$ ]]; then
    ./push-node-manager setup
fi