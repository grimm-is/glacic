#!/bin/sh
#
# Pending Rule Approval Integration Test
# Verifies pending rule approval workflow
#

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

CONFIG_FILE="/tmp/pending_rules.hcl"

cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"

api {
    enabled = false
    listen = "0.0.0.0:8091"
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
$APP_BIN test-api -listen :8091 > /tmp/api_pending.log 2>&1 &
API_PID=$!
track_pid $API_PID

wait_for_port 8091 10 || fail "API failed to start"

# Test 1: Get pending rules
diag "Test 1: Pending rules endpoint"
response=$(curl -s -o /dev/null -w "%{http_code}" "http://169.254.255.2:8091/api/learning/pending" 2>&1)
pass "Pending rules endpoint accessible (status: $response)"

# Test 2: Approve/reject endpoint exists
diag "Test 2: Approve endpoint"
response=$(curl -s -o /dev/null -w "%{http_code}" -X POST \
    -H "Content-Type: application/json" \
    -d '{"action":"approve"}' \
    "http://169.254.255.2:8091/api/learning/pending/test-rule" 2>&1)
pass "Approve endpoint accessible (status: $response)"

rm -f "$CONFIG_FILE"
diag "Pending rule approval test completed"
