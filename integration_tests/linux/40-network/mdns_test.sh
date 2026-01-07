#!/bin/sh
set -x
# mDNS Reflector Integration Test
# Verifies that mDNS packets are forwarded between interfaces.

TEST_TIMEOUT=30

# Source common functions
. "$(dirname "$0")/../common.sh"

require_root
require_binary

# Define helper to keep command short
# We use the main glacic binary with the hidden 'mcast' command we added
MDNS_TOOL="$APP_BIN mcast"

# --- Setup ---
plan 6

diag "Starting mDNS Reflector Test using $MDNS_TOOL"

cleanup() {
    diag "Cleanup..."
    teardown_test_topology
    rm -f "$TEST_CONFIG" 2>/dev/null
    stop_ctl
}
trap cleanup EXIT

# 1. Create config
TEST_CONFIG=$(mktemp_compatible "mdns_test.hcl")
cat > "$TEST_CONFIG" << 'EOF'
schema_version = "1.0"
ip_forwarding = true

interface "veth-lan" {
  zone = "lan"
  ipv4 = ["192.168.100.1/24"]
}

interface "veth-wan" {
  zone = "wan"
  ipv4 = ["10.99.99.1/24"]
}

zone "lan" {
  interfaces = ["veth-lan"]
}

zone "wan" {
  interfaces = ["veth-wan"]
}

# Enable mDNS reflector between LAN and WAN
mdns {
  enabled = true
  interfaces = ["veth-lan", "veth-wan"]
}
EOF

# 2. Setup Topology
setup_test_topology
ok 0 "Test topology created"

# Add multicast routes for mcast tool
# This is required even for the Go tool to find the route for 224.0.0.0/4
ip netns exec test_client ip route add 224.0.0.0/4 dev veth-client
ip netns exec test_server ip route add 224.0.0.0/4 dev veth-server

# 3. Start Control Plane
export GLACIC_LOG_FILE=stdout
start_ctl "$TEST_CONFIG"
ok 0 "Firewall started"

# Poll for reflector startup instead of dilated_sleep 5
count=0
while ! grep -q "Starting reflector" "$CTL_LOG" 2>/dev/null; do
    dilated_sleep 0.2
    count=$((count + 1))
    if [ $count -ge 25 ]; then  # 25 * 0.2s = 5s max
        diag "Timeout waiting for reflector startup"
        break
    fi
done
diag "Reflector check after $((count * 200))ms"

# 4. Verify mDNS sockets are bound
if grep -q "Starting reflector" "$CTL_LOG"; then
    ok 0 "Reflector service started"
else
    ok 1 "Reflector service failed to start"
    cat "$CTL_LOG"
fi

# 5. IPv4 Reflector Test
# Receiver on WAN side (test_server)
RCV_FILE=$(mktemp_compatible "mdns_rcv.log")

diag "Starting mDNS listener on WAN side (test_server)..."
# mcast tool listen on veth-server (inside ns)
# Using the main glacic binary which we proven works in netns
ip netns exec test_server $MDNS_TOOL -mode listen -iface veth-server -timeout 5s > "$RCV_FILE" 2>&1 &
RCV_PID=$!

dilated_sleep 2

diag "Sending mDNS packet from LAN side (test_client)..."
PAYLOAD="GLACIC_MDNS_TEST_PAYLOAD_$(date +%s)"
ip netns exec test_client $MDNS_TOOL -mode send -iface veth-client -msg "$PAYLOAD"

dilated_sleep 2

# Check reception
if grep -q "$PAYLOAD" "$RCV_FILE"; then
    ok 0 "mDNS packet forwarded LAN -> WAN"
else
    ok 1 "mDNS packet NOT received on WAN"
    diag "Receiver Log:"
    cat "$RCV_FILE"
    diag "Ctl Log:"
    cat "$CTL_LOG"
fi

# 6. Check Loop Prevention (Reverse check)
RCV_FILE_LAN=$(mktemp_compatible "mdns_rcv_lan.log")
diag "Starting mDNS listener on LAN side (test_client)..."
ip netns exec test_client $MDNS_TOOL -mode listen -iface veth-client -timeout 5s > "$RCV_FILE_LAN" 2>&1 &
RCV_PID_LAN=$!

dilated_sleep 2

diag "Sending mDNS packet from WAN side (test_server)..."
PAYLOAD_REV="GLACIC_MDNS_REVERSE_$(date +%s)"
ip netns exec test_server $MDNS_TOOL -mode send -iface veth-server -msg "$PAYLOAD_REV"

dilated_sleep 2

if grep -q "$PAYLOAD_REV" "$RCV_FILE_LAN"; then
    ok 0 "mDNS packet forwarded WAN -> LAN"
else
    ok 1 "mDNS packet NOT received on LAN"
fi

if [ $failed_count -eq 0 ]; then
    exit 0
else
    exit 1
fi
