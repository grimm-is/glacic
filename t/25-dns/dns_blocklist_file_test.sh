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

CONFIG_FILE="/tmp/dns_blocklist_file.hcl"
BLOCKLIST_FILE="/tmp/test_blocklist.txt"

# Create a test blocklist file
cat > "$BLOCKLIST_FILE" <<EOF
# Test blocklist
0.0.0.0 ads.example.com
0.0.0.0 tracking.example.com
127.0.0.1 malware.example.com
EOF

# Create test config
cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"

interface "lo" {
    ipv4 = ["127.0.0.1/8"]
}

zone "local" {}

dns {
    forwarders = ["8.8.8.8"]
    
    serve "local" {
        listen_port = 15353
        
        blocklist "test-ads" {
            file = "$BLOCKLIST_FILE"
        }
    }
}
EOF

plan 2

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
sleep 3

if [ -n "$CTL_PID" ] && kill -0 $CTL_PID 2>/dev/null; then
    pass "Control plane runs with DNS blocklist file"
else
    diag "CTL log:"
    cat "$CTL_LOG" 2>/dev/null | tail -10
    fail "Control plane crashed with blocklist config"
fi

rm -f "$CONFIG_FILE" "$BLOCKLIST_FILE"
diag "DNS blocklist file test completed"
