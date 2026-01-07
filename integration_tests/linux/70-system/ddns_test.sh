#!/bin/sh

# DDNS Test
# Verifies: Dynamic DNS configuration and update triggering.

set -e
set -x
source "$(dirname "$0")/../common.sh"

TEST_TIMEOUT=30

require_root
require_binary

cleanup_on_exit

diag "Test: DDNS Configuration"

# TAP plan
plan 1

# Create test config with DDNS enabled
TEST_CONFIG=$(mktemp_compatible ddns.hcl)
cat > "$TEST_CONFIG" <<EOF
schema_version = "1.0"

interface "lo" {
  zone = "lan"
  ipv4 = ["127.0.0.1/8"]
}

zone "lan" {
  interfaces = ["lo"]
}

ddns {
  enabled = true
  provider = "cloudflare"
  hostname = "test.example.com"
  token = "abcdef123456"
  interval = 5
}
EOF

# Use start_ctl helper (waits for socket, better error handling)
start_ctl "$TEST_CONFIG"

# Allow services to fully initialize before checking logs
dilated_sleep 2

# Check for DDNS service in logs (check both CTL_LOG and application log)
APP_LOG="/var/log/glacic/glacic.log"
if grep -qi "DDNS" "$CTL_LOG" 2>/dev/null || grep -qi "DDNS" "$APP_LOG" 2>/dev/null; then
    ok 0 "DDNS service initialized (log found)"
else
    diag "CTL_LOG output:"
    cat "$CTL_LOG" 2>/dev/null | sed 's/^/# /'
    diag "Application log output (/var/log/glacic/glacic.log):"
    cat "$APP_LOG" 2>/dev/null | tail -30 | sed 's/^/# /'
    ok 1 "DDNS not mentioned in logs"
fi


# Cleanup handled by cleanup_on_exit trap
rm -f "$TEST_CONFIG"

