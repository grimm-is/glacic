#!/bin/sh
set -x
#
# VPN Integration Test (WireGuard)
# Verifies full data plane with handshake using a simulated peer.
#
# Topology:
#   [ HOST (Glacic) ] <--(veth-pair)--> [ ns:peer ]
#      wg0: 10.200.0.1                   wg0: 10.200.0.2
#

# Source common functions
. "$(dirname "$0")/../common.sh"

require_root
require_binary

if ! command -v wg >/dev/null 2>&1; then
    echo "1..0 # SKIP wg tool not found"
    exit 0
fi

if ! modprobe wireguard 2>/dev/null; then
    # Usually modules are loaded, but if not we might fail.
    # Check if module is loaded or built-in.
    if [ ! -d /sys/module/wireguard ]; then
         echo "1..0 # SKIP wireguard module not available"
         exit 0
    fi
fi

# Cleanup
cleanup_vpn() {
    ip netns del peer 2>/dev/null || true
    ip link del veth-vpn 2>/dev/null || true
    ip link del wg0 2>/dev/null || true
    stop_ctl
    rm -f "$PRIV_KEY" "$PUB_KEY" "$PEER_PRIV_KEY" "$PEER_PUB_KEY" "$VPN_CONFIG"
}

trap cleanup_vpn EXIT INT TERM

# 1. Setup Network Topology (Simulate Internet connection between Host and Peer)
diag "Setting up VPN topology..."

ip netns add peer
ip link add veth-vpn type veth peer name veth-peer
ip link set veth-peer netns peer

# Host side of physical link (WAN simulation)
ip addr add 172.16.0.1/24 dev veth-vpn
ip link set veth-vpn up

# Peer side of physical link
ip netns exec peer ip addr add 172.16.0.2/24 dev veth-peer
ip netns exec peer ip link set veth-peer up
ip netns exec peer ip link set lo up

# 2. Generate Keys
diag "Generating keys..."
PRIV_KEY=$(mktemp_compatible priv.key)
PUB_KEY=$(mktemp_compatible pub.key)
PEER_PRIV_KEY=$(mktemp_compatible peer_priv.key)
PEER_PUB_KEY=$(mktemp_compatible peer_pub.key)
VPN_CONFIG=$(mktemp_compatible vpn.hcl)

wg genkey | tee "$PRIV_KEY" | wg pubkey > "$PUB_KEY"
wg genkey | tee "$PEER_PRIV_KEY" | wg pubkey > "$PEER_PUB_KEY"

HOST_PRIV=$(cat "$PRIV_KEY")
HOST_PUB=$(cat "$PUB_KEY")
PEER_PRIV=$(cat "$PEER_PRIV_KEY")
PEER_PUB=$(cat "$PEER_PUB_KEY")

# 3. Configure Glacic (Host)
cat > "$VPN_CONFIG" <<EOF
interface "veth-vpn" {
  ipv4 = ["172.16.0.1/24"]
  zone = "wan"
}

zone "wan" {
}

zone "vpn" {
}

policy "vpn" "vpn" {
  action = "accept"
}

vpn {
    wireguard "wg0" {
        enabled = true
        interface = "wg0"
        private_key = "$HOST_PRIV"
        listen_port = 51820

        peer "peer1" {
            public_key = "$PEER_PUB"
            allowed_ips = ["10.200.0.2/32"]
        }
    }
}

# Interface config for the wg interface itself must be defined to assign IP/Zone
interface "wg0" {
    ipv4 = ["10.200.0.1/24"]
    zone = "vpn"
}
EOF

# 4. Start Glacic
plan 2

# Pre-create wg0 to avoid race condition where interface config applies before VPN service starts
ip link add wg0 type wireguard 2>/dev/null || true

start_ctl "$VPN_CONFIG"

# Wait for startup
dilated_sleep 5

# 5. Configure Peer (Manual WireGuard setup)
diag "Configuring Peer WireGuard..."

ip netns exec peer ip link add wg0 type wireguard
ip netns exec peer wg set wg0 \
    private-key "$PEER_PRIV_KEY" \
    listen-port 51820 \
    peer "$HOST_PUB" \
    allowed-ips "10.200.0.1/32" \
    endpoint "172.16.0.1:51820"

ip netns exec peer ip addr add 10.200.0.2/24 dev wg0
ip netns exec peer ip link set wg0 up

# 6. Verify Handshake
diag "Verifying handshake..."

# Send a ping to trigger handshake
ip netns exec peer ping -c 3 -W 1 10.200.0.1 >/dev/null 2>&1

# Check handshake status on Host
if wg show wg0 latest-handshakes | grep -q "$PEER_PUB"; then
    pass "Handshake successful (verified on host)"
else
    # Check if handshake > 0
    LATEST=$(wg show wg0 latest-handshakes | awk '{print $2}')
    if [ "$LATEST" -gt 0 ]; then
        pass "Handshake successful (verified on host, time=$LATEST)"
    else
        diag "Peer output:"
        ip netns exec peer wg show
        diag "Host output:"
        wg show
        if [ -f "$CTL_LOG" ]; then
            diag "Control Plane Log:"
            cat "$CTL_LOG"
        fi
        fail "Handshake failed"
    fi
fi

# 7. Verify Data Plane
if ip netns exec peer ping -c 1 -W 1 10.200.0.1 >/dev/null 2>&1; then
    pass "Data plane working (Ping succeeded)"
else
    fail "Ping through tunnel failed"
fi
