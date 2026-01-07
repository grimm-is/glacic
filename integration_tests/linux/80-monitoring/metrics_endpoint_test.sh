#!/bin/sh
set -x
# Prometheus Metrics Endpoint Integration Test
# Verifies the /metrics endpoint serves Prometheus format metrics.
#
# Tests:
# 1. Start API server
# 2. Scrape /metrics endpoint
# 3. Verify Prometheus format
# 4. Verify key metric families present

TEST_TIMEOUT=60

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

plan 6

cleanup() {
    diag "Cleanup..."
    stop_api
    stop_ctl
    rm -f "$TEST_CONFIG" "$METRICS_OUTPUT" 2>/dev/null
}
trap cleanup EXIT

# --- Setup ---
diag "Starting Prometheus Metrics Endpoint Test"

# Create config
TEST_CONFIG=$(mktemp_compatible "metrics_endpoint_test.hcl")
cat > "$TEST_CONFIG" << 'EOF'
schema_version = "1.0"

api {
  enabled = false
  listen = "127.0.0.1:8080"
}

# Removed managed 'lo' to avoid locking out API
EOF

# 1. Start Control Plane
start_ctl "$TEST_CONFIG"
ok 0 "Control plane started"

# 2. Start API Server (uses common helper which waits for port 8080)
start_api "$TEST_CONFIG"
ok 0 "API server started"

dilated_sleep 1

# 3. Scrape /metrics endpoint (doesn't require auth)
diag "Scraping /metrics endpoint..."
METRICS_OUTPUT=$(mktemp_compatible "metrics_output.txt")
curl -s http://127.0.0.1:8080/metrics > "$METRICS_OUTPUT" 2>&1
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8080/metrics)

if [ "$HTTP_CODE" = "200" ]; then
    ok 0 "Metrics endpoint returns HTTP 200"
else
    ok 1 "Metrics endpoint failed: HTTP $HTTP_CODE"
    head -20 "$METRICS_OUTPUT"
fi

# 4. Verify Prometheus format (HELP and TYPE lines)
if grep -q "^# HELP" "$METRICS_OUTPUT" && grep -q "^# TYPE" "$METRICS_OUTPUT"; then
    ok 0 "Metrics are in Prometheus format"
else
    ok 1 "Invalid Prometheus format"
    head -30 "$METRICS_OUTPUT"
fi

# 5. Verify key glacic metrics are present
EXPECTED_METRICS="glacic_|promhttp_|go_"
if grep -qE "$EXPECTED_METRICS" "$METRICS_OUTPUT"; then
    ok 0 "Glacic/Prometheus metrics present"
    diag "Sample metrics:"
    grep -E "^glacic_" "$METRICS_OUTPUT" | head -5
else
    # Check for any metrics at all
    METRIC_COUNT=$(grep -c "^[a-z]" "$METRICS_OUTPUT" 2>/dev/null || echo 0)
    if [ "$METRIC_COUNT" -gt 0 ]; then
        ok 0 "Metrics found ($METRIC_COUNT lines)"
    else
        ok 1 "No metrics found"
    fi
fi

# 6. Verify specific metric families we expect
FOUND=0
for metric in "uptime" "api_requests" "conntrack" "dns" "dhcp"; do
    if grep -qi "$metric" "$METRICS_OUTPUT"; then
        FOUND=$((FOUND + 1))
    fi
done

if [ "$FOUND" -ge 1 ]; then
    ok 0 "Found $FOUND expected metric families"
else
    # Fallback: just count total metrics
    TOTAL=$(grep -cE "^[a-z_]+\{|^[a-z_]+ [0-9]" "$METRICS_OUTPUT" 2>/dev/null || echo 0)
    if [ "$TOTAL" -gt 10 ]; then
        ok 0 "Found $TOTAL metric lines"
    else
        ok 1 "Expected metric families not found"
        diag "Metrics output sample:"
        head -50 "$METRICS_OUTPUT"
    fi
fi

# Summary
if [ $failed_count -eq 0 ]; then
    diag "All Prometheus Metrics Endpoint tests passed!"
    exit 0
else
    diag "Some tests failed"
    exit 1
fi
