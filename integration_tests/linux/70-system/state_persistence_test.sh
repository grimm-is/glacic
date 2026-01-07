#!/bin/sh
set -x
#
# State Persistence Test (Hardened)
# Verifies: DHCP leases survive control plane restart using real client interaction and API verification.
#
# Topology:
#   [ ns:client ] <--(veth)--> [ HOST (Glacic) ]
#

# Source common functions
. "$(dirname "$0")/../common.sh"

require_root
require_binary

# Check for DHCP client
DHCP_CLIENT=""
if command -v udhcpc >/dev/null 2>&1; then
    DHCP_CLIENT="udhcpc"
elif command -v dhclient >/dev/null 2>&1; then
    DHCP_CLIENT="dhclient"
else
    echo "1..0 # SKIP No DHCP client (udhcpc or dhclient) found"
    exit 0
fi

cleanup_persist() {
    ip netns del client 2>/dev/null || true
    ip link del veth-lan 2>/dev/null || true
    rm -f "$TEST_CONFIG"
    stop_ctl
}

trap cleanup_persist EXIT INT TERM

plan 4

diag "Setting up test topology..."
ip netns add client
ip link add veth-lan type veth peer name veth-client
ip link set veth-client netns client
ip link set veth-lan up
ip addr add 10.99.99.1/24 dev veth-lan

# Configure Glacic
TEST_CONFIG=$(mktemp_compatible state_persist.hcl)
cat > "$TEST_CONFIG" <<EOF
schema_version = "1.0"

interface "veth-lan" {
  zone = "lan"
  ipv4 = ["10.99.99.1/24"]
}

zone "lan" {
  interfaces = ["veth-lan"]
}

api {
  enabled = true
  listen  = "0.0.0.0:8080"
  require_auth = false
}

dhcp {
  enabled = true
  scope "test" {
    interface = "veth-lan"
    range_start = "10.99.99.100"
    range_end = "10.99.99.200"
    router = "10.99.99.1"
  }
}
EOF

# Helper: Wait for log
wait_for_log() {
    pattern="$1"
    logfile="$2"
    timeout=${3:-30}
    diag "Waiting for '$pattern' in $(basename "$logfile")..."
    count=0
    while ! grep -q "$pattern" "$logfile" 2>/dev/null; do
        sleep 1
        count=$((count+1))
        if [ $count -ge $timeout ]; then
             fail "Timeout waiting for '$pattern' in $logfile"
        fi
    done
}

# 1. Start Control Plane
diag "Starting control plane (Run 1)..."
start_ctl "$TEST_CONFIG"

wait_for_log "Control plane listening" "$CTL_LOG"

# Start API server explicitly
export GLACIC_NO_SANDBOX=1
start_api -listen :8080

ok 0 "Control plane and API started"

# 2. Request Lease via Client
diag "Requesting DHCP lease..."
ip netns exec client ip link set lo up
ip netns exec client ip link set veth-client up

# Start DHCP client
if [ "$DHCP_CLIENT" = "udhcpc" ]; then
    ip netns exec client udhcpc -i veth-client -n -q -f &
    CLIENT_PID=$!
else
    ip netns exec client dhclient -v veth-client &
    CLIENT_PID=$!
fi

# Wait for IP (Poll)
diag "Waiting for IP assignment..."
count=0
while [ $count -lt 30 ]; do
    CLIENT_IP=$(ip netns exec client ip addr show veth-client | grep "inet " | awk '{print $2}' | cut -d/ -f1)
    [ -n "$CLIENT_IP" ] && break
    sleep 1
    count=$((count+1))
done

if [ -n "$CLIENT_IP" ]; then
    diag "Client got IP: $CLIENT_IP"
    ok 0 "Client received DHCP lease"
else
    fail "Client failed to get IP"
fi

# 3. Verify Lease in API (Before Restart)
dilated_sleep 1
LEASES_JSON_1=$(curl -s http://127.0.0.1:8080/api/leases)
if echo "$LEASES_JSON_1" | grep -q "$CLIENT_IP"; then
    ok 0 "Lease visible via API (Pre-Restart)"
else
    diag "API Response (Pre): $LEASES_JSON_1"
    fail "Lease not found in API"
fi

# 4. Restart Control Plane
diag "Restarting control plane..."
stop_ctl
cleanup_processes

# Clean logs for fresh grep
echo "" > "$CTL_LOG"

start_ctl "$TEST_CONFIG"

wait_for_log "Control plane listening" "$CTL_LOG"

# Restart API server
export GLACIC_NO_SANDBOX=1
start_api -listen :8080

# 5. Verify Lease in API (After Restart)
LEASES_JSON_2=$(curl -s http://127.0.0.1:8080/api/leases)
if echo "$LEASES_JSON_2" | grep -q "$CLIENT_IP"; then
    pass "Lease persisted via API (Post-Restart)"
else
    diag "API Response: $LEASES_JSON_2"
    fail "Lease lost after restart"
fi

