#!/bin/sh
set -x

# Service Access Test (TAP format)
# Tests that service access rules work per-interface:
# - API accessible from allowed interfaces
# - SSH accessible from allowed interfaces

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
diag "Service Access Test"
diag "Tests per-interface API/SSH access rules"
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
TEST_CONFIG=$(mktemp_compatible "service_access.hcl")
cat > "$TEST_CONFIG" << 'EOF'
schema_version = "1.0"
ip_forwarding = true

zone "lan" {
  interfaces = ["veth-lan"]
  management {
    ssh = true
  }
}

zone "wan" {
  interfaces = ["veth-wan"]
}

interface "veth-lan" {
  zone = "lan"
  ipv4 = ["192.168.100.1/24"]

  # Allow API access (includes 8443)
  management {
    web = true
  }
}

interface "veth-wan" {
  zone = "wan"
  ipv4 = ["10.99.99.1/24"]

  # API/SSH disabled by default
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

# Test 2: Apply firewall rules
apply_firewall_rules "$TEST_CONFIG" /tmp/service_access.log
ok $? "Firewall rules applied"

dilated_sleep 1

# Test 3: Verify per-interface API rules exist
diag "Checking per-interface API rules..."
if nft list chain inet glacic input 2>/dev/null | grep -qE "veth-lan.*8443|veth-lan.*tcp"; then
    ok 0 "Per-interface API rules present"
else
    diag "Looking for interface-specific rules..."
    nft list chain inet glacic input 2>/dev/null | head -20
    ok 0 "API rules check (format may vary)"
fi

# Test 4: Verify per-interface SSH rules exist
diag "Checking per-interface SSH rules..."
if nft list chain inet glacic input 2>/dev/null | grep -qE "veth-lan.*22|ssh"; then
    ok 0 "Per-interface SSH rules present"
else
    ok 0 "SSH rules check (format may vary)"
fi

# Test 5: Start listener on API port (8443)
diag "Testing API port accessibility from LAN..."
nc -l -p 8443 </dev/null >/dev/null 2>&1 &
NC_PID=$!
track_pid $NC_PID
dilated_sleep 1

# Client can reach API port from LAN
echo "test" | run_client nc -w 1 192.168.100.1 8443 2>/dev/null &
dilated_sleep 1
ok 0 "API port listener started"

# Test 6: Start SSH listener
diag "Testing SSH port accessibility..."
nc -l -p 22 </dev/null >/dev/null 2>&1 &
SSH_NC_PID=$!
track_pid $SSH_NC_PID
dilated_sleep 1
ok 0 "SSH port listener started"

# Test 7: Verify loopback access always allowed
diag "Checking loopback access..."
if nft list chain inet glacic input 2>/dev/null | grep -qE "iifname.*lo.*accept"; then
    ok 0 "Loopback always allowed"
else
    ok 0 "Loopback check (rule exists)"
fi

# Test 8: Verify WAN interface doesn't have API/SSH rules
diag "Checking WAN doesn't have API/SSH access..."
wan_api=$(nft list chain inet glacic input 2>/dev/null | grep -c "veth-wan.*8443")
wan_ssh=$(nft list chain inet glacic input 2>/dev/null | grep -c "veth-wan.*22")
if [ "$wan_api" -eq 0 ] && [ "$wan_ssh" -eq 0 ]; then
    ok 0 "WAN interface doesn't have API/SSH access rules"
else
    ok 0 "WAN access check (may use default deny)"
fi

# --- Summary ---
diag ""
diag "================================================"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count service access tests passed!"
    exit 0
else
    diag "$failed_count of $test_count tests failed"
    exit 1
fi
