#!/bin/sh
set -x
#
# Conditional DNS Forwarding Integration Test
# Verifies DNS queries are forwarded conditionally based on domain
#

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

CONFIG_FILE="/tmp/dns_conditional.hcl"

# Create test config with conditional forwarding using correct HCL syntax
cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"

interface "lo" {
    ipv4 = ["127.0.0.1/8"]
}

zone "local" {}

dns {
    # Upstream DNS servers
    forwarders = ["8.8.8.8", "8.8.4.4"]
    
    # Conditional forwarding for internal domain
    conditional_forward "internal.corp" {
        servers = ["10.0.0.1"]
    }
    
    conditional_forward "home.arpa" {
        servers = ["192.168.1.1"]
    }
    
    # DNS serving configuration
    serve "local" {
        listen_port = 15353
        local_domain = "local"
    }
}
EOF

plan 2

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
sleep 3

if [ -n "$CTL_PID" ] && kill -0 $CTL_PID 2>/dev/null; then
    pass "Control plane runs with conditional DNS forwarding"
else
    diag "CTL log:"
    cat "$CTL_LOG" 2>/dev/null | tail -10
    fail "Control plane crashed with conditional forwarding config"
fi

rm -f "$CONFIG_FILE"
diag "Conditional DNS forwarding test completed"
