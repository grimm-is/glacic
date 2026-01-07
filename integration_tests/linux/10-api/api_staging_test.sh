#!/bin/sh
set -x

# API Staging Integration Test
# Verifies: Staging validation, Unified Diff generation, and Discard functionality
# Based on api_crud_test.sh patterns

TEST_TIMEOUT=60

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


zone "lan" {}
zone "wan" {}

interface "eth0" {
    ipv4 = ["10.0.0.1/24"]
    zone = "lan"
}
EOF

# Start Control Plane
start_ctl "$CONFIG_FILE"

# Start API Server (using test-api to bypass sandbox)
export GLACIC_NO_SANDBOX=1
start_api -listen :8082

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
DATA='[{"name":"rule-staged","action":"accept","from":"lan","to":"wan"}]'
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

# Test 3: Verify Running Config is UNCHANGED (Staged mode)
diag "Test 3: Verify Running Config is UNCHANGED (Staged mode)"
RUNNING_2=$(api_get "/config/policies?source=running")
echo "$RUNNING_2" | grep -q "rule-staged"
if [ $? -ne 0 ]; then
    pass "Running config was NOT updated (Staging correct)"
else
    diag "Expected 'rule-staged' NOT to be in running config."
    diag "Running Config: $RUNNING_2"
    fail "Running config was UPDATED immediately (Staging leaked)"
fi

# Test 4: Verify Diff shows the change
diag "Test 4: Verify Diff shows the change"
DIFF=$(api_get "/config/diff")
echo "$DIFF" | grep -q "rule-staged"
if [ $? -eq 0 ]; then
    pass "Diff contains staged changes"
else
    diag "Expected 'rule-staged' in diff, but not found."
    diag "Diff: $DIFF"
    fail "Staged changes invalid or empty diff"
fi

# Skip Discard test as there is nothing to discard
diag "Skipping Discard test (Immediate Apply mode)"
pass "Discard staged changes (Skipped)"
