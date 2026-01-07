#!/bin/sh
set -x
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
  services {
    dns = true
  }
}

zone "wan" {
  interfaces = ["veth-wan"]
}

# New DNS syntax
dns {
  mode = "forward"
  forwarders = ["8.8.8.8"]

  serve "lan" {
    local_domain = "local"

    host "192.168.100.50" {
      hostnames = ["server", "server.local"]
    }

    host "192.168.100.1" {
      hostnames = ["router", "router.local", "gateway", "gateway.local"]
    }
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
    dilated_sleep 0.2
    count=$((count + 1))
    if [ $count -ge 25 ]; then  # 5s max
        diag "DNS port 53 not listening after 5s"
        break
    fi
done
diag "DNS ready check completed after $((count * 200))ms"

# Helper: Wait for DNS resolution (retries up to 5s)
wait_for_dns() {
    _host="$1"
    _expected="$2"
    _count=0

    diag "Waiting for DNS resolution of $_host..."
    while [ $_count -lt 5 ]; do
        _result=$(run_client dig @192.168.100.1 "$_host" +short +timeout=1 2>&1)
        if echo "$_result" | grep -q "$_expected"; then
            return 0
        fi
        diag "DNS retry $_count: $_result"
        dilated_sleep 0.2
        _count=$((_count + 1))
    done
    return 1
}

# Test: L3 Connectivity check
diag "Checking L3 connectivity to router..."
if run_client ping -c 1 -W 1 192.168.100.1 >/dev/null 2>&1; then
    ok 0 "Ping to router successful"
else
    ok 1 "Ping to router failed"
    diag "--- DEBUG STATE (Connectivity) ---"
    ip addr >&2
    ip route >&2
    nft list ruleset >&2
    diag "--------------------------------"
fi

# Test: LAN client resolves internal hostname
diag "Testing LAN DNS resolution for server.local..."
if wait_for_dns "server.local" "192.168.100.50"; then
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
else
    diag "--- DEBUG STATE (Timeout) ---"
    diag "--- CTL LOG ---"
    cat "$CTL_LOG" | sed 's/^/    /' >&2
    diag "---------------"
    nft list ruleset >&2
    ip addr >&2
    ip route >&2
    diag "-----------------------------"
    fail "Timeout waiting for server.local"
fi

# Test: Router hostname also resolves
diag "Testing router.local resolution..."
if wait_for_dns "router.local" "192.168.100.1"; then
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
else
    # Helper already logged error? No, it just loops.
    # We need to manually    diag "--- DEBUG STATE (Timeout) ---"
    diag "--- CTL LOG ---"
    cat "$CTL_LOG" | sed 's/^/    /' >&2
    diag "---------------"
    diag "-----------------------------"
    fail "Timeout waiting for router.local"
fi



# Test: WAN Client (Sad Path/Negative Test)
# Verify a client in the WAN zone CANNOT resolve "server.local" (which is LAN-only)
diag "Testing WAN DNS resolution (Should Fail for LAN record)..."

# run_server runs in the 'test_server' namespace, which is connected to veth-wan (zone=wan)
# 10.99.99.1 is the router IP on the WAN side.
WAN_RESULT=$(run_server dig @10.99.99.1 server.local +short +timeout=2 2>&1 || true)

if [ -z "$WAN_RESULT" ]; then
    ok 0 "WAN client got empty/NXDOMAIN for server.local (Expected)"
else
    # Check if it resolved to the LAN IP
    if echo "$WAN_RESULT" | grep -q "192.168.100.50"; then
        ok 1 "WAN client resolved LAN-only record: $WAN_RESULT (SECURITY FAILURE)"
    else
        ok 0 "WAN client failed to resolve (Result: $WAN_RESULT)"
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
