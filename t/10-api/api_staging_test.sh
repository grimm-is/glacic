#!/bin/sh

# API Staging Integration Test
# Verifies: Staging validation, Unified Diff generation, and Discard functionality
# Based on api_crud_test.sh patterns

TEST_TIMEOUT=30

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

log() { echo "[TEST] $1"; }

CONFIG_FILE="/tmp/api_staging.hcl"
KEY_STORE="/tmp/apikeys_staging.json"

# configuration
cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"

api {
  enabled = false
  listen  = "127.0.0.1:8082"
  require_auth = true
  key_store_path = "$KEY_STORE"

  key "admin-key" {
    key = "gfw_staging123"
    permissions = ["config:write", "config:read", "firewall:write"]
  }
}

interface "eth0" {
    ipv4 = ["10.0.0.1/24"]
}
EOF

# Start Control Plane
log "Starting Control Plane..."
GLACIC_LOG_FILE=stdout $APP_BIN ctl "$CONFIG_FILE" > /tmp/ctl_staging.log 2>&1 &
CTL_PID=$!
track_pid $CTL_PID

# Wait for socket
wait_for_file $CTL_SOCKET 10 || fail "Control plane socket not created"

# Start API Server (disable sandbox)
log "Starting API Server..."
export GLACIC_NO_SANDBOX=1
$APP_BIN test-api -listen :8082 > /tmp/api_staging.log 2>&1 &
API_PID=$!
track_pid $API_PID

wait_for_port 8082 10 || fail "API server failed to start"

API_URL="http://127.0.0.1:8082/api"
AUTH_HEADER="X-API-Key: gfw_staging123"

# Helper for curl requests
api_post() {
    endpoint="$1"
    data="$2"
    if command -v curl >/dev/null 2>&1; then
        out=$(curl -s -w "\n%{http_code}" -X POST "$API_URL$endpoint" \
            -H "Content-Type: application/json" \
            -H "$AUTH_HEADER" \
            -d "$data")
        body=$(echo "$out" | sed '$d')
        code=$(echo "$out" | tail -n1)
        echo "$code $body"
    else
        fail "curl is required"
    fi
}

api_get() {
    endpoint="$1"
    if command -v curl >/dev/null 2>&1; then
        curl -s -H "$AUTH_HEADER" "$API_URL$endpoint"
    else
        fail "curl is required"
    fi
}

# Ensure clean state by discarding any potentially staged changes (should be none on fresh start)
DATA='{}'
RESULT=$(api_post "/config/discard" "$DATA")
log "Discard initial result: $RESULT"

# Test 1: Get Running Config (Baseline)
log "Fetching Running Config..."
RUNNING=$(api_get "/config?source=running")
# Basic check
if echo "$RUNNING" | grep -q "10.0.0.1"; then
    pass "Fetched running config"
else
    fail "Failed to fetch running config: $RUNNING"
fi

# Test 2: Make a Staged Change
log "Staging a change (Update Policies)..."
DATA='[{"name":"rule-staged","action":"accept","from_zone":"lan","to_zone":"wan"}]'
RESULT=$(api_post "/config/policies" "$DATA")
CODE=$(echo "$RESULT" | awk '{print $1}')
if [ "$CODE" = "200" ]; then
    pass "Staged policy update"
else
    fail "Staged policy update failed: $RESULT"
fi

# Test 3: Verify Staged Config shows change
log "Verifying Staged Config..."
STAGED=$(api_get "/config?source=staged")
if echo "$STAGED" | grep -q "rule-staged"; then
    pass "Staged config contains new rule"
else
    fail "Staged config missing new rule: $STAGED"
fi

# Test 4: Verify Running Config is UNCHANGED
log "Verifying Running Config is unchanged..."
RUNNING_AGAIN=$(api_get "/config?source=running")
if echo "$RUNNING_AGAIN" | grep -q "rule-staged"; then
    fail "Running config was prematurely updated!"
else
    pass "Running config remained unchanged"
fi

# Test 5: Verify Diff
log "Verifying Diff..."
DIFF=$(curl -s -H "$AUTH_HEADER" "$API_URL/config/diff")
log "Diff Output:"
echo "$DIFF"

if echo "$DIFF" | grep -q "rule-staged"; then
    pass "Diff shows new rule"
else
    fail "Diff missing new rule"
fi

if echo "$DIFF" | grep -q "Running"; then
    pass "Diff contains header"
else
    fail "Diff missing header"
fi

# Test 6: Discard Changes
log "Discarding changes..."
RESULT=$(api_post "/config/discard" "{}")
CODE=$(echo "$RESULT" | awk '{print $1}')
if [ "$CODE" = "200" ]; then
    pass "Discard command successful"
else
    fail "Discard command failed: $RESULT"
fi

# Test 7: Verify Reverted State
log "Verifying Reverted State..."
STAGED_AFTER=$(api_get "/config?source=staged")
if echo "$STAGED_AFTER" | grep -q "rule-staged"; then
    fail "Staged config still has rule after discard!"
else
    pass "Staged config reverted successfully"
fi

log "API Staging Tests PASSED"
exit 0
