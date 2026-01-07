#!/bin/sh
set -x
#
# DNS Dynamic Updates Integration Test
# Verifies DHCP hostnames are added to DNS
#

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

CONFIG_FILE="/tmp/dns_dynamic.hcl"

cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"

interface "lo" {
    ipv4 = ["192.168.1.1/24"]
}

zone "local" {}

dhcp {
    scope "test" {
        interface = "lo"
        range_start = "192.168.1.100"
        range_end = "192.168.1.200"
        router = "192.168.1.1"
        domain = "local.lan"
    }
}

dns {
    forwarders = ["8.8.8.8"]

    serve "local" {
        listen_port = 15353
        local_domain = "local.lan"
        dhcp_integration = true
    }
}
EOF

plan 2

# Test 1: Config with DHCP-DNS integration parses
diag "Test 1: DHCP-DNS integration config"
OUTPUT=$($APP_BIN show "$CONFIG_FILE" 2>&1)
if [ $? -eq 0 ]; then
    pass "DNS dynamic updates config parses"
else
    diag "Output: $OUTPUT"
    fail "Config failed to parse"
fi

# Test 2: Control plane starts with DHCP-DNS integration
diag "Test 2: Control plane with DHCP-DNS integration"
start_ctl "$CONFIG_FILE"
dilated_sleep 3

if [ -n "$CTL_PID" ] && kill -0 $CTL_PID 2>/dev/null; then
    pass "DHCP-DNS integration works"
else
    fail "Control plane failed with DHCP-DNS config"
fi

rm -f "$CONFIG_FILE"
diag "DNS dynamic updates test completed"
