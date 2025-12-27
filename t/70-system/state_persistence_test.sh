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

# 1. Start Control Plane
diag "Starting control plane (Run 1)..."
start_ctl "$TEST_CONFIG"
# state_persistence uses Daemon API, removing manual start to avoid conflict
# start_api

ok 0 "Control plane and API started"

sleep 5

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

# Wait for IP
sleep 5
CLIENT_IP=$(ip netns exec client ip addr show veth-client | grep "inet " | awk '{print $2}' | cut -d/ -f1)

if [ -n "$CLIENT_IP" ]; then
    diag "Client got IP: $CLIENT_IP"
    ok 0 "Client received DHCP lease"
else
    fail "Client failed to get IP"
fi

# 3. Verify Lease in API (Before Restart)
LEASES_JSON=$(curl -k -v https://169.254.255.2:8080/api/leases 2>&1)
# Verify our IP is in there
if echo "$LEASES_JSON" | grep -q "$CLIENT_IP"; then
    pass "Lease verified via API (Pre-Restart)"
else
    diag "API Response: $LEASES_JSON"
    echo "--- API LOG ---"
    if [ -f "$API_LOG" ]; then cat "$API_LOG"; fi
    echo "--- CTL LOG ---"
    if [ -f "$CTL_LOG" ]; then cat "$CTL_LOG"; fi
    fail "FATAL: Lease not found in API"
fi

# 4. Restart Control Plane
diag "Restarting control plane..."
stop_ctl
# API stops when CTL stops usually via context or failure, but test-api might linger?
# test-api might fail health checks. Ideally we restart that too or just rely on retries.
# Assuming API proxy tracks CTL status or just fails.
# We'll restart everything to be safe.
kill $API_PID 2>/dev/null
wait $API_PID 2>/dev/null

sleep 2

start_ctl "$TEST_CONFIG"
# state_persistence uses Daemon API, removing manual start to avoid conflict
# start_api


sleep 5

# 5. Verify Lease in API (After Restart)
LEASES_JSON_2=$(curl -k -s https://169.254.255.2:8080/api/leases)
if echo "$LEASES_JSON_2" | grep -q "$CLIENT_IP"; then
    pass "Lease persisted via API (Post-Restart)"
else
    diag "API Response: "$LEASES_JSON_2
    fail "Lease lost after restart"
fi
