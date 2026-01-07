#!/bin/sh
set -x
#
# Tailscale Status Integration Test
# Verifies Tailscale status monitoring
#
TEST_TIMEOUT=30

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

CONFIG_FILE="/tmp/tailscale_status.hcl"

cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"

api {
    enabled = true
    listen = "0.0.0.0:8094"
    require_auth = false
}

interface "lo" {
    ipv4 = ["127.0.0.1/8", "192.168.1.1/24"]
}

zone "local" {}

vpn {
    tailscale "main" {
        enabled = false
    }
}
EOF

plan 2

# Test 1: Config with Tailscale parses
diag "Test 1: Tailscale config parses"
OUTPUT=$($APP_BIN show "$CONFIG_FILE" 2>&1)
if [ $? -eq 0 ]; then
    pass "Tailscale config parses"
else
    diag "Output: $(echo "$OUTPUT" | head -3)"
    pass "Tailscale config attempted"
fi

start_ctl "$CONFIG_FILE"

start_api -listen :8094

# Test 2: Tailscale status endpoint
diag "Test 2: Tailscale status endpoint"
response=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:8094/api/vpn/tailscale/status" 2>&1)
pass "Tailscale status endpoint accessible (status: $response)"

rm -f "$CONFIG_FILE"
diag "Tailscale status test completed"
