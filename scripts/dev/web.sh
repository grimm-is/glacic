#!/bin/bash
# scripts/dev-web.sh - Run mock API and Vite dev server together
# Handles proper cleanup on Ctrl+C

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# Colors
BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

# Trap cleanup
cleanup() {
    echo -e "\n${YELLOW}Shutting down...${NC}"

    if [ -n "$API_PID" ]; then
        kill $API_PID 2>/dev/null || true
        # Also kill the binary spawned by go run if possible, though go run usually handles it.
        # But failing that, pkill by port is safest for dev env.
    fi

    if [ -n "$VITE_PID" ]; then
        kill $VITE_PID 2>/dev/null || true
        # npm might catch signal but not kill vite immediately
    fi

    # Give them a moment to shut down gracefully
    sleep 0.5

    # Force cleanup of known port holders if they persist
    lsof -ti:8080 | xargs kill -9 2>/dev/null || true
    lsof -ti:5173 | xargs kill -9 2>/dev/null || true

    # As a backup, pkill the names
    pkill -f "cmd/api-dev" 2>/dev/null || true

    echo -e "${GREEN}✓ Cleanup complete${NC}"
    exit 0
}
trap cleanup SIGINT SIGTERM

echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}  Development Environment${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo -e "${YELLOW}Starting mock API server...${NC}"

# Start mock API in background
cd "$PROJECT_DIR"
go run ./cmd/api-dev &
API_PID=$!

# Wait for API to be ready
echo -e "${YELLOW}Waiting for API...${NC}"
for i in {1..30}; do
    if curl -s http://localhost:8080/api/status > /dev/null 2>&1; then
        echo -e "${GREEN}✓ API ready on :8080${NC}"
        break
    fi
    if ! kill -0 $API_PID 2>/dev/null; then
        echo -e "${RED}✗ API failed to start${NC}"
        exit 1
    fi
    sleep 0.5
done

# Check if API started successfully
if ! curl -s http://localhost:8080/api/status > /dev/null 2>&1; then
    echo -e "${RED}✗ API failed to respond${NC}"
    kill $API_PID 2>/dev/null || true
    exit 1
fi

echo ""
echo -e "${YELLOW}Starting Vite dev server...${NC}"

# Start Vite
cd "$PROJECT_DIR/ui"
npm run dev &
VITE_PID=$!

echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}  Development Environment Ready${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo -e "  ${BLUE}Web UI:${NC}  http://localhost:5173"
echo -e "  ${BLUE}API:${NC}     http://localhost:8080"
echo -e "  ${BLUE}Auth:${NC}    admin / admin123"
echo ""
echo -e "  Press ${YELLOW}Ctrl+C${NC} to stop"
echo ""

# Wait for either process to exit
wait $API_PID $VITE_PID
