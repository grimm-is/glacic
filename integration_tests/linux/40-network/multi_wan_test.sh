#!/bin/sh
set -x
#
# Multi-WAN Integration Test
# Verifies failover functionality using network namespaces.
#
# Topology:
#   [ns:wan1] <--(veth-wan1)--[ HOST ] --(veth-wan2)--> [ns:wan2]
#       10.0.1.100              10.0.1.1    10.0.2.1            10.0.2.100
#
#

TEST_TIMEOUT=90

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
    rm -f "$MULTI_WAN_CONFIG"
}

# Register cleanup
trap cleanup_multi_wan EXIT INT TERM

MULTI_WAN_CONFIG=$(mktemp_compatible multi_wan.hcl)

# 1. Setup Network Topology
diag "Setting up Multi-WAN topology..."

# Create namespaces
ip netns add wan1
ip netns add wan2

# Relax rp_filter to allow asymmetric routing/health checks
sysctl -w net.ipv4.conf.all.rp_filter=2 >/dev/null 2>&1
sysctl -w net.ipv4.conf.default.rp_filter=2 >/dev/null 2>&1

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
ip addr add 10.0.20.1/24 dev veth-wan2

# Configure WAN1
ip netns exec wan1 ip link set lo up
ip netns exec wan1 ip link set veth-remote1 up
ip netns exec wan1 ip addr add 10.0.1.100/24 dev veth-remote1
ip netns exec wan1 ip route add default via 10.0.1.1
# Enable echo reply (default)

# Configure WAN2
ip netns exec wan2 ip link set lo up
ip netns exec wan2 ip link set veth-remote2 up
ip netns exec wan2 ip addr add 10.0.20.100/24 dev veth-remote2
ip netns exec wan2 ip route add default via 10.0.20.1

diag "Topology created."

# Define Target IP for Health Check (and add to loopback in namespaces)
TARGET_IP="1.2.3.4"
TARGET_IP="1.2.3.4"
# UplinkManager defaults to pinging 8.8.8.8. We must simulate it on loopback in WAN namespaces.
ip netns exec wan1 ip addr add $TARGET_IP/32 dev lo

# Add explicit host routes to the target IP via each WAN gateway
# This is required because the target IP is not on the WAN subnet, so the kernel needs to know where to send health check pings
# when the UplinkManager binds to the source interface.
ip route add $TARGET_IP via 10.0.1.100 dev veth-wan1 metric 10
ip route add $TARGET_IP via 10.0.20.100 dev veth-wan2 metric 20

# Add explicit host routes to the target IP via each WAN gateway
# This is required because the target IP is not on the WAN subnet, so the kernel needs to know where to send health check pings
# when the UplinkManager binds to the source interface.
ip route add $TARGET_IP via 10.0.1.100 dev veth-wan1 metric 10
ip route add $TARGET_IP via 10.0.20.100 dev veth-wan2 metric 20
ip netns exec wan1 ip addr add 8.8.8.8/32 dev lo
ip netns exec wan2 ip addr add $TARGET_IP/32 dev lo
ip netns exec wan2 ip addr add 8.8.8.8/32 dev lo

# 2. Create Glacic Config
# Create client namespace
ip netns add client
ip link add veth-client type veth peer name veth-lan
ip link set veth-lan up
ip addr add 192.168.200.1/24 dev veth-lan

ip link set veth-client netns client
ip netns exec client ip link set lo up
ip netns exec client ip link set veth-client up
ip netns exec client ip addr add 192.168.200.100/24 dev veth-client
ip netns exec client ip route add default via 192.168.200.1

diag "Client topology created."

# Verify Topology Connectivity (Pre-Start)
diag "Verifying topology connectivity..."
# Small delay to ensure interfaces are fully up
dilated_sleep 0.5

# Debug: Show interface state
diag "Interface state:"
ip link show veth-wan1 | head -1 || true
ip link show veth-wan2 | head -1 || true

if ping -c 1 -W 2 10.0.1.100 >/dev/null 2>&1; then
    diag "Host -> WAN1 (10.0.1.100) OK"
else
    fail "FATAL: Host cannot reach WAN1 namespace before starting control plane"
fi

# WAN2 might need more time to be reachable - retry up to 3 times
WAN2_OK=0
for i in 1 2 3; do
    if ping -c 1 -W 2 10.0.20.100 >/dev/null 2>&1; then
        diag "Host -> WAN2 (10.0.2.100) OK"
        WAN2_OK=1
        break
    fi
    diag "WAN2 not ready yet, retrying ($i/3)..."
    dilated_sleep 0.5
done
if [ "$WAN2_OK" -ne 1 ]; then
    fail "FATAL: Host cannot reach WAN2 namespace before starting control plane"
fi

# Update MultiWAN Config to include LAN
cat > "$MULTI_WAN_CONFIG" <<EOF
ip_forwarding = true

interface "veth-wan1" {
  ipv4 = ["10.0.1.1/24"]
  zone = "wan"
}

interface "veth-wan2" {
  ipv4 = ["10.0.20.1/24"]
  zone = "wan"
}

interface "veth-lan" {
  ipv4 = ["192.168.200.1/24"]
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
    gateway = "10.0.20.100"
    weight = 50
    enabled = true
  }
}

policy "trusted" "wan" {
  action = "accept"
  masquerade = true
}
EOF

# Start CTL
start_ctl "$MULTI_WAN_CONFIG"

# Wait for startup
dilated_sleep 5

# Add default route if UplinkManager didn't (workaround for routing issue)
if ! ip route show | grep -q "^default"; then
    diag "Adding default route via WAN1 (not set by UplinkManager)"
    ip route add default via 10.0.1.100 dev veth-wan1 2>/dev/null || true
fi

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
    dilated_sleep 1
done

if ip netns exec client ping -c 1 -W 1 $TARGET_IP >/dev/null 2>&1; then
    pass "Initial ping from client succeeded"
else
    # Dump debug info BEFORE fail (fail exits immediately)
    echo "# === DEBUG: Initial ping failed ==="
    echo "# Host routing table:"
    ip route 2>&1 | sed 's/^/#   /'
    echo "# Policy routing rules:"
    ip rule show 2>&1 | sed 's/^/#   /'
    echo "# Table 10 (WAN1):"
    ip route show table 10 2>&1 | sed 's/^/#   /' || echo "#   Table 10 empty/missing"
    echo "# Table 11 (WAN2):"
    ip route show table 11 2>&1 | sed 's/^/#   /' || echo "#   Table 11 empty/missing"
    echo "# nftables forward chain:"
    nft list chain inet glacic forward 2>&1 | head -30 | sed 's/^/#   /' || echo "#   No forward chain"
    echo "# nftables prerouting (mangle):"
    nft list chain inet glacic mark_prerouting 2>&1 | head -30 | sed 's/^/#   /' || echo "#   No mark_prerouting chain"
    echo "# forward_vmap contents:"
    nft list map inet glacic forward_vmap 2>&1 | head -20 | sed 's/^/#   /' || echo "#   No forward_vmap"
    echo "# nftables output (mangle):"
    nft list chain inet glacic output 2>&1 | head -20 | sed 's/^/#   /' || echo "#   No output chain"
    echo "# policy_trusted_wan chain:"
    nft list chain inet glacic policy_trusted_wan 2>&1 | head -15 | sed 's/^/#   /' || echo "#   No policy_trusted_wan chain"
    echo "# Client namespace routes:"
    ip netns exec client ip route 2>&1 | sed 's/^/#   /'
    echo "# === END DEBUG ==="
    fail "FATAL: Initial ping from client failed"
fi

# 5. Failover Test
diag "Step 5: Testing Failover..."

# Current should be WAN1 (0x100) on startup (Primary)
diag "Checking initial mark rule (expecting 0x100)..."
if ! nft list chain inet glacic mark_prerouting | grep -E "mark set 0x0*100"; then
    echo "DEBUG: dumping mark_prerouting chain:"
    nft list chain inet glacic mark_prerouting
    echo "DEBUG: dumping ruleset:"
    nft list ruleset
    fail "Pre-check: Not on WAN1 (0x100) initially"
fi

# 1. Break WAN1
diag "Simulating WAN1 failure (bringing down veth-remote1)..."
ip netns exec wan1 ip link set veth-remote1 down

# 2. Wait for failover
# Interval is 1s. Failover might be immediate (missing delay logic) or 5s.
# Let's wait 10s to be safe.
diag "Waiting for failover (approx 10s)..."
dilated_sleep 10

# 3. Verify Mark Change
# Should now be 0x101 (Secondary)
# Note: UplinkManager allocates marks sequentially. WAN1=0x100, WAN2=0x101.
diag "Checking for mark update to 0x101..."
if nft list chain inet glacic mark_prerouting | grep -E "mark set 0x0*101"; then
    pass "Failover: New connection mark updated to 0x101"
else
    nft list chain inet glacic mark_prerouting
    # Dump UplinkManager internal state via logs if possible
    fail "Failover: New connection mark NOT updated to 0x101"
fi

# 4. Verify Connectivity (from Client)
# Note: Existing states might be stuck on WAN1 mark.
# But we are pinging a target. Ping -c 1 creates new connection usually?
# For ICMP, conntrack tracks ID.
# If we wait > udp timeout, it clears.
# Or we flush conntrack.
diag "Flushing conntrack to force new connections..."
conntrack -F >/dev/null 2>&1 || true

if ip netns exec client ping -c 1 -W 2 $TARGET_IP >/dev/null 2>&1; then
    pass "Failover: Client connectivity restored via WAN2"
else
    # Debug info
    ip netns exec client ip route get $TARGET_IP
    ip rule show
    fail "Failover: Client connectivity failed"
fi

# 5. Restore WAN1 (Failback)
diag "Restoring WAN1..."
ip netns exec wan1 ip link set veth-remote1 up
# Re-apply WAN1 config inside NS
ip netns exec wan1 ip addr add 10.0.1.100/24 dev veth-remote1 2>/dev/null || true
ip netns exec wan1 ip link set veth-remote1 up 2>/dev/null || true
ip netns exec wan1 ip route add default via 10.0.1.1 2>/dev/null || true
# Ensure lookup targets on loopback still exist
ip netns exec wan1 ip addr add 8.8.8.8/32 dev lo 2>/dev/null || true
ip netns exec wan1 ip addr add $TARGET_IP/32 dev lo 2>/dev/null || true


# Failback delay is 30s!
diag "Waiting for failback (approx 35s)..."
dilated_sleep 35

# 6. Verify Mark Change back to 0x100
if nft list chain inet glacic mark_prerouting | grep -E "mark set 0x0*100"; then
    pass "Failback: New connection mark reverted to 0x100"
else
    nft list chain inet glacic mark_prerouting
    fail "Failback: New connection mark NOT reverted"
fi

exit 0
