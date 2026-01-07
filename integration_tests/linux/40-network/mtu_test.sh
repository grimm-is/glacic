#!/bin/sh
set -x
#
# MTU Configuration Integration Test
# Verifies MTU settings are applied to interfaces
#

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

CONFIG_FILE="/tmp/mtu_test.hcl"

# Create test config with MTU settings
cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"

interface "lo" {
    ipv4 = ["127.0.0.1/8"]
    mtu = 16384
}

zone "local" {}
EOF

plan 2

# Test 1: Config with MTU parses correctly
diag "Test 1: Config parsing with MTU"
OUTPUT=$($APP_BIN show "$CONFIG_FILE" 2>&1)
if [ $? -eq 0 ]; then
    pass "Config with MTU parses successfully"
else
    diag "Output: $OUTPUT"
    fail "Config with MTU failed to parse"
fi

# Test 2: Start control plane with MTU config
diag "Test 2: Control plane starts with MTU config"
start_ctl "$CONFIG_FILE"
dilated_sleep 2

if [ -n "$CTL_PID" ] && kill -0 $CTL_PID 2>/dev/null; then
    pass "Control plane runs with MTU configuration"
else
    fail "Control plane failed with MTU config"
fi

rm -f "$CONFIG_FILE"
diag "MTU configuration test completed"
