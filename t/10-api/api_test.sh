#!/bin/sh

# API Sanity Test
# Verifies: API server startup and basic connectivity.

set -e
set -x
source "$(dirname "$0")/../common.sh"

TEST_TIMEOUT=30

require_root
require_binary
cleanup_on_exit

diag "Test: API Server Sanity"

# Create a temporary config for the control plane
CTL_CONFIG=$(mktemp_compatible "ctl_test.hcl")
cat > "$CTL_CONFIG" << 'EOF'
schema_version = "1.1"
api {
  enabled = false
  require_auth = false
}
EOF

# Start control plane using the helper from common.sh
start_ctl "$CTL_CONFIG"
diag "Control plane started (PID $CTL_PID)"

# Start API server (using test-api to bypass sandbox)
export GLACIC_NO_SANDBOX=1
start_api -listen :8099

# Test connectivity
diag "Testing connectivity to http://127.0.0.1:8099/api/status..."
if curl -s --connect-timeout 5 http://127.0.0.1:8099/api/status > /dev/null; then
    pass "API server reachable"
else
    diag "API server unreachable!"
    diag "--- API LOG ---"
    cat "$API_LOG" | sed 's/^/# /'
    diag "--- END LOG ---"
    fail "API server unreachable"
fi


diag "API test completed"
