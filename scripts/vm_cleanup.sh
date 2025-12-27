#!/bin/sh
# VM Cleanup Script
# Cleans up Glacic processes, firewall rules, and network namespaces.
# This script is intended to be run by 'orca' or test setup/teardown.

set -e

# Only run if we are root (likely inside VM)
if [ "$(id -u)" -ne 0 ]; then
    echo "Must run as root"
    exit 1
fi

BRAND_NAME="${BRAND_NAME:-Glacic}"
BRAND_LOWER="${BRAND_LOWER:-glacic}"
BINARY_NAME="${BINARY_NAME:-glacic}"
STATE_DIR="${STATE_DIR:-/var/lib/glacic}"
RUN_DIR="${RUN_DIR:-/var/run}"
CTL_SOCKET="${CTL_SOCKET:-$RUN_DIR/$BRAND_LOWER-ctl.sock}"

echo "Cleaning up $BRAND_NAME environment..."

# 1. Try graceful shutdown first
# Find the binary...
APP_BIN=$(command -v "$BINARY_NAME" || true)
if [ -n "$APP_BIN" ]; then
    "$APP_BIN" stop 2>/dev/null || true
fi

# 2. Kill any lingering processes (safely)
# We must exclude the toolbox/agent and this script itself
# Warning: pkill -f matches the full command line, which includes /mnt/glacic/... path
echo "Killing lingering processes matching $BINARY_NAME..."
# Exclude:
# - grep process itself (grep -v grep)
# - this script (vm_cleanup)
# - the test runner toolbox (toolbox)
# - the orchestrator (orca)
# - the agent listening for commands (orca-agent)
# - current shell ($$)
pkill -f "$BINARY_NAME" 2>/dev/null || true

# Fallback explicit kill (if exact match works)
pkill -9 -x "$BINARY_NAME" 2>/dev/null || true

# Also kill known dependencies that might linger
pkill -9 -x dnsmasq 2>/dev/null || true
pkill -9 -x radvd 2>/dev/null || true
pkill -9 -x zebra 2>/dev/null || true
pkill -9 -x ospfd 2>/dev/null || true
pkill -9 -x ospf6d 2>/dev/null || true
pkill -9 -x bgpd 2>/dev/null || true
pkill -9 -x tailscaled 2>/dev/null || true
pkill -9 -x suricata 2>/dev/null || true

# Kill lingering test utilities that might hold ports
pkill -9 -x nc 2>/dev/null || true
pkill -9 -x netcat 2>/dev/null || true
pkill -9 -x socat 2>/dev/null || true
pkill -9 -x dhcpcd 2>/dev/null || true
pkill -9 -x udhcpc 2>/dev/null || true
pkill -9 -x wget 2>/dev/null || true
pkill -9 -x curl 2>/dev/null || true
pkill -9 -f "python3 -m http.server" 2>/dev/null || true

# 3. Wait briefly for processes to exit
sleep 0.5

# 4. Remove control socket and pid files
rm -f "$CTL_SOCKET" 2>/dev/null
rm -f "$RUN_DIR/${BRAND_LOWER}"*.pid 2>/dev/null

# 5. Wipe state directory
rm -rf "$STATE_DIR"/* 2>/dev/null

# 6. Flush nftables
if command -v nft >/dev/null 2>&1; then
     nft flush ruleset 2>/dev/null || true
fi

# 7. Cleanup test namespaces and interfaces
echo "Cleaning namespaces and interfaces..."

# Delete all test namespaces (except default)
# Use timeout to prevent hanging on stubborn namespaces
ip netns list 2>/dev/null | cut -d' ' -f1 | while read -r ns; do
    if [ -n "$ns" ]; then
        timeout 2 ip netns del "$ns" 2>/dev/null || true
    fi
done

# Delete virtual interfaces (veth, dummy, bridge, tun, tap, bond, etc.)
# Strategy: Delete everything that IS NOT in the whitelist
# Whitelist: lo, eth0, wlan0 (and maybe docker0 if present)
ip -o link show | awk -F': ' '{print $2}' | while read -r iface; do
    # Strip any @suffix (e.g. veth1@if2)
    clean_iface=${iface%@*}
    
    case "$clean_iface" in
        lo|eth0|eth1|eth2|eth3|wlan0|docker0)
            # Keep system interfaces but clear IPs to avoid cross-test pollution
            if [ "$clean_iface" != "lo" ]; then
                ip addr flush dev "$clean_iface" 2>/dev/null || true
                ip link set "$clean_iface" up 2>/dev/null || true
            fi
            ;;
        *)
            # Delete everything else (veth, dummy, tun, tap, bond, bridge, wg, etc.)
            echo "Deleting interface: $clean_iface"
            ip link del "$clean_iface" 2>/dev/null || true
            ;;
    esac
done

# 8. Reset IP forwarding and routing rules
echo 0 > /proc/sys/net/ipv4/ip_forward 2>/dev/null || true

# Flush custom ip rules (keep defaults: local 0, main 32766, default 32767)
if command -v ip >/dev/null 2>&1; then
    ip rule show | grep -vE "^0:|^32766:|^32767:" | while read -r line; do
        prio=$(echo "$line" | awk -F: '{print $1}')
        ip rule del pref "$prio" 2>/dev/null || true
    done
fi

# Remove bonding and bridge modules if loaded to clear state (with timeout)
timeout 2 modprobe -r bonding 2>/dev/null || true
timeout 2 modprobe -r bridge 2>/dev/null || true
timeout 2 modprobe -r dummy 2>/dev/null || true

# 9. Ensure tun device exists (for VPN tests)
if [ ! -c /dev/net/tun ]; then
    mkdir -p /dev/net
    mknod /dev/net/tun c 10 200 2>/dev/null || true
fi

# 10. Ensure loopback is UP (Critical for unit tests)
ip link set lo up 2>/dev/null || true

echo "Cleanup complete."
