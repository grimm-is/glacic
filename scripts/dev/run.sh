#!/bin/bash
set -e

# Development script: Build UI + Firewall, launch VM with UI accessible at http://localhost:8080

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}  Firewall Development Environment${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

# ==============================================================================
# 1. Build UI
# ==============================================================================
echo -e "\n${YELLOW}[1/3] Building UI...${NC}"

cd ui

# Check if node_modules exists
if [ ! -d "node_modules" ]; then
    echo "  Installing npm dependencies..."
    npm install --silent
fi

# Build the UI
echo "  Compiling Svelte app..."
npm run build --silent

cd ..

echo -e "${GREEN}  ✓ UI built successfully${NC}"

# ==============================================================================
# 2. Build Firewall Binary
# ==============================================================================
echo -e "\n${YELLOW}[2/3] Building firewall binary...${NC}"

GOOS=linux go build -o glacic .

echo -e "${GREEN}  ✓ Firewall binary built${NC}"

# ==============================================================================
# 3. Check Alpine VM Image
# ==============================================================================
echo -e "\n${YELLOW}[3/3] Checking VM image...${NC}"

if [ ! -f "build/rootfs.ext4" ]; then
    echo "  Building Alpine VM image (first time setup)..."
    perl build_alpine.pl
else
    echo -e "${GREEN}  ✓ VM image exists${NC}"
fi

# ==============================================================================
# 4. Launch VM
# ==============================================================================
echo ""
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}  Starting Firewall VM${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo -e "  ${GREEN}➜${NC}  Web UI:  ${BLUE}http://localhost:8080${NC}"
echo -e "  ${GREEN}➜${NC}  API:     ${BLUE}http://localhost:8080/api/status${NC}"
echo ""
echo -e "  Press ${YELLOW}Ctrl+A, X${NC} to exit VM"
echo ""
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

# Use firewall.hcl by default, or accept a custom config
CONFIG_FILE="${1:-firewall.hcl}"

# Pass dev_mode=true as kernel parameter to trigger interactive mode
./../vm/dev.sh "$(pwd)" "$CONFIG_FILE" "dev_mode=true"
