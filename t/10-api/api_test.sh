#!/bin/sh

# API Sanity Test
# Verifies: API server startup and basic connectivity.

set -e
source "$(dirname "$0")/../common.sh"

TEST_TIMEOUT=30

require_root
require_binary

diag "Test: API Server Sanity"

# Create a temporary config for the control plane
CTL_CONFIG=$(mktemp_compatible "ctl_test.hcl")
cat > "$CTL_CONFIG" << 'EOF'
schema_version = "1.1"
api {
  require_auth = false
}
EOF

# Start control plane using the helper from common.sh
start_ctl "$CTL_CONFIG"
diag "Control plane started (PID $CTL_PID)"

# Start API server (using test-api to bypass sandbox)
LOG_FILE="/tmp/api_test.log"
$APP_BIN test-api -listen 127.0.0.1:8080 > "$LOG_FILE" 2>&1 &
API_PID=$!
track_pid $API_PID

diag "Waiting for API startup..."
# Wait for port instead of static sleep
if ! wait_for_port 8080 15; then
    cat "$LOG_FILE" | sed 's/^/# /'
    fail "API server failed to start within 15 seconds"
fi

# Check if process is still running after port check
if ! kill -0 $API_PID 2>/dev/null; then
    cat "$LOG_FILE" | sed 's/^/# /'
    fail "API server failed to start"
fi

# Test connectivity
if wget -q -O - http://127.0.0.1:8080/api/status > /dev/null 2>&1; then
    pass "API server reachable (GET /api/status)"
else
    # Check if curl available
    if command -v curl >/dev/null 2>&1; then
        if curl -s http://127.0.0.1:8080/api/status > /dev/null; then
            pass "API server reachable (curl)"
        else
            fail "API server unreachable"
        fi
    else
        # Try nc?
        if echo -e "GET /api/status HTTP/1.0\r\n\r\n" | nc 127.0.0.1 8080 | grep "200 OK"; then
             pass "API server reachable (nc)"
        else
             fail "API server unreachable (wget/curl/nc failed)"
        fi
    fi
fi

# Cleanup
kill $API_PID 2>/dev/null
rm -f "$LOG_FILE"
