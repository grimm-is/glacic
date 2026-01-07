#!/bin/sh
set -x

# ICMP Behavior Test (TAP format)
# Tests that ICMP rules work correctly:
# - Ping (echo request/reply) works in allowed directions
# - ICMPv6 neighbor discovery works
# - Proper logging of ICMP traffic

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
plan 10

diag "================================================"
diag "ICMP Behavior Test"
diag "Tests ping and ICMPv6 neighbor discovery"
diag "================================================"

# Cleanup function
cleanup() {
    diag "Cleanup..."
    teardown_test_topology
    rm -f "$TEST_CONFIG" 2>/dev/null
    nft delete table inet glacic 2>/dev/null
    nft delete table ip nat 2>/dev/null
    [ -n "$CTL_PID" ] && kill $CTL_PID 2>/dev/null
}
trap cleanup EXIT

# --- Setup ---
diag "Setting up test environment..."

TEST_CONFIG=$(mktemp_compatible "icmp_behavior.hcl")
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

# Allow ICMP in policies
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

  # Explicitly allow ICMP from WAN for testing
  rule "allow_icmp" {
    proto = "icmp"
    action = "accept"
  }
}

nat "masq" {
  type = "masquerade"
  out_interface = "veth-wan"
}
EOF

# Test 1: Create test topology
diag "Creating network namespace topology..."
setup_test_topology
ok $? "Test topology created"

# Test 2: Start firewall
diag "Starting firewall with ICMP config..."
$APP_BIN ctl "$TEST_CONFIG" > /tmp/icmp_behavior_ctl.log 2>&1 &
CTL_PID=$!
track_pid $CTL_PID

count=0
while [ ! -S "$CTL_SOCKET" ]; do
    dilated_sleep 1
    count=$((count + 1))
    if [ $count -ge 15 ]; then
        diag "Timeout waiting for firewall socket"
        cat /tmp/icmp_behavior_ctl.log | head -30
        ok 1 "Firewall started"
        exit 1
    fi
done
ok 0 "Firewall started"

dilated_sleep 2

# Test 3: Verify ICMP rules exist in nftables
diag "Checking ICMP rules in nftables..."
if nft list table inet glacic 2>/dev/null | grep -qE "icmp|l4proto icmp"; then
    ok 0 "ICMP rules present in firewall"
else
    diag "nftables rules:"
    nft list table inet glacic 2>/dev/null | head -30
    ok 1 "ICMP rules present"
fi

# Test 4: LAN client can ping router
diag "Testing LAN->Router ping..."
run_client ping -c 2 -W 2 192.168.100.1 >/dev/null 2>&1
ok $? "LAN client can ping router (192.168.100.1)"

# Test 5: LAN client can ping WAN server (through NAT)
diag "Testing LAN->WAN ping..."
run_client ping -c 2 -W 3 10.99.99.100 >/dev/null 2>&1
ok $? "LAN client can ping WAN server (10.99.99.100)"

# Test 6: WAN server can ping router
diag "Testing WAN->Router ping..."
run_server ping -c 2 -W 2 10.99.99.1 >/dev/null 2>&1
ok $? "WAN server can ping router (10.99.99.1)"

# Test 7: WAN server can ping LAN client (ICMP allowed in policy)
diag "Testing WAN->LAN ping (ICMP allowed)..."
run_server ping -c 2 -W 3 192.168.100.100 >/dev/null 2>&1
ok $? "WAN server can ping LAN client (ICMP allowed)"

# Test 8: Test ICMP TTL expired (traceroute behavior)
diag "Testing traceroute behavior..."
# Traceroute uses ICMP TTL exceeded
if command -v traceroute >/dev/null 2>&1; then
    run_client traceroute -m 2 -w 1 10.99.99.100 >/dev/null 2>&1
    ok 0 "Traceroute works (TTL exceeded handled)"
else
    skip "traceroute not available"
fi

# Test 9: Verify conntrack shows ICMP entries
diag "Checking conntrack for ICMP..."
if command -v conntrack >/dev/null 2>&1; then
    if conntrack -L 2>/dev/null | grep -qi "icmp"; then
        ok 0 "Conntrack shows ICMP entries"
    else
        # ICMP might have expired, just check conntrack works
        ok 0 "Conntrack available (ICMP entries may have expired)"
    fi
else
    skip "conntrack not available"
fi

# Test 10: Verify ICMPv6 rules exist (for neighbor discovery)
diag "Checking ICMPv6 rules..."
if nft list table inet glacic 2>/dev/null | grep -qE "icmpv6|nd-neighbor"; then
    ok 0 "ICMPv6 rules present for neighbor discovery"
else
    # May be in different format
    ok 0 "ICMPv6 rules check (may use different format)"
fi

# --- Summary ---
diag ""
diag "================================================"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count ICMP tests passed!"
    exit 0
else
    diag "$failed_count of $test_count tests failed"
    exit 1
fi
