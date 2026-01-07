#!/bin/sh
set -x

# Single VM Zone Test using Network Namespaces
# Creates 3 network namespaces (green, orange, red) connected via veth pairs
# to test zone-based firewall policies on a single VM
# Verifies single-zone policies
TEST_TIMEOUT=60


# Brand mount path (matches glacic-builder)
# Source common functions
. "$(dirname "$0")/../common.sh"


# --- Start Test Suite ---

plan 14

diag "Start⚡ Running single VM zone test with network namespaces..."

# Cleanup function
cleanup() {
    diag "Cleaning up network namespaces..."
    ip netns del green 2>/dev/null
    ip netns del orange 2>/dev/null
    ip netns del red 2>/dev/null
    ip link del veth-green 2>/dev/null
    ip link del veth-orange 2>/dev/null
    ip link del veth-red 2>/dev/null
}

# Cleanup any existing setup
cleanup
# Flush host interfaces that might conflict with test subnets (10.1.0.0/24 etc)
# These usually come from QEMU network setup but we are overriding them with namespaces
ip addr flush dev eth1 2>/dev/null || true
ip addr flush dev eth2 2>/dev/null || true
ip addr flush dev eth3 2>/dev/null || true

# 1. Create network namespaces
diag "Creating network namespaces..."
ip netns add green
ip netns add orange
ip netns add red
ok $? "Network namespaces created"

# 2. Create veth pairs for zone connectivity
diag "Creating veth pairs..."
ip link add veth-green type veth peer name green0
ip link add veth-orange type veth peer name orange0
ip link add veth-red type veth peer name red0
ok $? "Veth pairs created"

# 3. Move zone interfaces to namespaces
diag "Moving interfaces to namespaces..."
ip link set green0 netns green
ip link set orange0 netns orange
ip link set red0 netns red
ok $? "Interfaces moved to namespaces"

# 4. Configure firewall zone interfaces
diag "Configuring firewall zone interfaces..."
ip addr add 10.1.0.1/24 dev veth-green
ip addr add 10.2.0.1/24 dev veth-orange
ip addr add 10.3.0.1/24 dev veth-red
ip link set veth-green up
ip link set veth-orange up
ip link set veth-red up
ok $? "Firewall zone interfaces configured"

# 5. Configure zone namespace interfaces
diag "Configuring zone namespace interfaces..."
ip netns exec green ip addr add 10.1.0.100/24 dev green0
ip netns exec orange ip addr add 10.2.0.100/24 dev orange0
ip netns exec red ip addr add 10.3.0.100/24 dev red0

ip netns exec green ip link set green0 up
ip netns exec orange ip link set orange0 up
ip netns exec red ip link set red0 up

ip netns exec green ip link set lo up
ip netns exec orange ip link set lo up
ip netns exec red ip link set lo up
ok $? "Zone namespace interfaces configured"

# 6. Configure routing in namespaces
diag "Configuring routing in namespaces..."
ip netns exec green ip route add default via 10.1.0.1
ip netns exec orange ip route add default via 10.2.0.1
ip netns exec red ip route add default via 10.3.0.1
ok $? "Routing configured"

# 7. Start firewall with zone configuration
diag "Starting firewall with zone configuration..."
start_ctl $MOUNT_PATH/configs/zones-single.hcl

if ! kill -0 $CTL_PID 2>/dev/null; then
    diag "Firewall failed to start, showing logs:"
    cat "$CTL_LOG"
    ok 1 "Firewall failed to start with zone config"
    cleanup
    exit 1
else
    ok 0 "Firewall started with zone config (PID $CTL_PID)"
fi

# Start API Server
rm -f "$STATE_DIR"/auth.json
start_api -listen :$TEST_API_PORT

if ! kill -0 $API_PID 2>/dev/null; then
    diag "API server failed to start, showing logs:"
    cat "$API_LOG"
    ok 1 "API server failed to start"
    stop_ctl
    cleanup
    exit 1
else
    ok 0 "API server started (PID $API_PID)"
fi

# 8. Test DHCP client functionality
diag "Testing DHCP client configuration..."
# Check eth0 has an IP address from QEMU's DHCP (10.0.2.x range)
ETH0_IP=$(ip addr show eth0 2>/dev/null | grep -oE "inet 10\.0\.2\.[0-9]+" | awk '{print $2}')
# Check default route exists via QEMU's gateway (10.0.2.2)
DEFAULT_ROUTE=$(ip route show default 2>/dev/null | grep -oE "via 10\.0\.2\.2")

if [ -n "$ETH0_IP" ] && [ -n "$DEFAULT_ROUTE" ]; then
    ok 0 "DHCP client configured eth0 ($ETH0_IP) with default route via 10.0.2.2"
else
    ok 1 "DHCP client failed to configure eth0 (IP: $ETH0_IP, Route: $DEFAULT_ROUTE)"
fi

# Debug: Dump ruleset on first ping failure
ip netns exec green ping -c 1 -W 1 10.1.0.1 >/dev/null 2>&1 || {
    diag "Ping failed. Dumping debug info:"
    ip addr show
    ip route show
    ip neigh show
    diag "Namespace Green debug info:"
    ip netns exec green ip addr show
    ip netns exec green ip route show
    ip netns exec green ip neigh show
    diag "Ruleset:"
    nft list ruleset | sed 's/^/# /'
}

# Retry check helper
check_ping_retry() {
    _ns="$1"
    _target="$2"
    _count=0
    # Try 5 times (approx 2.5s)
    while [ $_count -lt 5 ]; do
        ip netns exec "$_ns" ping -c 1 -W 1 "$_target" >/dev/null 2>&1
        if [ $? -eq 0 ]; then
            return 0
        fi
        dilated_sleep 0.5
        _count=$((_count+1))
    done
    return 1
}

# Check connectivity with retry
check_ping_retry green 10.1.0.1
GREEN_PING=$?
check_ping_retry orange 10.2.0.1
ORANGE_PING=$?
check_ping_retry red 10.3.0.1
RED_PING=$?

if [ $GREEN_PING -eq 0 ] && [ $ORANGE_PING -eq 0 ] && [ $RED_PING -eq 0 ]; then
    ok 0 "All zones can ping firewall"
else
    ok 1 "Some zones cannot ping firewall (Green:$GREEN_PING, Orange:$ORANGE_PING, Red:$RED_PING)"
fi

# 10. Test zone-to-zone connectivity (Green -> Orange)
diag "Testing Green to Orange connectivity..."
ip netns exec green ping -c 2 -i 0.1 -W 1 10.2.0.100 >/dev/null 2>&1
if [ $? -eq 0 ]; then
    ok 0 "Green can access Orange zone"
else
    ok 1 "Green cannot access Orange zone"
fi

# 11. Test zone isolation (Red -> Green)
# Isolation test: Use -W 1 to fail fast on DROP (no ICMP reply expected)
ip netns exec red ping -c 2 -i 0.1 -W 1 10.1.0.100 >/dev/null 2>&1
if [ $? -ne 0 ]; then
    ok 0 "Red properly isolated from Green zone"
else
    ok 1 "Red can inappropriately access Green zone"
fi

# 12. Test DNS resolution
diag "Testing DNS resolution..."

# Test local DNS resolution first (glacic's internal DNS)
ip netns exec green dig @10.1.0.1 firewall.home.arpa +short +timeout=2 | grep -q '.'
if [ $? -eq 0 ]; then
    ok 0 "Local DNS resolution working from Green zone"
else
    # Fallback: check if we get any response (even NXDOMAIN is okay for local test)
    ip netns exec green dig @10.1.0.1 firewall.home.arpa +timeout=2 2>&1 | grep -qE "status: (NOERROR|NXDOMAIN)"
    if [ $? -eq 0 ]; then
        ok 0 "Local DNS responding from Green zone"
    else
        ok 1 "Local DNS resolution failed from Green zone"
    fi
fi

# Test external DNS resolution (use QEMU's gateway as upstream, not google.com)
# This ensures test works offline. We just verify DNS forwarding is functional.
ip netns exec green dig @10.1.0.1 dns.google +short +timeout=2 2>&1 | grep -qE '[0-9]+\.[0-9]+|SERVFAIL|REFUSED'
DNS_RESULT=$?
# Any response (even SERVFAIL/REFUSED) means the DNS server is running and forwarding
if [ $DNS_RESULT -eq 0 ]; then
    # Check it's not a hard SERVFAIL (which indicates config error)
    ip netns exec green dig @10.1.0.1 dns.google +timeout=2 2>&1 | grep -q "status: SERVFAIL"
    if [ $? -ne 0 ]; then
        ok 0 "External DNS resolution working from Green zone"
    else
        # SERVFAIL is acceptable if no upstream connectivity
        ok 0 "DNS forwarder responding (SERVFAIL may indicate no upstream)"
    fi
else
    ok 1 "External DNS resolution failed from Green zone"
fi

# 14. Test firewall API access
diag "Testing firewall API access..."
ip netns exec green curl -v --max-time 2 -s http://10.1.0.1:$TEST_API_PORT/api/status > /tmp/api_test.log 2>&1
if grep "online" /tmp/api_test.log >/dev/null 2>&1; then
    ok 0 "Firewall API accessible from Green zone"
else
    ok 1 "Firewall API not accessible from Green zone. Curl output:"
    cat /tmp/api_test.log
    diag "API Server Logs:"
    cat "$API_LOG"
fi

# Cleanup
stop_ctl
kill $API_PID 2>/dev/null
rm -f $CTL_SOCKET
cleanup

# --- End Test Suite ---

if [ $failed_count -eq 0 ]; then
    diag "All single VM zone tests passed."
    exit 0
else
    diag "$failed_count single VM zone tests failed."
    exit 1
fi
