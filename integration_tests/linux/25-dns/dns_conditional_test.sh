#!/bin/sh
set -x
#
# Conditional DNS Forwarding Integration Test
# Verifies conditional DNS forwarding config works and DNS server starts
#

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

if ! command -v dig >/dev/null 2>&1; then
    echo "1..0 # SKIP dig command not found"
    exit 0
fi

CONFIG_FILE="/tmp/dns_conditional.hcl"

# Create test config with conditional forwarding
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

    # Conditional forwarding for internal domain
    conditional_forward "internal.corp" {
        servers = ["10.0.0.1"]
    }

    conditional_forward "home.arpa" {
        servers = ["192.168.1.1"]
    }

    serve "local" {
        local_domain = "local"

        host "10.1.1.1" {
            hostnames = ["test", "test.local"]
        }
    }
}
EOF

plan 4

# Test 1: Config with conditional forwarding parses
diag "Test 1: Conditional forwarding config parses"
OUTPUT=$($APP_BIN show "$CONFIG_FILE" 2>&1)
if [ $? -eq 0 ]; then
    pass "Conditional DNS forwarding config parses"
else
    diag "Output: $OUTPUT"
    fail "Conditional forwarding config failed to parse"
fi

# Test 2: Control plane starts with conditional forwarding
diag "Test 2: Control plane starts"
start_ctl "$CONFIG_FILE"
dilated_sleep 3

if [ -n "$CTL_PID" ] && kill -0 $CTL_PID 2>/dev/null; then
    pass "Control plane runs with conditional DNS forwarding"
else
    diag "CTL log:"
    cat "$CTL_LOG" 2>/dev/null | tail -10
    fail "Control plane crashed with conditional forwarding config"
fi

# Test 3: DNS server is listening on port 53
diag "Test 3: DNS server listening"
if wait_for_port 53 5; then
    pass "DNS server listening on port 53"
else
    fail "DNS server not listening on port 53"
fi

# Test 4: DNS server responds to local record
diag "Test 4: DNS query for local record"
RESULT=$(dig @127.0.0.1 -p 53 test.local A +short +timeout=5 2>&1)
if echo "$RESULT" | grep -qE "^10\.1\.1\.1$"; then
    pass "Local DNS query succeeded: test.local -> $RESULT"
else
    diag "Query result: $RESULT"
    fail "Local DNS query failed"
fi

rm -f "$CONFIG_FILE"
diag "Conditional DNS forwarding test completed"
