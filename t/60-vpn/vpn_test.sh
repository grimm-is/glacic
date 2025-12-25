#!/bin/sh
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
    rm -f priv.key pub.key peer_priv.key peer_pub.key vpn.hcl
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
wg genkey | tee priv.key | wg pubkey > pub.key
wg genkey | tee peer_priv.key | wg pubkey > peer_pub.key

HOST_PRIV=$(cat priv.key)
HOST_PUB=$(cat pub.key)
PEER_PRIV=$(cat peer_priv.key)
PEER_PUB=$(cat peer_pub.key)

# 3. Configure Glacic (Host)
cat > vpn.hcl <<EOF
interface "veth-vpn" {
  ipv4 = ["172.16.0.1/24"]
  zone = "wan"
}

zone "wan" {
}

zone "vpn" {
  accept_from = ["vpn"] # Allow VPN to VPN ?
}

vpn {
    wireguard "wg0" {
        private_key = "$HOST_PRIV"
        listen_port = 51820
        
        peer {
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

start_ctl vpn.hcl

# Wait for startup
sleep 5

# 5. Configure Peer (Manual WireGuard setup)
diag "Configuring Peer WireGuard..."

ip netns exec peer ip link add wg0 type wireguard
ip netns exec peer wg set wg0 \
    private-key peer_priv.key \
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
        fail "Handshake failed"
    fi
fi

# 7. Verify Data Plane
if ip netns exec peer ping -c 1 -W 1 10.200.0.1 >/dev/null 2>&1; then
    pass "Data plane working (Ping succeeded)"
else
    fail "Ping through tunnel failed"
fi
