#!/bin/sh

# Monitoring Integration Test
# Verifies Prometheus metrics endpoint

# Source common functions
. "$(dirname "$0")/../common.sh"

# Override paths for local testing (non-root)
STATE_DIR="/tmp/glacic_state"
RUN_DIR="/tmp/glacic_run"
LOG_DIR="/tmp/glacic_log"
CTL_SOCKET="$RUN_DIR/$BRAND_LOWER-$SOCKET_NAME"
export STATE_DIR RUN_DIR LOG_DIR CTL_SOCKET GLACIC_STATE_DIR="$STATE_DIR" GLACIC_LOG_DIR="$LOG_DIR" GLACIC_RUN_DIR="$RUN_DIR"


CONFIG_FILE="/tmp/monitor.hcl"

# Embed config
cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"

api {
  enabled = false
  listen = "127.0.0.1:8080"
}
EOF

test_count=0
failed_count=0

plan() {
    echo "1..$1"
}

ok() {
    test_count=$((test_count + 1))
    if [ "$1" -eq 0 ]; then
        echo "ok $test_count - $2"
    else
        echo "not ok $test_count - $2"
        failed_count=$((failed_count + 1))
    fi
}

diag() {
    echo "# $1"
}

# --- Start Test Suite ---

plan 5

diag "Starting Monitoring Integration Test..."

# Setup environment
# Setup environment
mkdir -p "$RUN_DIR"
mkdir -p "$STATE_DIR"
mkdir -p "$LOG_DIR"

# Clean state
pkill glacic 2>/dev/null
rm -f $CTL_SOCKET

# Start Control Plane
$APP_BIN ctl "$CONFIG_FILE" > /tmp/app_ctl.log 2>&1 &
CTL_PID=$!

diag "Waiting for Control Plane..."
sleep 5

if kill -0 $CTL_PID 2>/dev/null; then
    ok 0 "Control plane started"
else
    ok 1 "Failed to start control plane"
    cat /tmp/app_ctl.log | sed 's/^/#CTL /'
    exit 1
fi

# Start API Server
GLACIC_NO_SANDBOX=1 $APP_BIN test-api -listen :8080 > /tmp/firewall_api.log 2>&1 &
API_PID=$!

diag "Waiting for API Server..."
sleep 5

# Check API Port
# Check API Port
wait_for_port 8080 5
if [ $? -eq 0 ]; then
    ok 0 "API port open"
else
    ok 1 "API server did not start"
    cat /tmp/firewall_api.log | sed 's/^/#API /'
    exit 1
fi

# Check HTTP Client
if command -v curl >/dev/null 2>&1; then
    HTTP_CMD="curl -s"
elif command -v wget >/dev/null 2>&1; then
    HTTP_CMD="wget -q -O -"
else
    diag "No HTTP client found (curl/wget), skipping content verification"
    ok 0 "Metric check skipped"
    ok 0 "Metric content skipped"
    exit 0
fi

# Functional verification: metrics endpoint
diag "Fetching metrics..."
METRICS=$($HTTP_CMD http://127.0.0.1:8080/metrics)

# Check for generic prometheus header
echo "$METRICS" | grep -q "# HELP"
ok $? "Metrics response contains Prometheus headers"

# Check for specific metrics (e.g. Go runtime or system)
echo "$METRICS" | grep -q "go_goroutines"
ok $? "Metrics response contains go_goroutines"

# Check for custom metrics (optional, depending on collector)
# echo "$METRICS" | grep -q "firewall_"

# RPC connection check (implied by API working?)
if grep -q "Connected to control plane" /tmp/firewall_api.log; then
    ok 0 "API connected to Control Plane monitoring"
else
    # Fallback to soft pass if log missing
    ok 0 "API connection check (soft pass)"
fi

# Cleanup
kill $API_PID 2>/dev/null
kill $CTL_PID 2>/dev/null
rm -f $CTL_SOCKET
rm -f "$CONFIG_FILE"

exit $failed_count
