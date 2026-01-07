#!/bin/sh
set -x
#
# OpenAPI Documentation Integration Test
# Verifies that the API serves OpenAPI spec and Swagger UI
#

TEST_TIMEOUT=60

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
EOF

# Start Control Plane
start_ctl "$CONFIG_FILE"

# Start API Server
export GLACIC_NO_SANDBOX=1
start_api -listen :8085

plan 4

# Test 1: OpenAPI spec is served
log "Test 1: OpenAPI YAML endpoint"
status=$(curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8085/api/openapi.yaml)
# Download to file to avoid shell variable limits
curl -s -o /tmp/openapi.yaml http://127.0.0.1:8085/api/openapi.yaml

if [ "$status" = "200" ]; then
    if grep -q "openapi:" /tmp/openapi.yaml; then
        pass "OpenAPI YAML spec served at /api/openapi.yaml (200 OK + Matches)"
    else
        log "Response start: $(head -n 5 /tmp/openapi.yaml)"
        fail "OpenAPI spec content mismatch"
    fi
else
    fail "OpenAPI spec endpoint returned $status (Expected 200)"
fi

# Test 2: OpenAPI spec contains required sections
log "Test 2: OpenAPI spec structure"
has_info=$(grep -c "info:" /tmp/openapi.yaml || echo "0")
has_paths=$(grep -c "paths:" /tmp/openapi.yaml || echo "0")

if [ "$has_info" -gt 0 ] && [ "$has_paths" -gt 0 ]; then
    pass "OpenAPI spec has required sections (info, paths)"
else
    log "Has info: $has_info, Has paths: $has_paths"
    fail "OpenAPI spec missing required sections"
fi

# Test 3: Swagger UI endpoint serves HTML
log "Test 3: Swagger UI endpoint"
status=$(curl -s -o /dev/null -w "%{http_code}" -L http://127.0.0.1:8085/api/docs)
response=$(curl -s -L http://127.0.0.1:8085/api/docs)

if [ "$status" = "200" ]; then
    if echo "$response" | grep -q "swagger-ui"; then
        pass "Swagger UI served at /api/docs (200 OK + Matches)"
    else
        log "Response: $(echo "$response" | head -5)"
        fail "Swagger UI content mismatch"
    fi
else
    fail "Swagger UI endpoint returned $status (Expected 200)"
fi

# Test 4: Key API paths documented
log "Test 4: API paths documented"
# Reuse file downloaded in Test 1
has_status=$(grep -c "/api/status" /tmp/openapi.yaml || echo "0")
has_config=$(grep -c "/api/config" /tmp/openapi.yaml || echo "0")

if [ "$has_status" -gt 0 ] || [ "$has_config" -gt 0 ]; then
    pass "Key API endpoints documented in spec"
else
    log "Has /api/status: $has_status, Has /api/config: $has_config"
    fail "Missing key endpoint documentation"
fi

log "OpenAPI documentation tests completed"
rm -f "$CONFIG_FILE"
exit 0
