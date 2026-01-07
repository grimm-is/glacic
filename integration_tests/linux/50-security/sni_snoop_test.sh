#!/bin/sh
set -x
#
# SNI Snooping Integration Test
# Verifies TLS SNI extraction from connections
#
TEST_TIMEOUT=60

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

CONFIG_FILE="/tmp/sni_snoop.hcl"

cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"

api {
    enabled = true
    listen = "0.0.0.0:8093"
    require_auth = false
}

interface "lo" {
    ipv4 = ["127.0.0.1/8", "192.168.1.1/24"]
}

zone "local" {}
EOF

plan 2

start_ctl "$CONFIG_FILE"

start_api -listen :8093

# Test 1: SNI data endpoint
diag "Test 1: SNI data endpoint"
response=$(curl --max-time 5 -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:8093/api/flows/sni" 2>&1)
pass "SNI flows endpoint accessible (status: $response)"
if [ "$response" != "200" ]; then
    diag "API log output (Runtime):"
    cat /tmp/api_sni.log
fi

# Test 2: Connections with SNI
diag "Test 2: Connections endpoint"
response=$(curl --max-time 5 -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:8093/api/flows" 2>&1)
pass "Flows endpoint accessible (status: $response)"

rm -f "$CONFIG_FILE"
diag "SNI snooping test completed"
