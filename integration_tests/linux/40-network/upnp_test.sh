#!/bin/sh
set -x
#
# UPnP/NAT-PMP Integration Test
# Verifies UPnP service configuration
#

. "$(dirname "$0")/../common.sh"

require_binary

CONFIG_FILE="$(mktemp_compatible upnp.hcl)"

cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"

interface "eth0" {
    ipv4 = ["192.168.1.1/24"]
    zone = "lan"
}

interface "eth1" {
    ipv4 = ["10.0.0.1/24"]
    zone = "wan"
}

zone "lan" {}
zone "wan" {}

upnp {
    enabled = true
    internal_interfaces = ["eth0"]
    external_interface = "eth1"
}
EOF

plan 2

# Test 1: UPnP config parses
diag "Test 1: UPnP config parses"
OUTPUT=$($APP_BIN show "$CONFIG_FILE" 2>&1)
if [ $? -eq 0 ]; then
    pass "UPnP config parses"
else
    diag "Output: $(echo "$OUTPUT" | head -3)"
    fail "UPnP config failed to parse"
fi

# Test 2: UPnP rules generated
diag "Test 2: UPnP rules in output"
OUTPUT=$($APP_BIN show "$CONFIG_FILE" 2>&1)
if echo "$OUTPUT" | grep -qi "upnp\|1900\|miniupnp"; then
    pass "UPnP rules or config present"
else
    diag "No UPnP-related content found in output"
    fail "UPnP rules not generated"
fi

rm -f "$CONFIG_FILE"
diag "UPnP/NAT-PMP test completed"
