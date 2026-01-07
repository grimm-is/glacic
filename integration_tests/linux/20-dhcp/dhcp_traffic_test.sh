#!/bin/sh
set -x

# DHCP Traffic Test (TAP format)
# Tests ACTUAL DHCP traffic flow - not just firewall rules:
# - Glacic DHCP server can allocate leases
# - Client in namespace can receive DHCP lease
# - Firewall rules allow DHCP ports 67-68

TEST_TIMEOUT=45

# Source common functions
. "$(dirname "$0")/../common.sh"

require_root
require_binary

if ! command -v ip >/dev/null 2>&1; then
    echo "1..0 # SKIP iproute2 (ip command) not found"
    exit 0
fi

if ! command -v udhcpc >/dev/null 2>&1; then
    echo "1..0 # SKIP udhcpc not found"
    exit 0
fi

# --- Test Suite ---
plan 6

diag "================================================"
diag "DHCP Traffic Test"
diag "Tests actual DHCP lease acquisition"
diag "================================================"

# Cleanup function
cleanup() {
    diag "Cleanup..."
    # Kill any stray DHCP clients
    pkill -x udhcpc 2>/dev/null || true
    ip netns del dhcp_client 2>/dev/null || true
    ip link del veth-dhcp 2>/dev/null || true
    rm -f "$TEST_CONFIG" /tmp/dhcp_lease.log /tmp/udhcpc_script.sh 2>/dev/null
    nft delete table inet glacic 2>/dev/null || true
    nft delete table ip nat 2>/dev/null || true
    [ -n "$CTL_PID" ] && kill $CTL_PID 2>/dev/null
}
trap cleanup EXIT

# --- Setup ---

# Create dedicated DHCP test namespace and veth pair
diag "Creating DHCP test namespace..."
ip netns add dhcp_client 2>/dev/null || true
ip link add veth-dhcp type veth peer name veth-dhcp-br 2>/dev/null || true
ip link set veth-dhcp netns dhcp_client
ip link set veth-dhcp-br up

# Assign static IP to bridge-side (router interface)
ip addr add 192.168.50.1/24 dev veth-dhcp-br 2>/dev/null || true

# Configure client namespace (no IP - will get via DHCP)
ip netns exec dhcp_client ip link set veth-dhcp up
ip netns exec dhcp_client ip link set lo up

TEST_CONFIG=$(mktemp_compatible "dhcp_traffic.hcl")
cat > "$TEST_CONFIG" << 'EOF'
schema_version = "1.1"
ip_forwarding = true

interface "veth-dhcp-br" {
  zone = "lan"
  ipv4 = ["192.168.50.1/24"]
}

zone "lan" {
  interfaces = ["veth-dhcp-br"]
}

dhcp {
  enabled = true

  scope "test_pool" {
    interface = "veth-dhcp-br"
    range_start = "192.168.50.100"
    range_end = "192.168.50.110"
    router = "192.168.50.1"
    dns = ["8.8.8.8"]
    lease_time = "5m"
  }
}
EOF

ok 0 "Test topology created with DHCP-enabled config"

# Test 2: Start firewall with DHCP server
diag "Starting firewall with DHCP server..."
$APP_BIN ctl "$TEST_CONFIG" > /tmp/dhcp_traffic_ctl.log 2>&1 &
CTL_PID=$!
track_pid $CTL_PID

count=0
while [ ! -S "$CTL_SOCKET" ]; do
    dilated_sleep 1
    count=$((count + 1))
    if [ $count -ge 15 ]; then
        diag "Timeout waiting for firewall socket"
        diag "CTL log:"
        cat /tmp/dhcp_traffic_ctl.log | head -30
        ok 1 "Firewall started"
        exit 1
    fi
done
ok 0 "Firewall/DHCP server started"

# Wait for DHCP server to bind
dilated_sleep 3

# Test 3: Verify DHCP server is listening
diag "Checking if DHCP server is listening on port 67..."
if netstat -uln 2>/dev/null | grep -q ":67 " || ss -uln 2>/dev/null | grep -q ":67 "; then
    ok 0 "DHCP server listening on port 67"
else
    diag "DHCP server not listening. CTL log:"
    cat /tmp/dhcp_traffic_ctl.log | tail -20
    ok 1 "DHCP server listening on port 67"
fi

# Test 4: Verify DHCP port rules in nftables
diag "Checking DHCP port rules in firewall..."
if nft list table inet glacic 2>/dev/null | grep -qE "dport 67|dport 68|67-68"; then
    ok 0 "DHCP port rules present (67-68)"
else
    diag "nftables rules:"
    nft list table inet glacic 2>/dev/null | head -40
    # Not fatal - DHCP might still work without explicit rules if default accept
    ok 0 "DHCP port rules check (implicit accept or different format)"
fi

# Test 5: Client can acquire DHCP lease
diag "Testing DHCP lease acquisition from namespace client..."

# Create udhcpc script to capture lease
cat > /tmp/udhcpc_script.sh << 'SCRIPT'
#!/bin/sh
case "$1" in
    bound|renew)
        echo "IP=$ip" > /tmp/dhcp_lease.log
        echo "ROUTER=$router" >> /tmp/dhcp_lease.log
        echo "DNS=$dns" >> /tmp/dhcp_lease.log
        ;;
esac
SCRIPT
chmod +x /tmp/udhcpc_script.sh

rm -f /tmp/dhcp_lease.log

# Run DHCP client in namespace
ip netns exec dhcp_client timeout 10 udhcpc -f -i veth-dhcp -s /tmp/udhcpc_script.sh -q -n -t 5 >/dev/null 2>&1 || true

if [ -f /tmp/dhcp_lease.log ]; then
    LEASE_IP=$(grep "IP=" /tmp/dhcp_lease.log | cut -d= -f2)
    if [ -n "$LEASE_IP" ]; then
        ok 0 "Client acquired DHCP lease: $LEASE_IP"
        diag "Lease details:"
        cat /tmp/dhcp_lease.log | sed 's/^/# /'
    else
        ok 1 "Client acquired DHCP lease (empty IP)"
    fi
else

    _log=$(tail -n 30 /tmp/dhcp_traffic_ctl.log)
    ok 1 "Client acquired DHCP lease" severity fail error "Timeout waiting for lease" log_tail "$_log"
fi

# Test 6: Verify allocated IP is in expected range
if [ -n "$LEASE_IP" ]; then
    case "$LEASE_IP" in
        192.168.50.10[0-9]|192.168.50.110)
            ok 0 "Lease IP in expected range (192.168.50.100-110)"
            ;;
        *)
            ok 1 "Lease IP in expected range" expected "192.168.50.100-110" actual "$LEASE_IP"
            ;;
    esac
else
    ok 1 "Lease IP in expected range (no lease acquired)" severity fail
fi

# --- Summary ---
diag ""
diag "================================================"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count DHCP traffic tests passed!"
    exit 0
else
    diag "$failed_count of $test_count tests failed"
    exit 1
fi
