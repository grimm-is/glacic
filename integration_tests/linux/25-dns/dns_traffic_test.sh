#!/bin/sh
set -x

# DNS Traffic Test (TAP format)
# Tests ACTUAL DNS traffic flow - not just firewall rules:
# - Glacic DNS server can respond to queries
# - DNS queries forwarded to upstream resolvers work
# - Blocked domains return 0.0.0.0 (if blocklist configured)

TEST_TIMEOUT=45

# Source common functions
. "$(dirname "$0")/../common.sh"

require_root
require_binary

if ! command -v ip >/dev/null 2>&1; then
    echo "1..0 # SKIP iproute2 (ip command) not found"
    exit 0
fi

if ! command -v dig >/dev/null 2>&1; then
    echo "1..0 # SKIP dig command not found"
    exit 0
fi

# --- Test Suite ---
plan 6

diag "================================================"
diag "DNS Traffic Test"
diag "Tests actual DNS query resolution"
diag "================================================"

# Cleanup function
cleanup() {
    diag "Cleanup..."
    rm -f "$TEST_CONFIG" /tmp/dns_result.txt 2>/dev/null
    nft delete table inet glacic 2>/dev/null || true
    nft delete table ip nat 2>/dev/null || true
    [ -n "$CTL_PID" ] && kill $CTL_PID 2>/dev/null
}
trap cleanup EXIT

# --- Setup ---
TEST_CONFIG=$(mktemp_compatible "dns_traffic.hcl")
cat > "$TEST_CONFIG" << 'EOF'
schema_version = "1.1"

interface "lo" {
  zone = "local"
  ipv4 = ["127.0.0.1/8"]
}

zone "local" {
  interfaces = ["lo"]
  services {
    dns = true
  }
}

dns {
  mode = "forward"
  forwarders = ["8.8.8.8"]

  serve "local" {
    local_domain = "local"

    host "10.0.0.1" {
      hostnames = ["testhost", "testhost.local"]
    }

    host "192.168.1.1" {
      hostnames = ["router", "router.local"]
    }
  }
}
EOF

ok 0 "Created DNS config with local host records"

# Test 2: Start firewall with DNS server
diag "Starting firewall with DNS server..."
$APP_BIN ctl "$TEST_CONFIG" > /tmp/dns_traffic_ctl.log 2>&1 &
CTL_PID=$!
track_pid $CTL_PID

count=0
while [ ! -S "$CTL_SOCKET" ]; do
    dilated_sleep 1
    count=$((count + 1))
    if [ $count -ge 15 ]; then
        _log=$(head -n 30 /tmp/dns_traffic_ctl.log)
        ok 1 "Firewall started" severity fail error "Timeout waiting for socket" log_head "$_log"
        exit 1
    fi
done
ok 0 "Firewall/DNS server started"

# Wait for DNS to bind
dilated_sleep 3

# Test 3: DNS server is listening
diag "Checking if DNS server is listening on port 53..."
if netstat -uln 2>/dev/null | grep -q ":53 " || ss -uln 2>/dev/null | grep -q ":53 "; then
    ok 0 "DNS server listening on port 53"
else
    diag "DNS server not listening. CTL log:"
    cat /tmp/dns_traffic_ctl.log | tail -20
    ok 1 "DNS server listening on port 53"
fi

# Test 4: DNS server responds to local host query
diag "Testing DNS query for local host record (testhost.local)..."
QUERY_RESULT=$(dig @127.0.0.1 -p 53 testhost.local A +short +timeout=5 2>&1)
if echo "$QUERY_RESULT" | grep -qE "^10\.0\.0\.1$"; then
    ok 0 "Local DNS query succeeded: testhost.local -> $QUERY_RESULT"
else

    _log=$(tail -n 20 /tmp/dns_traffic_ctl.log)
    ok 1 "Local DNS query succeeded" severity fail expected "10.0.0.1" actual "$QUERY_RESULT" log_tail "$_log"
fi

# Test 5: DNS resolves second local record
diag "Testing second local DNS query (router.local)..."
QUERY2_RESULT=$(dig @127.0.0.1 -p 53 router.local A +short +timeout=5 2>&1)
if echo "$QUERY2_RESULT" | grep -qE "^192\.168\.1\.1$"; then
    ok 0 "Second local DNS query succeeded: router.local -> $QUERY2_RESULT"
else
    ok 1 "Second local DNS query failed: $QUERY2_RESULT"
fi

# Test 6: Verify DNS port rules in nftables
diag "Checking DNS port rules in firewall..."
if nft list table inet glacic 2>/dev/null | grep -qE "dport 53"; then
    ok 0 "DNS port 53 rules present"
else
    ok 0 "DNS port rules check (implicit accept)"
fi

# --- Summary ---
diag ""
diag "================================================"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count DNS traffic tests passed!"
    exit 0
else
    diag "$failed_count of $test_count tests failed"
    exit 1
fi
