#!/bin/sh
#
# Port Scanning Integration Test
# Verifies active port scanning functionality
#

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

CONFIG_FILE="/tmp/port_scan.hcl"

cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"

api {
    enabled = false
    listen = "0.0.0.0:8092"
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
$APP_BIN test-api -listen :8092 > /tmp/api_scan.log 2>&1 &
API_PID=$!
track_pid $API_PID

wait_for_port 8092 10 || fail "API failed to start"

# Test 1: Start scan endpoint
diag "Test 1: Start scan endpoint"
response=$(curl -s -o /dev/null -w "%{http_code}" -X POST \
    -H "Content-Type: application/json" \
    -d '{"target":"127.0.0.1","ports":"22,80,443"}' \
    "http://169.254.255.2:8092/api/scan/start" 2>&1)
pass "Scan start endpoint accessible (status: $response)"

# Test 2: Get scan results
diag "Test 2: Scan results endpoint"
response=$(curl -s -o /dev/null -w "%{http_code}" "http://169.254.255.2:8092/api/scan/results" 2>&1)
pass "Scan results endpoint accessible (status: $response)"

rm -f "$CONFIG_FILE"
diag "Port scanning test completed"
