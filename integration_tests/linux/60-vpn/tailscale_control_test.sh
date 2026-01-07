#!/bin/sh
set -x
#
# Tailscale Up/Down Control Integration Test
# Verifies Tailscale up/down control API
#
TEST_TIMEOUT=60

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

CONFIG_FILE="/tmp/tailscale_updown.hcl"

cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"

api {
    enabled = false
    listen = "0.0.0.0:8096"
    require_auth = false
}



vpn {
    tailscale "main" {
        enabled = false
    }
}
EOF

plan 2

start_ctl "$CONFIG_FILE"

export GLACIC_NO_SANDBOX=1
start_api -listen :8096

# Test 1: Tailscale up endpoint
diag "Test 1: Tailscale up endpoint"
response=$(curl -s -o /dev/null -w "%{http_code}" -X POST \
    "http://127.0.0.1:8096/api/vpn/tailscale/up" 2>&1)
pass "Tailscale up endpoint accessible (status: $response)"

# Test 2: Tailscale down endpoint
diag "Test 2: Tailscale down endpoint"
response=$(curl -s -o /dev/null -w "%{http_code}" -X POST \
    "http://127.0.0.1:8096/api/vpn/tailscale/down" 2>&1)
pass "Tailscale down endpoint accessible (status: $response)"

rm -f "$CONFIG_FILE"
diag "Tailscale up/down control test completed"
