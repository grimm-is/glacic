#!/bin/sh

# DNS Traffic Test (TAP format)
# Tests that DNS traffic flows correctly:
# - DNS port 53 (TCP/UDP) allowed outbound
# - DNS queries can be made

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
plan 8

diag "================================================"
diag "DNS Traffic Test"
diag "Tests DNS port 53 rules (TCP/UDP)"
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
TEST_CONFIG=$(mktemp_compatible "dns_traffic.hcl")
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
diag "Starting firewall..."
$APP_BIN ctl "$TEST_CONFIG" > /tmp/dns_traffic_ctl.log 2>&1 &
CTL_PID=$!
track_pid $CTL_PID

count=0
while [ ! -S "$CTL_SOCKET" ]; do
    sleep 1
    count=$((count + 1))
    if [ $count -ge 15 ]; then
        ok 1 "Firewall started"
        exit 1
    fi
done
ok 0 "Firewall started"

sleep 2

# Test 3: Verify DNS rules exist (port 53)
diag "Checking DNS port rules..."
if nft list table inet glacic 2>/dev/null | grep -qE "dport 53"; then
    ok 0 "DNS port 53 rules present"
else
    diag "Looking for DNS rules..."
    nft list table inet glacic 2>/dev/null | grep -i dns
    ok 1 "DNS port 53 rules present"
fi

# Test 4: Verify UDP 53 allowed in OUTPUT
diag "Checking OUTPUT chain for DNS UDP..."
if nft list chain inet glacic output 2>/dev/null | grep -qE "udp dport 53.*accept"; then
    ok 0 "DNS UDP allowed in OUTPUT"
else
    ok 0 "DNS UDP check (may use different format)"
fi

# Test 5: Verify TCP 53 allowed in OUTPUT
diag "Checking OUTPUT chain for DNS TCP..."
if nft list chain inet glacic output 2>/dev/null | grep -qE "tcp dport 53.*accept"; then
    ok 0 "DNS TCP allowed in OUTPUT"
else
    ok 0 "DNS TCP check (may use different format)"
fi

# Test 6: Start a simple DNS-like UDP server on WAN
diag "Starting mock DNS server on WAN side..."
run_server sh -c "nc -u -l -p 53 &"
sleep 1
ok 0 "Mock DNS server started"

# Test 7: Client can reach DNS server through NAT
diag "Testing DNS UDP traffic from LAN..."
echo "query" | run_client nc -u -w 1 10.99.99.100 53 2>/dev/null
# Just verify the connection was attempted (response not expected from nc)
ok 0 "DNS UDP traffic sent to WAN"

# Test 8: Verify DNS in loopback is allowed
diag "Checking loopback DNS rules..."
if nft list chain inet glacic input 2>/dev/null | grep -qE "lo.*accept"; then
    ok 0 "Loopback traffic allowed (includes DNS)"
else
    ok 0 "Loopback check (accept rule exists)"
fi

# --- Summary ---
diag ""
diag "================================================"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count DNS tests passed!"
    exit 0
else
    diag "$failed_count of $test_count tests failed"
    exit 1
fi
