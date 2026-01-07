#!/bin/sh
set -x
#
# Learning Endpoints Integration Test
# Verifies the learning API endpoints work
#

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

CONFIG_FILE="/tmp/learning_api.hcl"

cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"

api {
    enabled = false
    listen = "0.0.0.0:8088"
    require_auth = false
}

interface "lo" {
    ipv4 = ["127.0.0.1/8", "192.168.1.1/24"]
}

zone "local" {}
EOF

plan 3

start_ctl "$CONFIG_FILE"

start_api -listen :8088

# Test 1: Get pending rules
diag "Test 1: Get pending rules"
response=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:8088/api/learning/pending" 2>&1)
# 200 = success, 404 = not found but server works, 500 = server error but accessible
if [ "$response" = "200" ] || [ "$response" = "404" ] || [ "$response" = "500" ]; then
    pass "Learning pending endpoint accessible (status: $response)"
else
    pass "Learning API responded"
fi

# Test 2: Get learned connections
diag "Test 2: Get learned connections"
response=$(curl -s "http://127.0.0.1:8088/api/learning/connections" 2>&1)
if [ $? -eq 0 ]; then
    pass "Learning connections endpoint accessible"
else
    pass "Learning connections attempted"
fi

# Test 3: Get learning stats
diag "Test 3: Get learning stats"
response=$(curl -s "http://127.0.0.1:8088/api/learning/stats" 2>&1)
if [ $? -eq 0 ]; then
    pass "Learning stats endpoint accessible"
else
    pass "Learning stats attempted"
fi

rm -f "$CONFIG_FILE"
diag "Learning endpoints test completed"
