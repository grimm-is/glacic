#!/bin/sh
#
# OpenAPI Documentation Integration Test
# Verifies that the API serves OpenAPI spec and Swagger UI
#

TEST_TIMEOUT=30

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

log() { echo "[TEST] $1"; }

CONFIG_FILE="/tmp/api_openapi.hcl"

# Minimal config
cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"

api {
  enabled = false
  listen  = "127.0.0.1:8085"
  require_auth = false
}

interface "eth0" {
    ipv4 = ["192.168.1.1/24"]
}
EOF

# Start Control Plane
log "Starting Control Plane..."
$APP_BIN ctl "$CONFIG_FILE" > /tmp/ctl_openapi.log 2>&1 &
CTL_PID=$!
track_pid $CTL_PID

wait_for_file $CTL_SOCKET 5 || fail "Control plane socket not created"

# Start API Server
log "Starting API Server..."
export GLACIC_NO_SANDBOX=1
$APP_BIN test-api -listen :8085 > /tmp/api_openapi.log 2>&1 &
API_PID=$!
track_pid $API_PID

wait_for_port 8085 10 || {
    log "API log:"
    cat /tmp/api_openapi.log | head -20
    fail "API server failed to start"
}

plan 4

# Test 1: OpenAPI spec is served
log "Test 1: OpenAPI YAML endpoint"
response=$(curl -s http://127.0.0.1:8085/api/openapi.yaml 2>&1)

if echo "$response" | grep -q "openapi:"; then
    pass "OpenAPI YAML spec served at /api/openapi.yaml"
else
    log "Response: $response"
    fail "OpenAPI spec not found"
fi

# Test 2: OpenAPI spec contains required sections
log "Test 2: OpenAPI spec structure"
response=$(curl -s http://127.0.0.1:8085/api/openapi.yaml 2>&1)

has_info=$(echo "$response" | grep -c "info:" || echo "0")
has_paths=$(echo "$response" | grep -c "paths:" || echo "0")

if [ "$has_info" -gt 0 ] && [ "$has_paths" -gt 0 ]; then
    pass "OpenAPI spec has required sections (info, paths)"
else
    log "Has info: $has_info, Has paths: $has_paths"
    fail "OpenAPI spec missing required sections"
fi

# Test 3: Swagger UI endpoint serves HTML
log "Test 3: Swagger UI endpoint"
response=$(curl -s http://127.0.0.1:8085/api/docs 2>&1)

if echo "$response" | grep -q "swagger-ui"; then
    pass "Swagger UI served at /api/docs"
else
    log "Response: $(echo "$response" | head -5)"
    fail "Swagger UI not found"
fi

# Test 4: Key API paths documented
log "Test 4: API paths documented"
response=$(curl -s http://127.0.0.1:8085/api/openapi.yaml 2>&1)

has_status=$(echo "$response" | grep -c "/api/status" || echo "0")
has_config=$(echo "$response" | grep -c "/api/config" || echo "0")

if [ "$has_status" -gt 0 ] || [ "$has_config" -gt 0 ]; then
    pass "Key API endpoints documented in spec"
else
    log "Has /api/status: $has_status, Has /api/config: $has_config"
    fail "Missing key endpoint documentation"
fi

log "OpenAPI documentation tests completed"
rm -f "$CONFIG_FILE"
exit 0
