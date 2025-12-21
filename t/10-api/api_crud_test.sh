#!/bin/sh

# API CRUD Integration Test
# Verifies: CRUD operations for Interfaces, DHCP, and Policies
# This authenticates via a read-write API key.

TEST_TIMEOUT=30

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

log() { echo "[TEST] $1"; }

CONFIG_FILE="/tmp/api_crud.hcl"
KEY_STORE="/tmp/apikeys_crud.json"

# configuration
cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"

api {
  enabled = false
  listen  = "127.0.0.1:8081"
  require_auth = true
  key_store_path = "$KEY_STORE"

  key "admin-key" {
    key = "gfw_admin123"
    permissions = ["config:write", "config:read", "firewall:write", "dhcp:write", "dns:write"]
  }
}

interface "eth0" {
    ipv4 = ["10.0.0.1/24"]
}
EOF

# Start Control Plane
log "Starting Control Plane..."
$APP_BIN ctl "$CONFIG_FILE" > /tmp/ctl_crud.log 2>&1 &
CTL_PID=$!
track_pid $CTL_PID

# Wait for socket
wait_for_file $CTL_SOCKET 5 || fail "Control plane socket not created"

# Start API Server (disable sandbox)
log "Starting API Server..."
export GLACIC_NO_SANDBOX=1
$APP_BIN test-api -listen :8081 > /tmp/api_crud.log 2>&1 &
API_PID=$!
track_pid $API_PID

wait_for_port 8081 10 || fail "API server failed to start"

API_URL="http://127.0.0.1:8081/api"
AUTH_HEADER="X-API-Key: gfw_admin123"

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
        # minimal wget fallback if needed, but we assume curl for complex JSON testing usually
        fail "curl is required for CRUD tests"
    fi
}

# Test 1: Update Interface (enable DHCP)
log "Testing UpdateInterface (enable DHCP on eth0)..."
DATA='{
  "name": "eth0",
  "action": "update",
  "dhcp": true
}'

RESULT=$(api_post "/interfaces/update" "$DATA")
CODE=$(echo "$RESULT" | awk '{print $1}')
if [ "$CODE" = "200" ]; then
    pass "UpdateInterface returned 200"
else
    fail "UpdateInterface failed: $RESULT"
fi

# Test 2: Update DHCP Config
log "Testing UpdateDHCP..."
DATA='{
  "enabled": true,
  "scopes": [
    {
      "name": "lan",
      "interface": "eth0",
      "range_start": "10.0.0.100",
      "range_end": "10.0.0.200",
      "router": "10.0.0.1"
    }
  ]
}'
RESULT=$(api_post "/config/dhcp" "$DATA")
CODE=$(echo "$RESULT" | awk '{print $1}')
if [ "$CODE" = "200" ]; then
    pass "UpdateDHCP returned 200"
else
    fail "UpdateDHCP failed: $RESULT"
fi

# Test 3: Update Policies
log "Testing UpdatePolicies..."
DATA='[
  {
    "name": "lan_to_wan",
    "from_zone": "lan",
    "to_zone": "wan",
    "action": "accept"
  }
]'
RESULT=$(api_post "/config/policies" "$DATA")
CODE=$(echo "$RESULT" | awk '{print $1}')
if [ "$CODE" = "200" ]; then
    pass "UpdatePolicies returned 200"
else
    fail "UpdatePolicies failed: $RESULT"
fi

# Step 3.5: Apply Changes (Push Staged Config to Control Plane)
log "Applying Configuration..."
# Apply uses the current state of s.Config in the API server
RESULT=$(api_post "/config/apply" "{}")
CODE=$(echo "$RESULT" | awk '{print $1}')
if [ "$CODE" = "200" ]; then
    pass "ApplyConfig returned 200"
else
    fail "ApplyConfig failed: $RESULT"
fi

# Test 4: Verify Policy persistence via GET
log "Verifying Policies..."
if command -v curl >/dev/null 2>&1; then
    OUT=$(curl -s -H "$AUTH_HEADER" "$API_URL/config/policies")
    if echo "$OUT" | grep -q "lan_to_wan"; then
        pass "Policy 'lan_to_wan' found in GET response"
    else
        fail "Policy verification failed: $OUT"
    fi
else
    fail "curl required"
fi

log "API CRUD Tests PASSED"
exit 0
