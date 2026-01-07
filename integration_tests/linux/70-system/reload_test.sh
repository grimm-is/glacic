#!/bin/sh
# Scripts/test/reload_test.sh
# Verifies 'glacic reload' functionality

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
echo "1..4"

# Clean previous
rm -f $PID_FILE $LOG_FILE

# 1. Start daemon
CONFIG_FILE="/tmp/reload_config.hcl"
cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"
ip_forwarding = false
interface "lo" {
  description = "Loopback"
  ipv4 = ["127.0.0.1/8"]
  zone = "management"
}
EOF

echo "# Starting daemon..."
$GLACIC start --config "$CONFIG_FILE"
dilated_sleep 2

if [ -f "$PID_FILE" ]; then
    echo "ok 1 - Daemon started"
else
    echo "not ok 1 - Daemon failed to start"
    exit 1
fi

# 2. Modify config
echo "# Modifying config..."
# Modify description to see if it changes (would verify if we had a way to query running config easily via CLI json)
# But we can check logs for "Reloading configuration"
cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"
# Changed
ip_forwarding = true
interface "lo" {
  description = "Loopback Reloaded"
  ipv4 = ["127.0.0.1/8"]
  zone = "management"
}
EOF

# 3. Reload
echo "# Running reload command..."
# Must specify config file because reload defaults to /etc/glacic/glacic.hcl
# but we started daemon with a custom config
if $GLACIC reload "$CONFIG_FILE"; then
    echo "ok 2 - Reload command succeeded"
else
    echo "not ok 2 - Reload command failed"
    exit 1
fi

dilated_sleep 2

# 4. Check logs
if grep -q "Received SIGHUP, reloading configuration" "$LOG_FILE"; then
    echo "ok 3 - Daemon received SIGHUP and reloaded"
else
    echo "not ok 3 - Daemon did not log reload"
    tail -n 20 "$LOG_FILE" | sed 's/^/# /'
fi

# 5. Stop
$GLACIC stop
echo "ok 4 - Stopped"
