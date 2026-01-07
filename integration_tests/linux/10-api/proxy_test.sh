#!/bin/sh

# API Proxy Test
# Verifies: Unix Socket + Userspace Proxy architecture
# 1. API creates Unix Socket.
# 2. Control Plane spawns Proxy.
# 3. Proxy listens on TCP and forwards to Socket.

set -e
# set -x # Enable for debug
source "$(dirname "$0")/../common.sh"

TEST_TIMEOUT=30

require_root
require_binary
cleanup_on_exit

diag "Test: API Unix Socket Proxy"

# Create config
CTL_CONFIG=$(mktemp_compatible "ctl_test.hcl")
cat > "$CTL_CONFIG" << 'EOF'
schema_version = "1.1"
api {
  enabled = true
  require_auth = false
  listen = ":8080"
}
EOF

# Start Control Plane (Daemon)
# This spawns _api-server and _proxy
export GLACIC_SKIP_API=0  # We NEED the proxy for this test
start_ctl "$CTL_CONFIG"
diag "Control plane started (PID $CTL_PID)"

# Wait for sockets (Manual loop to capture logs on failure)
diag "Waiting for API socket..."
for i in $(seq 1 15); do
    if [ -S "/var/run/glacic/api/api.sock" ]; then
        break
    fi
    sleep 1
done

if [ ! -S "/var/run/glacic/api/api.sock" ]; then
    diag "TIMEOUT waiting for API socket"
    diag "Process List:"
    ps aux
    diag "Network Sockets:"
    netstat -tlpn
    diag "CTL Log Content:"
    cat "$CTL_LOG"
    fail "API socket never appeared"
fi

diag "Waiting for Proxy port 8080..."
wait_for_port 8080

# Verify Socket
if [ -S "/var/run/glacic/api/api.sock" ]; then
    pass "Unix socket /var/run/glacic/api/api.sock created"
else
    fail "Unix socket missing"
fi

# Verify Connectivity via Proxy
diag "Testing HTTP connectivity via Proxy (localhost:8080)..."
if curl -s --connect-timeout 5 http://127.0.0.1:8080/api/status > /dev/null; then
    pass "Can connect via Proxy"
else
    fail "Proxy connectivity failed (curl)"
fi

# Check logs for confirmation of architecture
if grep -q "Starting API server on /var/run/glacic/api/api.sock (unix, HTTP)" "$CTL_LOG"; then
    pass "API log confirms Unix listener"
else
    diag "CTL Log Content:"
    cat "$CTL_LOG"
    fail "API log does not confirm Unix listener"
fi

# Explicitly verify process ownership of port 8080
diag "Verifying process ownership of port 8080..."
NETSTAT_OUT=$(netstat -tlpn | grep ":8080")
diag "Netstat for 8080: $NETSTAT_OUT"

if echo "$NETSTAT_OUT" | grep -q "_proxy"; then
    pass "Port 8080 is held by _proxy"
elif echo "$NETSTAT_OUT" | grep -q "_api-server"; then
    fail "Port 8080 is held by _api-server (Arch violation!)"
else
    # Fallback to checking PID if name truncated
    PROXY_PID=$(pgrep -f "_proxy" | head -n1)
    if echo "$NETSTAT_OUT" | grep -q "$PROXY_PID"; then
        pass "Port 8080 is held by _proxy (PID match)"
    else
        fail "Port 8080 held by unknown process"
    fi
fi
