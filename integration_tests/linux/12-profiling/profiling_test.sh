#!/bin/sh
set -x
# Device Profiling Integration Test
# Verifies that mDNS and DHCP packets are collected and profiled.

TEST_TIMEOUT=60

# Source common functions
. "$(dirname "$0")/../common.sh"

require_root
require_binary

# Use glacic binary for tools (toolbox crashes in netns)
MDNS_PUB="$APP_BIN mdns-publish"
DHCP_REQ="$APP_BIN dhcp-request"
# API_CURL is not used in netns, standard curl on host is fine
# JSON_QUERY not used in script currently (grep is used)

# --- Setup ---
plan 4

diag "Starting Device Profiling Test"

cleanup() {
    diag "Cleanup..."
    teardown_test_topology
    rm -f "$TEST_CONFIG" 2>/dev/null
    stop_ctl
}
trap cleanup EXIT

# 1. Create config
TEST_CONFIG=$(mktemp_compatible "profiling_test.hcl")
cat > "$TEST_CONFIG" << 'EOF'
schema_version = "1.0"
ip_forwarding = true

interface "veth-lan" {
  zone = "lan"
  ipv4 = ["192.168.100.1/24"]
}

zone "lan" {
  interfaces = ["veth-lan"]
}

# Enable mDNS reflector (which enables collector)
mdns {
  enabled = true
  interfaces = ["veth-lan"]
}

dhcp {
  enabled = true
  scope "lan" {
    interface = "veth-lan"
    range_start = "192.168.100.100"
    range_end = "192.168.100.200"
    router = "192.168.100.1"
  }
}

api {
  enabled = true
  require_auth = false
}
EOF

# 2. Setup Topology
setup_test_topology
ok 0 "Test topology created"

# 3. Start Control Plane
export GLACIC_LOG_FILE=stdout
start_ctl "$TEST_CONFIG"

# Start API server explicitly (bypass internal spawn for reliable testing)
export GLACIC_NO_SANDBOX=1
start_api -listen :8085

ok 0 "API ready on port 8085"

# 4. mDNS Profiling
diag "Broadcasting mDNS announcement from client..."
hostname="My-AppleTV.local"
service="_airplay._tcp"
port=7000

# Run in background to ensure it sends enough packets while we poll?
# The tool sends 5 packets then exits. We can just run it.
# Force ARP resolution
diag "Populating ARP table via ping (Host -> Client)..."
ping -c 1 192.168.100.100 >/dev/null 2>&1 || true

diag "ARP Table content:"
cat /proc/net/arp

ip netns exec test_client $MDNS_PUB -iface veth-client -host "$hostname" -service "$service" -port $port -txt "model=AppleTV3,2"

dilated_sleep 2

# Check API via HTTP
response=$(curl -s "http://127.0.0.1:8085/api/network?details=full")

# Verify mDNS data
if echo "$response" | grep -q "My-AppleTV"; then
    ok 0 "Device found via mDNS"
    # Basic grep check. For deeper check we would use jq.
    if echo "$response" | grep -q "_airplay._tcp"; then
         diag "Service found"
    else
         diag "Service NOT found"
         echo "$response"
    fi
else
    ok 1 "Device NOT found via mDNS"
    echo "Response: $response"
    cat "$CTL_LOG"
fi


# 5. DHCP Profiling
diag "Sending DHCP Discover from client..."
# We need to run dhclient or our toolbox.
# Client interface needs to be UP.
ip netns exec test_client ip link set veth-client up

# Send packet
# Fingerprint: 1,3,6,15 (arbitrary)
ip netns exec test_client $DHCP_REQ -iface veth-client -hostname "My-Laptop" -fingerprint "1,3,6,15"

dilated_sleep 2

response=$(curl -s "http://127.0.0.1:8085/api/network?details=full")

if echo "$response" | grep -q "My-Laptop"; then
    # We should also check if the fingerprint is there.
    # The API output might include "dhcp_fingerprint".
    if echo "$response" | grep -q "1,3,6,15"; then
         ok 0 "Device profiled via DHCP"
    else
         ok 1 "DHCP hostname found but fingerprint missing"
         echo "$response"
    fi
else
    ok 1 "Device NOT found via DHCP"
    echo "Response: $response"
fi

if [ $failed_count -eq 0 ]; then
    exit 0
else
    exit 1
fi
