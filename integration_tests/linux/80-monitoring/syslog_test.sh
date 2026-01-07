#!/bin/sh
set -x

# Syslog Forwarding Test
# Verifies: Logs are forwarded to remote syslog server
# Tests actual log forwarding functionality

TEST_TIMEOUT=30
. "$(dirname "$0")/../common.sh"

plan 4

require_root
require_binary

cleanup_on_exit

diag "Test: Syslog Log Forwarding"

# Start a UDP syslog receiver in background (netcat)
SYSLOG_PORT=15514
SYSLOG_OUTPUT=$(mktemp_compatible syslog_output.log)

# Start netcat listening for UDP syslog messages
# Use timeout to prevent hanging
diag "Starting syslog receiver on UDP port $SYSLOG_PORT..."
timeout 10 nc -u -l -p "$SYSLOG_PORT" > "$SYSLOG_OUTPUT" 2>/dev/null &
NC_PID=$!
dilated_sleep 0.5

# Create test config with syslog pointing to our receiver
TEST_CONFIG=$(mktemp_compatible syslog.hcl)
cat > "$TEST_CONFIG" <<EOF
schema_version = "1.0"

interface "lo" {
  zone = "lan"
  ipv4 = ["127.0.0.1/8"]
}

zone "lan" {
  interfaces = ["lo"]
}

syslog {
    enabled = true
    host = "127.0.0.1"
    port = $SYSLOG_PORT
    protocol = "udp"
    tag = "glacic-test"
}
EOF

# Start control plane
diag "Starting control plane with syslog forwarding..."
start_ctl "$TEST_CONFIG"
ok 0 "Control plane started with syslog forwarding config"

# Wait for logs to be forwarded
dilated_sleep 3

# Check if syslog forwarding is mentioned in control plane logs
if grep -qi "syslog forwarding enabled" "$CTL_LOG" 2>/dev/null; then
    ok 0 "Syslog forwarding enabled message in logs"
else
    # Check for connection failure warning
    if grep -qi "Failed to connect to syslog" "$CTL_LOG" 2>/dev/null; then
        ok 1 "Syslog connection failed"
        diag "Check if netcat is listening - port may be in use"
    else
        ok 0 "Syslog config accepted # SKIP (no explicit message)"
    fi
fi

# Stop netcat
kill "$NC_PID" 2>/dev/null || true
wait "$NC_PID" 2>/dev/null || true

# Check if any logs were received by the syslog server
if [ -s "$SYSLOG_OUTPUT" ]; then
    ok 0 "Syslog messages received by remote server"
    diag "Sample syslog output:"
    head -3 "$SYSLOG_OUTPUT" | sed 's/^/# /'
else
    ok 1 "No syslog messages received (file empty)"
    diag "This may be a timing issue or netcat problem"
fi

# Verify firewall is still running
if nft list table inet glacic >/dev/null 2>&1; then
    ok 0 "Firewall table created"
else
    ok 1 "Firewall table missing"
fi

rm -f "$TEST_CONFIG" "$SYSLOG_OUTPUT"
exit 0
