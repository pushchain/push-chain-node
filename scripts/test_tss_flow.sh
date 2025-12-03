#!/bin/bash

# TSS Complete Testing Flow Script
# This script automates the complete testing flow for TSS operations

set -e

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Node configurations
NODE1_VALIDATOR="pushvaloper1fv2fm76q7cjnr58wdwyntzrjgtc7qya6n7dmlu"
NODE1_KEY="30B0D912700C3DF94F4743F440D1613F7EA67E1CEF32C73B925DB6CD7F1A1544"
NODE1_PORT="39001"

NODE2_VALIDATOR="pushvaloper12jzrpp4pkucxxvj6hw4dfxsnhcpy6ddty2fl75"
NODE2_KEY="59BA39BF8BCFE835B6ABD7FE5208D8B8AEFF7B467F9FE76F1F43ED392E5B9432"
NODE2_PORT="39002"

NODE3_VALIDATOR="pushvaloper1vzuw2x3k2ccme70zcgswv8d88kyc07grdpvw3e"
NODE3_KEY="957590C7179F8645368162418A3DF817E5663BBC7C24D0EFE1D64EFFB11DC595"
NODE3_PORT="39003"

NODE4_VALIDATOR="pushvaloper1a4b5c6d7e8f9g0h1i2j3k4l5m6n7o8p9q0r1s2t3u4v5w6x7y8z9"
NODE4_KEY="b770bee33ec21760b3407354f563219fdabf0ee697fcf73271ab36758c5bd943"
NODE4_PORT="39004"

BINARY="./build/tss"

echo -e "${BLUE}=== TSS Complete Testing Flow ===${NC}\n"

# Step 0: Build binary first (if prepare command exists, it will rebuild, otherwise this ensures we have a binary)
echo -e "${YELLOW}Step 0: Building binary...${NC}"
go build -o ./build/tss ./cmd/tss
echo -e "${GREEN}✓ Binary built${NC}\n"

# Step 1: Prepare environment (clean and build)
echo -e "${YELLOW}Step 1: Preparing environment (cleaning and building)...${NC}"
$BINARY prepare
echo -e "${GREEN}✓ Prepared${NC}\n"
sleep 2

# Step 2: Start Node 1 (Pending Join)
echo -e "${YELLOW}Step 2: Starting Node 1 (Pending Join)...${NC}"
echo "Run this in a separate terminal:"
echo -e "${BLUE}$BINARY node -validator-address=$NODE1_VALIDATOR -private-key=$NODE1_KEY -p2p-listen=/ip4/127.0.0.1/tcp/$NODE1_PORT${NC}"
echo "Press Enter after Node 1 is running..."
read

# Step 3: Start Node 2 (Pending Join)
echo -e "${YELLOW}Step 3: Starting Node 2 (Pending Join)...${NC}"
echo "Run this in a separate terminal:"
echo -e "${BLUE}$BINARY node -validator-address=$NODE2_VALIDATOR -private-key=$NODE2_KEY -p2p-listen=/ip4/127.0.0.1/tcp/$NODE2_PORT${NC}"
echo "Press Enter after Node 2 is running..."
read

# Step 4: Start Node 3 (Pending Join)
echo -e "${YELLOW}Step 4: Starting Node 3 (Pending Join)...${NC}"
echo "Run this in a separate terminal:"
echo -e "${BLUE}$BINARY node -validator-address=$NODE3_VALIDATOR -private-key=$NODE3_KEY -p2p-listen=/ip4/127.0.0.1/tcp/$NODE3_PORT${NC}"
echo "Press Enter after Node 3 is running..."
read

sleep 3

# Step 5: Do keygen
echo -e "${YELLOW}Step 5: Ready to run keygen...${NC}"
echo "Press Enter to start keygen..."
read
$BINARY keygen
echo -e "${GREEN}✓ Keygen completed${NC}\n"
echo "Review the keygen results. Press Enter once satisfied to mark nodes as Active..."
read

# Mark nodes as active
echo -e "${YELLOW}Marking Node 1, 2 & 3 as Active...${NC}"
$BINARY status -validator-address=$NODE1_VALIDATOR -status=active
$BINARY status -validator-address=$NODE2_VALIDATOR -status=active
$BINARY status -validator-address=$NODE3_VALIDATOR -status=active
echo -e "${GREEN}✓ Nodes marked as Active${NC}\n"
sleep 2

# Step 6: Do sign
echo -e "${YELLOW}Step 6: Ready to sign message 1...${NC}"
echo "Press Enter to start signing..."
read
$BINARY sign -message="Test message 1"
echo -e "${GREEN}✓ Sign completed${NC}\n"
sleep 2

# Step 7: Start Node 4 (Pending Join)
echo -e "${YELLOW}Step 7: Starting Node 4 (Pending Join)...${NC}"
echo "Run this in a separate terminal:"
echo -e "${BLUE}$BINARY node -validator-address=$NODE4_VALIDATOR -private-key=$NODE4_KEY -p2p-listen=/ip4/127.0.0.1/tcp/$NODE4_PORT${NC}"
echo "Press Enter after Node 4 is running..."
read

sleep 3

# Step 8: Do sign (with 3 active nodes)
echo -e "${YELLOW}Step 8: Ready to sign message 2 (with 3 active nodes)...${NC}"
echo "Press Enter to start signing..."
read
$BINARY sign -message="Test message 2"
echo -e "${GREEN}✓ Sign completed${NC}\n"
echo "Review the sign results. Press Enter to continue to QC..."
read

# Step 9: Do QC - Adds Node 4
echo -e "${YELLOW}Step 9: Ready to run QC (adds Node 4)...${NC}"
echo "Press Enter to start QC..."
read
$BINARY qc
echo -e "${GREEN}✓ QC completed${NC}\n"
sleep 2

# Mark Node 4 as active
echo -e "${YELLOW}Marking Node 4 as Active...${NC}"
$BINARY status -validator-address=$NODE4_VALIDATOR -status=active
echo -e "${GREEN}✓ Node 4 marked as Active${NC}\n"
sleep 2

# Sign with 4 active nodes
echo -e "${YELLOW}Ready to sign message 3 (with 4 active nodes: Node 1, 2, 3 & 4)...${NC}"
echo "Press Enter to start signing..."
read
$BINARY sign -message="Test message 3"
echo -e "${GREEN}✓ Sign completed${NC}\n"
echo "Review the sign results. Press Enter to continue..."
read

# Step 10: Mark Node 2 as Pending Leave
echo -e "${YELLOW}Step 10: Marking Node 2 as Pending Leave...${NC}"
echo "Stop Node 2 process (Ctrl+C in its terminal) and press Enter..."
read
$BINARY status -validator-address=$NODE2_VALIDATOR -status=pending_leave
echo -e "${GREEN}✓ Node 2 marked as Pending Leave${NC}\n"
sleep 2

# Do QC to remove Node 2
echo -e "${YELLOW}Ready to run QC (removes Node 2)...${NC}"
echo "Press Enter to start QC..."
read
$BINARY qc
echo -e "${GREEN}✓ QC completed${NC}\n"
sleep 2

# Mark Node 2 as inactive
echo -e "${YELLOW}Marking Node 2 as Inactive...${NC}"
$BINARY status -validator-address=$NODE2_VALIDATOR -status=inactive
echo -e "${GREEN}✓ Node 2 marked as Inactive${NC}\n"
sleep 2

# Step 11: Do sign (with 3 active nodes: Node 1, 3 & 4)
echo -e "${YELLOW}Step 11: Ready to sign message 4 (with 3 active nodes: Node 1, 3 & 4)...${NC}"
echo "Press Enter to start signing..."
read
$BINARY sign -message="Test message 4"
echo -e "${GREEN}✓ Sign completed${NC}\n"

echo -e "${GREEN}=== Testing Flow Completed Successfully! ===${NC}"

