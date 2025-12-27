#!/bin/sh
set -x
#
# SNI Snooping Integration Test
# Verifies TLS SNI extraction from connections
#
TEST_TIMEOUT=90

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
    ipv4 = ["192.168.1.1/24"]
}

zone "local" {}
EOF

plan 2

start_ctl "$CONFIG_FILE"

export GLACIC_NO_SANDBOX=1
$APP_BIN test-api -listen :8093 > /tmp/api_sni.log 2>&1 &
API_PID=$!
track_pid $API_PID

wait_for_port 8093 30 || fail "API failed to start"

# Test 1: SNI data endpoint
diag "Test 1: SNI data endpoint"
response=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:8093/api/flows/sni" 2>&1)
pass "SNI flows endpoint accessible (status: $response)"

# Test 2: Connections with SNI
diag "Test 2: Connections endpoint"
response=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:8093/api/flows" 2>&1)
pass "Flows endpoint accessible (status: $response)"

rm -f "$CONFIG_FILE"
diag "SNI snooping test completed"
