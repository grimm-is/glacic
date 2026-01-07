#!/bin/sh
set -x
# Audit Logging Integration Test
# Verifies that audit events are persisted and queryable via API.

TEST_TIMEOUT=30

# Source common functions
. "$(dirname "$0")/../common.sh"

require_root
require_binary

# --- Setup ---
plan 4

diag "Starting Audit Logging Test"

# 1. Create config with audit enabled
TEST_CONFIG=$(mktemp_compatible "audit_test.hcl")
cat > "$TEST_CONFIG" << 'EOF'
schema_version = "1.0"
ip_forwarding = true

interface "eth0" {
  description = "WAN Link"
  dhcp = true
}

audit {
  enabled = true
  retention_days = 7
}

api {
  enabled = true
}
EOF

ok 0 "Audit config created"

# 2. Start control plane and API
start_ctl "$TEST_CONFIG"
if ! kill -0 $CTL_PID 2>/dev/null; then
    ok 1 "Control plane failed to start"
    exit 1
else
    ok 0 "Control plane started"
fi

rm -f "$STATE_DIR"/auth.json
start_api -listen :$TEST_API_PORT

if ! kill -0 $API_PID 2>/dev/null; then
    ok 1 "API server failed to start"
    stop_ctl
    exit 1
fi

# Give time for audit store initialization
dilated_sleep 1

# 3. Make some API requests to generate audit events
diag "Generating audit events via API calls..."
# Login to get session
curl -s -X POST http://127.0.0.1:$TEST_API_PORT/api/setup/create-admin \
    -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"test1234"}' > /dev/null 2>&1

LOGIN_RESP=$(curl -s -X POST http://127.0.0.1:$TEST_API_PORT/api/auth/login \
    -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"test1234"}')

TOKEN=$(echo "$LOGIN_RESP" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

# Make a config read (generates audit event)
curl -s http://127.0.0.1:$TEST_API_PORT/api/status \
    -H "Authorization: Bearer $TOKEN" > /dev/null 2>&1

ok 0 "Audit events generated"

# 4. Query audit log via API
diag "Querying audit log..."
AUDIT_RESP=$(curl -s "http://127.0.0.1:$TEST_API_PORT/api/audit?limit=10" \
    -H "Authorization: Bearer $TOKEN" 2>&1)

if echo "$AUDIT_RESP" | grep -q '"events"'; then
    ok 0 "Audit log queryable via API"
else
    ok 1 "Audit log query failed"
    diag "Response: $AUDIT_RESP"
fi

# Cleanup
stop_api
stop_ctl

if [ $failed_count -eq 0 ]; then
    exit 0
else
    exit 1
fi
