#!/bin/sh
# Set timeout for this test - need time for daemon startup + API spawn
TEST_TIMEOUT=30
set -e
set -x

# Path to glacic binary
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
. "$SCRIPT_DIR/../common.sh"
GLACIC="$APP_BIN"
PID_FILE="/var/run/glacic.pid"
LOG_FILE="/var/log/glacic/glacic.log"

echo "TAP version 13"
echo "1..7"

# Clean previous run
rm -f $PID_FILE

export GLACIC_LOG_FILE=stdout

# Generate minimal valid config
CONFIG_FILE="/tmp/test_config.hcl"
cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"
ip_forwarding = false

interface "lo" {
  description = "Loopback"
  ipv4 = ["127.0.0.1/8"]
  zone = "management"
}

api {
  enabled = true
  listen = ":8080"
  disable_sandbox = true
}
EOF

# 1. Start daemon
echo "# Running start command..."
# Use generated configuration
if $GLACIC start --config "$CONFIG_FILE"; then
    echo "ok 1 - glacic start command succeeded"
else
    echo "not ok 1 - glacic start command failed"
    exit 1
fi

# Poll for PID file instead of fixed dilated_sleep (max 5s, 100ms intervals)
WAITED=0
while [ ! -f "$PID_FILE" ] && [ $WAITED -lt 50 ]; do
    dilated_sleep 0.1
    WAITED=$((WAITED + 1))
done

# 2. Check PID file
if [ -f "$PID_FILE" ]; then
    echo "ok 2 - PID file exists at $PID_FILE (waited ${WAITED}00ms)"
else
    echo "not ok 2 - PID file missing after 5s"
    if [ -f "$LOG_FILE" ]; then
        echo "# Log content:"
        cat "$LOG_FILE" | sed 's/^/# /'
    else
        echo "# Log file not found at $LOG_FILE"
        find /var/log -name "glacic.log" | sed 's/^/# /'
    fi
    exit 1
fi

# 3. Check process
PID=$(cat "$PID_FILE")
if [ -d "/proc/$PID" ]; then
    echo "ok 3 - Daemon process is running (PID $PID)"
else
    echo "not ok 3 - Daemon process not running"
    exit 1
fi

# 4. Check API started (poll for log entry, max 10s)
if [ ! -f "$LOG_FILE" ]; then
    LOG_FILE=$(find /var/log -name "glacic.log" | head -1)
fi

WAITED=0
while [ $WAITED -lt 100 ]; do
    if [ -f "$LOG_FILE" ] && grep -q "Spawning API server" "$LOG_FILE"; then
        echo "# API server spawning logged (waited ${WAITED}00ms)"
        break
    fi
    dilated_sleep 0.1
    WAITED=$((WAITED + 1))
done

if [ $WAITED -ge 100 ]; then
    echo "not ok 4 - API spawning not found in logs after 10s ($LOG_FILE)"
    echo "# Log tail:"
    tail -n 10 "$LOG_FILE" 2>/dev/null | sed 's/^/# /' || echo "# No log file"
fi

# Wait for API server to actually bind the port (child process startup)
WAITED=0
while [ $WAITED -lt 50 ]; do
    if grep -q "Starting API server on" "$LOG_FILE"; then
        # Check if port is actually bound (log is printed before Bind)
        if netstat -lnt | grep -q ":8080"; then
            echo "ok 4 - API server bound port 8080"
            break
        fi
    fi
    dilated_sleep 0.1
    WAITED=$((WAITED + 1))
done

# 4.5 Verify 'glacic test-api' detects running instance
echo "# Testing 'glacic test-api' status message..."
API_OUTPUT=$(timeout 5 $GLACIC test-api 2>&1 || true)
# Check for various messages indicating API is already running
if echo "$API_OUTPUT" | grep -q "Glacic API is already running\|Waiting for previous API instance\|API server is already running\|address already in use"; then
    echo "ok 5 - glacic test-api detects running instance"
else
    echo "not ok 5 - glacic test-api failed to detect running instance"
    echo "# Output:"
    echo "$API_OUTPUT" | sed 's/^/# /'
    echo "# Daemon Log Content:"
    cat "$LOG_FILE" | sed 's/^/# /' || echo "# Failed to read log file"
    # Don't fail hard, this is a UX improvement
fi

# 5. Stop daemon
echo "# Running stop command..."
if $GLACIC stop; then
    echo "ok 6 - glacic stop command succeeded"
else
    echo "not ok 6 - glacic stop command failed"
    exit 1
fi

# Poll for PID file removal instead of fixed dilated_sleep (max 5s, 100ms intervals)
WAITED=0
while [ -f "$PID_FILE" ] && [ $WAITED -lt 50 ]; do
    dilated_sleep 0.1
    WAITED=$((WAITED + 1))
done

# 6. Check PID file removed
if [ ! -f "$PID_FILE" ]; then
    echo "ok 7 - PID file removed (waited ${WAITED}00ms)"
else
    echo "not ok 7 - PID file still exists after 5s"
    exit 1
fi
