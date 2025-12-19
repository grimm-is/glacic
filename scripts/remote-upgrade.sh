#!/bin/bash
# Remote Upgrade Script for Glacic
# Queries remote for architecture, builds, and uploads binary

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Usage
if [[ $# -lt 2 ]]; then
    echo "Usage: $0 <remote-url> <api-key>"
    echo "Example: $0 https://172.30.20.101:8443 gfw_xxx..."
    exit 1
fi

REMOTE_URL="$1"
API_KEY="$2"

echo -e "${YELLOW}Querying remote system...${NC}"

# Query remote for status (insecure for self-signed certs)
STATUS=$(curl -sk -H "X-API-Key: ${API_KEY}" "${REMOTE_URL}/api/status")

if [[ -z "$STATUS" ]]; then
    echo -e "${RED}Error: Failed to connect to remote${NC}"
    exit 1
fi

# Extract architecture from status
ARCH=$(echo "$STATUS" | jq -r '.build_arch // empty')
VERSION=$(echo "$STATUS" | jq -r '.version // "unknown"')

if [[ -z "$ARCH" ]]; then
    echo -e "${RED}Error: Remote did not report architecture${NC}"
    echo "Response: $STATUS"
    exit 1
fi

echo -e "${GREEN}Remote:${NC} ${REMOTE_URL}"
echo -e "${GREEN}Version:${NC} ${VERSION}"
echo -e "${GREEN}Architecture:${NC} ${ARCH}"

# Parse architecture (format: linux/arm64)
TARGET_OS="${ARCH%%/*}"
TARGET_ARCH="${ARCH##*/}"

if [[ "$TARGET_OS" != "linux" ]]; then
    echo -e "${RED}Error: Only Linux targets are supported (got: ${TARGET_OS})${NC}"
    exit 1
fi

echo ""
echo -e "${YELLOW}Building for ${TARGET_ARCH}...${NC}"

# Build for target architecture
if [[ "$TARGET_ARCH" == "amd64" ]]; then
    make build LINUX_ARCH=amd64
elif [[ "$TARGET_ARCH" == "arm64" ]]; then
    make build LINUX_ARCH=arm64
else
    echo -e "${RED}Error: Unsupported architecture: ${TARGET_ARCH}${NC}"
    exit 1
fi

BINARY="build/glacic"

if [[ ! -f "$BINARY" ]]; then
    echo -e "${RED}Error: Build failed - binary not found at ${BINARY}${NC}"
    exit 1
fi

echo ""
echo -e "${YELLOW}Uploading binary...${NC}"

# Use the glacic-darwin CLI to perform the upgrade
./build/glacic-darwin upgrade \
    --remote "${REMOTE_URL}" \
    --binary "${BINARY}" \
    --api-key "${API_KEY}"

echo ""
echo -e "${GREEN}✓ Remote upgrade complete!${NC}"
