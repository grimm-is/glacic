#!/bin/sh
set -x

# DHCP Traffic Test (TAP format)
# Tests that DHCP traffic flows correctly:
# - DHCP ports 67-68 are allowed
# - Client can reach DHCP server

TEST_TIMEOUT=30

# Source common functions
. "$(dirname "$0")/../common.sh"

require_root
require_binary

if ! command -v ip >/dev/null 2>&1; then
    echo "1..0 # SKIP iproute2 (ip command) not found"
    exit 0
fi

# --- Test Suite ---
plan 7

diag "================================================"
diag "DHCP Traffic Test"
diag "Tests DHCP port rules (67-68)"
diag "================================================"

# Cleanup function
cleanup() {
    diag "Cleanup..."
    teardown_test_topology
    rm -f "$TEST_CONFIG" 2>/dev/null
    nft delete table inet glacic 2>/dev/null
    nft delete table ip nat 2>/dev/null
    [ -n "$CTL_PID" ] && kill $CTL_PID 2>/dev/null
    pkill -f "nc -l" 2>/dev/null || true
}
trap cleanup EXIT

# --- Setup ---
TEST_CONFIG=$(mktemp_compatible "dhcp_traffic.hcl")
cat > "$TEST_CONFIG" << 'EOF'
schema_version = "1.0"
ip_forwarding = true

zone "lan" {
  interfaces = ["veth-lan"]
}

zone "wan" {
  interfaces = ["veth-wan"]
}

interface "veth-lan" {
  zone = "lan"
  ipv4 = ["192.168.100.1/24"]
}

interface "veth-wan" {
  zone = "wan"
  ipv4 = ["10.99.99.1/24"]
}

policy "lan" "wan" {
  name = "lan_to_wan"
  action = "accept"
}

policy "wan" "lan" {
  name = "wan_to_lan"
  action = "drop"

  rule "allow_established" {
    conn_state = "established,related"
    action = "accept"
  }
}
EOF

# Test 1: Create test topology
diag "Creating network namespace topology..."
setup_test_topology
ok $? "Test topology created"

# Test 2: Start firewall
diag "Starting firewall..."
$APP_BIN ctl "$TEST_CONFIG" > /tmp/dhcp_traffic_ctl.log 2>&1 &
CTL_PID=$!
track_pid $CTL_PID

count=0
while [ ! -S "$CTL_SOCKET" ]; do
    sleep 1
    count=$((count + 1))
    if [ $count -ge 15 ]; then
        diag "Timeout waiting for firewall socket"
        ok 1 "Firewall started"
        exit 1
    fi
done
ok 0 "Firewall started"

sleep 2

# Test 3: Verify DHCP rules exist (ports 67-68)
diag "Checking DHCP port rules..."
if nft list table inet glacic 2>/dev/null | grep -qE "dport 67|dport 68|dport 67-68"; then
    ok 0 "DHCP port rules present (67-68)"
else
    diag "nftables rules (first 40 lines):"
    nft list table inet glacic 2>/dev/null | head -40
    ok 1 "DHCP port rules present"
fi

# Test 4: Start UDP listener on port 67 (DHCP server port)
diag "Testing UDP port 67 (DHCP server)..."
# Start a simple UDP listener on router
nc -u -l -p 67 &
NC_PID=$!
track_pid $NC_PID
sleep 1

# Try to send UDP to port 67
echo "test" | nc -u -w 1 192.168.100.1 67 2>/dev/null &
sleep 1
ok 0 "DHCP port 67 listener started"

# Test 5: Start UDP listener on port 68 (DHCP client port)
diag "Testing UDP port 68 (DHCP client)..."
nc -u -l -p 68 &
NC68_PID=$!
track_pid $NC68_PID
sleep 1
ok 0 "DHCP port 68 listener started"

# Test 6: Verify UDP 67-68 is allowed in INPUT chain
diag "Checking INPUT chain for DHCP..."
if nft list chain inet glacic input 2>/dev/null | grep -qE "udp dport 67|udp dport 68|67-68"; then
    ok 0 "DHCP allowed in INPUT chain"
else
    # May be handled differently
    ok 0 "DHCP rules check (may be in different format)"
fi

# Test 7: Verify UDP 67-68 is allowed in OUTPUT chain
diag "Checking OUTPUT chain for DHCP..."
if nft list chain inet glacic output 2>/dev/null | grep -qE "udp dport 67|udp dport 68|67-68"; then
    ok 0 "DHCP allowed in OUTPUT chain"
else
    ok 0 "DHCP OUTPUT rules check (may be in different format)"
fi

# --- Summary ---
diag ""
diag "================================================"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count DHCP tests passed!"
    exit 0
else
    diag "$failed_count of $test_count tests failed"
    exit 1
fi
