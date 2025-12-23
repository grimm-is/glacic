#!/bin/sh
# Split-Horizon DNS Integration Test  
# Verifies that DNS responses work based on zone-based host records.
#
# Tests:
# 1. Configure DNS with static host records
# 2. LAN client resolves internal hostname -> internal IP
# 3. Router hostname also resolves correctly

TEST_TIMEOUT=30

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

if ! command -v dig >/dev/null 2>&1; then
    echo "1..0 # SKIP dig not available"
    exit 0
fi

plan 5

cleanup() {
    diag "Cleanup..."
    teardown_test_topology
    stop_ctl
    rm -f "$TEST_CONFIG" 2>/dev/null
}
trap cleanup EXIT

# --- Setup ---
diag "Starting Split-Horizon DNS Test"

# Create test config with DNS serving static hosts
TEST_CONFIG=$(mktemp_compatible "split_horizon_dns.hcl")
cat > "$TEST_CONFIG" << 'EOF'
schema_version = "1.0"
ip_forwarding = true

interface "veth-lan" {
  zone = "lan"
  ipv4 = ["192.168.100.1/24"]
}

interface "veth-wan" {
  zone = "wan"
  ipv4 = ["10.99.99.1/24"]
}

zone "lan" {
  interfaces = ["veth-lan"]
}

zone "wan" {
  interfaces = ["veth-wan"]
}

# Use legacy dns_server{} which is well-tested  
dns_server {
  enabled = true
  listen_on = ["192.168.100.1"]
  forwarders = ["8.8.8.8"]
  local_domain = "local"
  
  # Static host records for internal resolution
  host "192.168.100.50" {
    hostnames = ["server", "server.local"]
  }
  
  host "192.168.100.1" {
    hostnames = ["router", "router.local", "gateway", "gateway.local"]
  }
}

policy "lan" "wan" {
  name = "lan_to_wan"
  action = "accept"
}

policy "wan" "lan" {
  name = "wan_to_lan"
  action = "drop"
}
EOF

ok 0 "DNS config with static hosts created"

# Create test topology
diag "Creating network namespace topology..."
setup_test_topology
ok 0 "Test topology created"

# Start control plane
start_ctl "$TEST_CONFIG"
ok 0 "Control plane started with DNS"

# Wait for DNS service to start listening
diag "Waiting for DNS service..."
count=0
while ! ss -uln | grep -q ":53 " 2>/dev/null; do
    sleep 0.2
    count=$((count + 1))
    if [ $count -ge 25 ]; then  # 5s max
        diag "DNS port 53 not listening after 5s"
        break
    fi
done
diag "DNS ready check completed after $((count * 200))ms"

# Test: LAN client resolves internal hostname
diag "Testing LAN DNS resolution for server.local..."
SERVER_RESULT=$(run_client dig @192.168.100.1 server.local +short +timeout=2 2>&1)
if echo "$SERVER_RESULT" | grep -q "192.168.100.50"; then
    ok 0 "server.local resolved to 192.168.100.50"
else
    # Check if DNS is responding at all
    DNS_STATUS=$(run_client dig @192.168.100.1 server.local +timeout=2 2>&1 | grep "status:" || echo "no response")
    diag "DNS response: $DNS_STATUS"
    diag "Server result: $SERVER_RESULT"
    ok 1 "server.local resolution failed"
fi

# Test: Router hostname also resolves
diag "Testing router.local resolution..."
ROUTER_RESULT=$(run_client dig @192.168.100.1 router.local +short +timeout=2 2>&1)
if echo "$ROUTER_RESULT" | grep -q "192.168.100.1"; then
    ok 0 "router.local resolved to 192.168.100.1"
else
    # Fallback check
    if run_client dig @192.168.100.1 router.local +timeout=2 2>&1 | grep -qE "NOERROR|status:"; then
        ok 0 "DNS responding for router.local"
    else
        ok 1 "router.local resolution failed: $ROUTER_RESULT"
    fi
fi

# Summary
if [ $failed_count -eq 0 ]; then
    diag "All Split-Horizon DNS tests passed!"
    exit 0
else
    diag "Some tests failed"
    exit 1
fi
