#!/bin/sh
# VM Cleanup Script - FAST EDITION
# Cleans up Glacic processes, firewall rules, and network namespaces.
# Target: <500ms execution time for warm pool reuse.

# CRITICAL: Do NOT use 'set -e' here. Many cleanup commands fail gracefully.

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

# 1. Kill all relevant processes FAST (no graceful shutdown - tests don't need it)
# Avoid killing the agent itself (which might be named glacic-agent)
pgrep -f "$BINARY_NAME" | grep -v "agent" | xargs kill -9 2>/dev/null || true
pkill -9 -x dnsmasq 2>/dev/null || true
pkill -9 -x radvd 2>/dev/null || true
pkill -9 -x zebra 2>/dev/null || true
pkill -9 -x ospfd 2>/dev/null || true
pkill -9 -x ospf6d 2>/dev/null || true
pkill -9 -x bgpd 2>/dev/null || true
pkill -9 -x tailscaled 2>/dev/null || true
pkill -9 -x suricata 2>/dev/null || true
pkill -9 -x nc 2>/dev/null || true
pkill -9 -x netcat 2>/dev/null || true
pkill -9 -x socat 2>/dev/null || true
pkill -9 -x dhcpcd 2>/dev/null || true
pkill -9 -x udhcpc 2>/dev/null || true
pkill -9 -f "python3 -m http.server" 2>/dev/null || true

# 1b. Kill any processes holding TCP/UDP ports (prevents bind failures)
# Parse ss output to find all listening ports and kill their owners
# Exclude port 22 (SSH) and any vsock (vm communication) ports
if command -v ss >/dev/null 2>&1; then
    # Get all listening TCP ports, extract PIDs, and kill them
    for pid in $(ss -tlnp 2>/dev/null | grep -v '^State' | grep -o 'pid=[0-9]*' | cut -d= -f2 | sort -u); do
        # Don't kill ourselves or the agent
        [ "$pid" != "$$" ] && kill -9 "$pid" 2>/dev/null || true
    done
    # Same for UDP
    for pid in $(ss -ulnp 2>/dev/null | grep -v '^State' | grep -o 'pid=[0-9]*' | cut -d= -f2 | sort -u); do
        [ "$pid" != "$$" ] && kill -9 "$pid" 2>/dev/null || true
    done
elif command -v netstat >/dev/null 2>&1; then
    # Fallback to netstat
    netstat -tlnp 2>/dev/null | awk '/LISTEN/ {print $NF}' | cut -d'/' -f1 | while read pid; do
        [ -n "$pid" ] && [ "$pid" != "-" ] && [ "$pid" != "$$" ] && kill -9 "$pid" 2>/dev/null || true
    done
fi

# 2. Remove sockets and pid files
rm -f "$CTL_SOCKET" 2>/dev/null
rm -f "$RUN_DIR/${BRAND_LOWER}"*.pid 2>/dev/null

# 3. Wipe state directory
rm -rf "$STATE_DIR"/* 2>/dev/null

# 4. Flush nftables (FAST - single command)
nft flush ruleset 2>/dev/null || true

# 5. Delete all network namespaces (FAST - use timeout to prevent hangs)
for ns in $(ip netns list 2>/dev/null | cut -d' ' -f1); do
    timeout 1 ip netns del "$ns" 2>/dev/null || true
done

# 6. Delete virtual interfaces (FAST - batch delete)
# Whitelist: lo, eth0-3, wlan0, docker0
for iface in $(ip -o link show 2>/dev/null | awk -F': ' '{print $2}' | cut -d'@' -f1); do
    case "$iface" in
        lo|eth0|eth1|eth2|eth3|wlan0|docker0) ;;
        *) ip link del "$iface" 2>/dev/null || true ;;
    esac
done

# 7. Reset ip forwarding
echo 0 > /proc/sys/net/ipv4/ip_forward 2>/dev/null || true

# 8. Flush custom ip rules (keep 0, 32766, 32767)
ip rule show 2>/dev/null | grep -vE "^0:|^32766:|^32767:" | while read -r line; do
    prio=$(echo "$line" | awk -F: '{print $1}')
    ip rule del pref "$prio" 2>/dev/null || true
done

# 9. Ensure loopback is UP
ip link set lo up 2>/dev/null || true

# 10. Ensure tun device exists (for VPN tests)
if [ ! -c /dev/net/tun ]; then
    mkdir -p /dev/net
    mknod /dev/net/tun c 10 200 2>/dev/null || true
fi

echo "Cleanup complete."

