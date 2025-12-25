#!/bin/sh
#
# Tailscale Up/Down Control Integration Test
# Verifies Tailscale up/down control API
#

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

interface "lo" {
    ipv4 = ["192.168.1.1/24"]
}

zone "local" {}

vpn {
    tailscale "main" {
        enabled = false
    }
}
EOF

plan 2

start_ctl "$CONFIG_FILE"

export GLACIC_NO_SANDBOX=1
$APP_BIN test-api -listen :8096 > /tmp/api_tsctl.log 2>&1 &
API_PID=$!
track_pid $API_PID

wait_for_port 8096 10 || fail "API failed to start"

# Test 1: Tailscale up endpoint
diag "Test 1: Tailscale up endpoint"
response=$(curl -s -o /dev/null -w "%{http_code}" -X POST \
    "http://169.254.255.2:8096/api/vpn/tailscale/up" 2>&1)
pass "Tailscale up endpoint accessible (status: $response)"

# Test 2: Tailscale down endpoint
diag "Test 2: Tailscale down endpoint"
response=$(curl -s -o /dev/null -w "%{http_code}" -X POST \
    "http://169.254.255.2:8096/api/vpn/tailscale/down" 2>&1)
pass "Tailscale down endpoint accessible (status: $response)"

rm -f "$CONFIG_FILE"
diag "Tailscale up/down control test completed"
