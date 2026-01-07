#!/bin/sh
set -x

# DNS Blocklist URL Integration Test
# Verifies that DNS blocklists can be downloaded from a URL and enforced.

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

if ! command -v dig >/dev/null 2>&1; then
    echo "1..0 # SKIP dig command not found"
    exit 0
fi

TEST_TIMEOUT=45
BLOCKLIST_PORT=8088
BLOCKLIST_URL="http://127.0.0.1:$BLOCKLIST_PORT/blocklist.txt"
CONFIG_FILE="/tmp/dns_blocklist_url.hcl"

# --- Setup HTTP Server ---

# Create blocklist file
mkdir -p /tmp/www
cat > /tmp/www/blocklist.txt << 'EOF'
0.0.0.0 blocked-domain.com
0.0.0.0 ads.badsite.com
EOF

# Start HTTP server using toolbox
run_service "http-server" "$TOOLBOX_BIN" http -p $BLOCKLIST_PORT -d /tmp/www
dilated_sleep 1

# Verify HTTP server is running
if ! kill -0 $SERVICE_PID 2>/dev/null; then
    echo "1..0 # SKIP toolbox http failed to start"
    exit 0
fi

# --- Create Config ---

cat >"$CONFIG_FILE" <<EOF
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

    blocklist "test_url_list" {
      url = "$BLOCKLIST_URL"
    }

    host "10.3.3.3" {
      hostnames = ["allowed", "allowed.local"]
    }
  }
}
EOF

plan 5

# Test 1: HTTP server is accessible
diag "Test 1: HTTP server check"
HTTP_RESULT=$(curl -s "http://127.0.0.1:$BLOCKLIST_PORT/blocklist.txt" 2>&1 || true)
if echo "$HTTP_RESULT" | grep -q "blocked-domain.com"; then
    pass "HTTP server serving blocklist"
else
    diag "HTTP result: $HTTP_RESULT"
    fail "HTTP server not accessible"
fi

# Test 2: Start control plane
diag "Test 2: Starting Glacic with blocklist URL"
start_ctl "$CONFIG_FILE"
dilated_sleep 5  # Extra time to download blocklist

if [ -n "$CTL_PID" ] && kill -0 $CTL_PID 2>/dev/null; then
    pass "Control plane started"
else
    diag "CTL log:"
    cat "$CTL_LOG" 2>/dev/null | tail -20
    fail "Control plane failed to start"
fi

# Test 3: DNS server is listening
diag "Test 3: DNS server listening"
if wait_for_port 53 5; then
    pass "DNS server listening on port 53"
else
    fail "DNS server not listening"
fi

# Test 4: Blocked domain returns NXDOMAIN
diag "Test 4: Blocked domain query"
BLOCKED_RESULT=$(dig @127.0.0.1 -p 53 blocked-domain.com A +short +timeout=5 2>&1)
if [ -z "$BLOCKED_RESULT" ] || echo "$BLOCKED_RESULT" | grep -qE "^(0\.0\.0\.0|NXDOMAIN)"; then
    pass "Blocked domain returns NXDOMAIN/empty: ${BLOCKED_RESULT:-<empty>}"
else
    diag "Result: $BLOCKED_RESULT"
    fail "Blocked domain not blocked"
fi

# Test 5: Non-blocked local record resolves
diag "Test 5: Non-blocked domain query"
ALLOWED_RESULT=$(dig @127.0.0.1 -p 53 allowed.local A +short +timeout=5 2>&1)
if echo "$ALLOWED_RESULT" | grep -qE "^10\.3\.3\.3$"; then
    pass "Non-blocked domain resolves: allowed.local -> $ALLOWED_RESULT"
else
    diag "Result: $ALLOWED_RESULT"
    fail "Non-blocked domain failed"
fi

# Cleanup
rm -f "$CONFIG_FILE"
rm -rf /tmp/www

diag "DNS Blocklist URL test completed"
