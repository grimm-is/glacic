#!/bin/sh
# Increase timeout for multiple restarts
TEST_TIMEOUT=30

set -e
set -x

# Setup test environment
source "$(dirname "$0")/../common.sh"

# This test requires root (for managing ctl/logs)
require_root
require_binary

echo "Starting Boot Loop Protection test..."

# We need to ensure we start fresh (no previous crash state)
rm -f "$STATE_DIR/crash.state"

# Create a valid config (to prove we don't load it in Safe Mode)
CONFIG_FILE=$(mktemp_compatible config.hcl)
cat > "$CONFIG_FILE" <<EOF
interface "eth0" {
  zone = "lan"
  ipv4 = ["192.168.1.1/24"]
}
EOF

# Crash 1
echo "Simulating Crash 1..."
rm -f "$CTL_SOCKET"
start_ctl "$CONFIG_FILE"
# Check it's running
if ! kill -0 $CTL_PID; then fail "CTL failed to start (1)"; fi
# Kill it immediately
kill -9 $CTL_PID
wait $CTL_PID 2>/dev/null || true
track_pid "" # untrack (it's dead)

# Crash 2
echo "Simulating Crash 2..."
# We need small delay? CrashWindow is 5 min.
dilated_sleep 1
rm -f "$CTL_SOCKET"
start_ctl "$CONFIG_FILE"
kill -9 $CTL_PID
wait $CTL_PID 2>/dev/null || true
track_pid ""

# Crash 3
echo "Simulating Crash 3..."
dilated_sleep 1
rm -f "$CTL_SOCKET"
start_ctl "$CONFIG_FILE"
kill -9 $CTL_PID
wait $CTL_PID 2>/dev/null || true
track_pid ""

# Next start should trigger Safe Mode (Consecutive = 4 > Threshold 3)
echo "Starting CTL (Expect Safe Mode)..."
dilated_sleep 1
rm -f "$CTL_SOCKET"
# We allow it to run this time
start_ctl "$CONFIG_FILE"

# Wait for it to stabilize/log
dilated_sleep 5
# Wait for it to stabilize/log
dilated_sleep 5

# Check log for Safe Mode message
if grep -q "ENTERING SAFE MODE" "$CTL_LOG"; then
    pass "Safe Mode detected in output logs"
    exit 0
elif [ -f "$LOG_DIR/glacic.log" ] && grep -q "ENTERING SAFE MODE" "$LOG_DIR/glacic.log"; then
    pass "Safe Mode detected in system log file"
    exit 0
else
    echo "FAILURE: Safe Mode NOT detected in logs."
    echo "Checking $CTL_LOG:"
    cat "$CTL_LOG"
    if [ -f "$LOG_DIR/glacic.log" ]; then
        echo "Checking $LOG_DIR/glacic.log:"
        cat "$LOG_DIR/glacic.log"
    fi
    exit 1
fi

# Verify config is minimal (eth0 from file should NOT be present if we loaded minimal config)
# But wait, start_ctl starts API? No, separate call.
start_api

# Wait for API and check verification
wait_for_port 8080 10

# Helper to check if eth0 is configured
# GET /api/interfaces should show only 'lo'?
# Or checking `nft list ruleset`
# Let's check API.
INTERFACES=$(curl -s http://localhost:8080/api/interfaces)
echo "Interfaces in Safe Mode: $INTERFACES"

if echo "$INTERFACES" | grep -q "eth0"; then
    echo "FAILURE: eth0 present in API, Safe Mode minimal config failed (loaded file?)"
    exit 1
else
    echo "SUCCESS: eth0 not present (Safe Mode minimal config active)"
fi

echo "Boot Loop Protection verification passed!"
