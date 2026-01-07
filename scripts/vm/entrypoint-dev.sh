#!/bin/sh

# Development entrypoint - starts firewall with privilege separation
# Does NOT run tests or power off

# Brand mount path (matches glacic-builder)
MOUNT_PATH="/mnt/glacic"

trap "poweroff" EXIT

echo "===== DEV MODE (Privilege Separated) ====="

# Parse kernel args for config_file override
for arg in $(cat /proc/cmdline); do
    case $arg in
        config_file=*)
            KERNEL_CONFIG="${arg#*=}"
            ;;
    esac
done

CONFIG_FILE="${KERNEL_CONFIG:-${1:-firewall.hcl}}"
CONFIG_FILE="${KERNEL_CONFIG:-${1:-firewall.hcl}}"
ARCH=$(uname -m)
case $ARCH in
    aarch64) GOARCH="arm64" ;;
    x86_64) GOARCH="amd64" ;;
    *) GOARCH="$ARCH" ;;
esac

if [ -f "$MOUNT_PATH/build/glacic-linux-$GOARCH" ]; then
    FIREWALL_BIN="$MOUNT_PATH/build/glacic-linux-$GOARCH"
else
    FIREWALL_BIN="$MOUNT_PATH/build/glacic"
fi
CONFIG_PATH="$MOUNT_PATH/$CONFIG_FILE"

# Load kernel modules
echo "🔧 Loading kernel modules..."
modprobe nf_tables 2>/dev/null || echo "❌ Failed to load nf_tables"
modprobe nfnetlink 2>/dev/null || true
modprobe nfnetlink_log 2>/dev/null || true
modprobe nfnetlink_queue 2>/dev/null || true
modprobe nft_chain_nat 2>/dev/null || true
modprobe nft_nat 2>/dev/null || true

# Ensure network is up
echo "🌐 Configuring network..."
ip link set lo up
ip link set eth0 up

# Configure DHCP on WAN (eth0)
if [ -f /usr/share/udhcpc/default.script ]; then
    udhcpc -i eth0 -b -s /usr/share/udhcpc/default.script
else
    udhcpc -i eth0 -b
fi

# Configure static IPs for zones (consistent with vm-dev.sh mac addresses)
ip link set eth1 up
ip addr add 10.1.0.1/24 dev eth1 2>/dev/null || true

ip link set eth2 up
ip addr add 10.2.0.1/24 dev eth2 2>/dev/null || true

ip link set eth3 up
ip addr add 10.3.0.1/24 dev eth3 2>/dev/null || true

# Configure Management Interface (eth4) if present
if ip link show eth4 >/dev/null 2>&1; then
    echo "🔧 Configuring Management Interface (eth4)..."
    ip link set eth4 up
    # We use udhcpc for eth4 as well since it's on the user network (DHCP enabled)
    # Use -n (now) to exit if failure, but don't block forever
    udhcpc -i eth4 -b -q
fi

echo "🌐 Network Status:"
ip link
ip addr
ip route
ping -c 1 8.8.8.8 || echo "❌ No internet access"



# Check if binary exists
if [ ! -f "$FIREWALL_BIN" ]; then
    echo "❌ Firewall binary not found at $FIREWALL_BIN"
    echo "   Run: make build-linux"
    exit 1
fi

# Check if config exists
if [ ! -f "$CONFIG_PATH" ]; then
    echo "❌ Config file not found: $CONFIG_PATH"
    exit 1
fi

echo ""
echo "🔍 Config content check:"
ls -l "$CONFIG_PATH"
cat "$CONFIG_PATH"
echo "---------------------------------------------------"
echo ""
echo "🔧 Starting privileged control plane (as root)..."

# Configure and start SSHD for dev access
if [ -x /usr/sbin/sshd ]; then
    echo "🔧 Configuring SSHD..."
    ssh-keygen -A >/dev/null 2>&1
    echo "PermitRootLogin yes" >> /etc/ssh/sshd_config
    echo "root:root" | chpasswd
    /usr/sbin/sshd
    echo "✅ SSHD started (Port 22)"
else
    echo "⚠️  SSHD not found (skipping)"
fi

# Start control plane (privileged) in background
$FIREWALL_BIN ctl "$CONFIG_PATH" &
CTL_PID=$!

# Wait for control plane to start and create socket
sleep 3

# Check if control plane is running
if ! kill -0 $CTL_PID 2>/dev/null; then
    echo "❌ Control plane failed to start"
    exit 1
fi

echo ""
echo "🌐 Starting API server (as nobody)..."

# Start API server (unprivileged) in background
# Note: -user nobody drops privileges after binding to port
# DISABLED: ctl spawns api itself if configured!
# $FIREWALL_BIN _api-server -listen 0.0.0.0:8080 -user nobody &
# API_PID=$!

# Wait a moment for API startup
sleep 2

# Check if both are running
if kill -0 $CTL_PID 2>/dev/null; then
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  ✅ Control Plane running (PID: $CTL_PID) [root]"
    echo "  ✅ API Server managed by Control Plane"
    echo ""
    echo "  📡 Web UI: http://localhost:8080 (from host)"
    echo "  📡 API:    http://localhost:8080/api/status"
    echo "  🔌 RPC:    /var/run/glacic-ctl.sock"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    # Test API connectivity from inside VM
    echo "🧪 Testing local API connectivity..."

    # Wait for API to fully start with retry loop
    API_READY=0
    for i in 1 2 3 4 5 6 7 8 9 10; do
        sleep 1
        if wget -q -O /tmp/api_test --timeout=2 http://127.0.0.1:8080/api/auth/status 2>/dev/null; then
            API_READY=1
            break
        fi
        echo "  Waiting for API... (${i}s)"
    done

    if [ "$API_READY" = "1" ]; then
        echo "  ✅ Local API test: SUCCESS"
        cat /tmp/api_test
        echo ""
    else
        echo "  ❌ Local API test: FAILED after 10s"
        echo "  Port status:"
        cat /proc/net/tcp 2>/dev/null | head -5 || echo "  /proc/net/tcp not available"
        echo "  Trying netstat..."
        netstat -tlnp 2>/dev/null | grep 8080 || ss -tlnp 2>/dev/null | grep 8080 || echo "  netstat/ss not available"
    fi
    echo ""
    echo "  Type 'poweroff' to shut down the VM"
    echo ""
else
    echo "❌ Failed to start one or more components"
    echo "   CTL running: $(kill -0 $CTL_PID 2>/dev/null && echo yes || echo no)"
    echo "   API running: $(kill -0 $API_PID 2>/dev/null && echo yes || echo no)"
    exit 1
fi

# Drop to interactive shell (don't exit or poweroff)
/bin/sh
