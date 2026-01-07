#!/bin/sh
set -x
#
# DNS Blocklists (File) Integration Test
# Verifies local hosts-format blocklist files work
#

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

if ! command -v dig >/dev/null 2>&1; then
    echo "1..0 # SKIP dig command not found"
    exit 0
fi

CONFIG_FILE="/tmp/dns_blocklist_file.hcl"
BLOCKLIST_FILE="/tmp/test_blocklist.txt"

# Create a test blocklist file
cat > "$BLOCKLIST_FILE" << 'EOF'
# Test blocklist
0.0.0.0 ads.example.com
0.0.0.0 tracking.example.com
127.0.0.1 malware.example.com
EOF

# Create test config with zone services and blocklist
cat > "$CONFIG_FILE" <<EOF
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

        blocklist "test-ads" {
            file = "$BLOCKLIST_FILE"
        }

        host "10.2.2.2" {
            hostnames = ["allowed", "allowed.local"]
        }
    }
}
EOF

plan 5

# Test 1: Config with file blocklist parses
diag "Test 1: Blocklist file config parses"
OUTPUT=$($APP_BIN show "$CONFIG_FILE" 2>&1)
if [ $? -eq 0 ]; then
    pass "DNS blocklist file config parses"
else
    diag "Output: $OUTPUT"
    fail "DNS blocklist file config failed to parse"
fi

# Test 2: Control plane starts with blocklist config
diag "Test 2: Control plane starts with blocklist"
start_ctl "$CONFIG_FILE"
dilated_sleep 3

if [ -n "$CTL_PID" ] && kill -0 $CTL_PID 2>/dev/null; then
    pass "Control plane runs with DNS blocklist file"
else
    diag "CTL log:"
    cat "$CTL_LOG" 2>/dev/null | tail -10
    fail "Control plane crashed with blocklist config"
fi

# Test 3: DNS server is listening on port 53
diag "Test 3: DNS server listening"
if wait_for_port 53 5; then
    pass "DNS server listening on port 53"
else
    fail "DNS server not listening on port 53"
fi

# Test 4: Blocked domain returns NXDOMAIN or empty
diag "Test 4: Blocked domain query"
BLOCKED_RESULT=$(dig @127.0.0.1 -p 53 ads.example.com A +short +timeout=5 2>&1)
if echo "$BLOCKED_RESULT" | grep -qE "^(0\.0\.0\.0|127\.0\.0\.1|NXDOMAIN|REFUSED)$" || [ -z "$BLOCKED_RESULT" ]; then
    pass "Blocked domain returns sinkhole/empty: ${BLOCKED_RESULT:-<empty>}"
else
    diag "Blocked domain returned: $BLOCKED_RESULT (expected 0.0.0.0 or empty)"
    fail "Blocked domain not blocked"
fi

# Test 5: Non-blocked local record resolves
diag "Test 5: Non-blocked domain query"
NORMAL_RESULT=$(dig @127.0.0.1 -p 53 allowed.local A +short +timeout=5 2>&1)
if echo "$NORMAL_RESULT" | grep -qE "^10\.2\.2\.2$"; then
    pass "Non-blocked domain resolves: allowed.local -> $NORMAL_RESULT"
else
    diag "Query result: $NORMAL_RESULT"
    fail "Non-blocked domain failed to resolve"
fi

rm -f "$CONFIG_FILE" "$BLOCKLIST_FILE"
diag "DNS blocklist file test completed"
