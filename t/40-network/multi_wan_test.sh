#!/bin/sh
#
# Multi-WAN Integration Test
# Verifies failover functionality using network namespaces.
#
# Topology:
#   [ns:wan1] <--(veth-wan1)--[ HOST ] --(veth-wan2)--> [ns:wan2]
#       10.0.1.100              10.0.1.1    10.0.2.1            10.0.2.100
#

# Source common functions
. "$(dirname "$0")/../common.sh"

require_root
require_binary

# Clean up any leftover state
cleanup_multi_wan() {
    ip netns del wan1 2>/dev/null || true
    ip netns del wan2 2>/dev/null || true
    ip link del veth-wan1 2>/dev/null || true
    ip link del veth-wan2 2>/dev/null || true
    stop_ctl
    rm -f multi_wan.hcl
}

# Register cleanup
trap cleanup_multi_wan EXIT INT TERM

# 1. Setup Network Topology
diag "Setting up Multi-WAN topology..."

# Create namespaces
ip netns add wan1
ip netns add wan2

# Create veth pairs
# Link 1
ip link add veth-wan1 type veth peer name veth-remote1
ip link set veth-remote1 netns wan1
ip link set veth-wan1 up
ip addr add 10.0.1.1/24 dev veth-wan1

# Link 2
ip link add veth-wan2 type veth peer name veth-remote2
ip link set veth-remote2 netns wan2
ip link set veth-wan2 up
ip addr add 10.0.2.1/24 dev veth-wan2

# Configure WAN1
ip netns exec wan1 ip link set lo up
ip netns exec wan1 ip link set veth-remote1 up
ip netns exec wan1 ip addr add 10.0.1.100/24 dev veth-remote1
ip netns exec wan1 ip route add default via 10.0.1.1
# Enable echo reply (default)

# Configure WAN2
ip netns exec wan2 ip link set lo up
ip netns exec wan2 ip link set veth-remote2 up
ip netns exec wan2 ip addr add 10.0.2.100/24 dev veth-remote2
ip netns exec wan2 ip route add default via 10.0.2.1

diag "Topology created."

# Define Target IP for Health Check (and add to loopback in namespaces)
TARGET_IP="1.2.3.4"
ip netns exec wan1 ip addr add $TARGET_IP/32 dev lo
ip netns exec wan2 ip addr add $TARGET_IP/32 dev lo

# 2. Create Glacic Config
# Create client namespace
ip netns add client
ip link add veth-client type veth peer name veth-lan
ip link set veth-lan up
ip addr add 10.0.0.1/24 dev veth-lan

ip link set veth-client netns client
ip netns exec client ip link set lo up
ip netns exec client ip link set veth-client up
ip netns exec client ip addr add 10.0.0.100/24 dev veth-client
ip netns exec client ip route add default via 10.0.0.1

diag "Client topology created."

# Verify Topology Connectivity (Pre-Start)
diag "Verifying topology connectivity..."
if ping -c 1 -W 1 10.0.1.100 >/dev/null 2>&1; then
    diag "Host -> WAN1 (10.0.1.100) OK"
else
    fail "FATAL: Host cannot reach WAN1 namespace before starting control plane"
fi

if ping -c 1 -W 1 10.0.2.100 >/dev/null 2>&1; then
    diag "Host -> WAN2 (10.0.2.100) OK"
else
    fail "FATAL: Host cannot reach WAN2 namespace before starting control plane"
fi

# Update MultiWAN Config to include LAN
cat > multi_wan.hcl <<EOF
ip_forwarding = true

interface "veth-wan1" {
  ipv4 = ["10.0.1.1/24"]
  zone = "wan"
}

interface "veth-wan2" {
  ipv4 = ["10.0.2.1/24"]
  zone = "wan"
}

interface "veth-lan" {
  ipv4 = ["10.0.0.1/24"]
  zone = "trusted"
}

multi_wan {
  enabled = true
  mode = "failover"
  
  health_check {
    interval = 1
    timeout = 1
    threshold = 1
    targets = ["1.2.3.4"]
  }

  wan "primary" {
    interface = "veth-wan1"
    gateway = "10.0.1.100"
    weight = 100
    enabled = true
  }

  wan "secondary" {
    interface = "veth-wan2"
    gateway = "10.0.2.100"
    weight = 50
    enabled = true
  }
}

policy "trusted" "wan" {
  action = "accept"
}
EOF

# Start CTL
start_ctl "multi_wan.hcl"

# Wait for startup
sleep 5

# Check for fwmark rules
if ip rule show | grep -q "fwmark 0x100"; then
    pass "Policy rule for WAN1 (fwmark 0x100) present"
else
    ip rule show
    if [ -f "$CTL_LOG" ]; then
        echo "--- CTL LOG Content ---"
        cat "$CTL_LOG"
    fi
    fail "FATAL: Policy routing rules missing (fwmark 0x100)"
fi

if ip rule show | grep -q "fwmark 0x101"; then
    pass "Policy rule for WAN2 (fwmark 0x101) present"
else
    ip rule show
    fail "FATAL: Policy routing rules missing (fwmark 0x101)"
fi

# 4. Verify Initial Connectivity from Client
diag "Verifying initial connectivity from Client..."
MAX_RETRIES=10
for i in $(seq 1 $MAX_RETRIES); do
    if ip netns exec client ping -c 1 -W 1 $TARGET_IP >/dev/null 2>&1; then
        diag "Check successful on attempt $i"
        break
    fi
    diag "Attempt $i/$MAX_RETRIES failed, waiting..."
    sleep 1
done

if ip netns exec client ping -c 1 -W 1 $TARGET_IP >/dev/null 2>&1; then
    pass "Initial ping from client succeeded"
else
    fail "FATAL: Initial ping from client failed"
    # Dump debug info
    ip route
    nft list ruleset
fi

# 5. Simulate WAN1 failure
diag "Simulating WAN1 failure (down interface)..."
# Correct command to bring down interface in namespace
ip netns exec wan1 ip link set veth-remote1 down

# Wait for health check to fail
sleep 10

# 6. Verify failover to WAN2
diag "Verifying failover..."
if ip netns exec client ping -c 1 -W 1 $TARGET_IP >/dev/null 2>&1; then
    pass "Ping succeeded during failover"
else
    ip route
    echo "--- Table 10 (WAN1) ---"
    ip route show table 10
    echo "--- Table 11 (WAN2) ---"
    ip route show table 11
    echo "--- NFT Ruleset ---"
    nft list ruleset
    if [ -f "$CTL_LOG" ]; then
        echo "--- CTL LOG Content ---"
        cat "$CTL_LOG"
    fi
    fail "Ping failed during failover"
fi

# 7. Restore WAN1
diag "Restoring WAN1..."
ip netns exec wan1 ip link set veth-remote1 up
sleep 10

if ip netns exec client ping -c 1 -W 1 $TARGET_IP >/dev/null 2>&1; then
    pass "Ping succeeded after failback"
fi
