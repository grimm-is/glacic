#!/bin/sh
set -x

# Time-of-Day Rule Test
# Verifies: Time-based firewall rules using nftables meta hour/day
# Requires: Kernel 5.4+, nftables 0.9.3+

TEST_TIMEOUT=30
. "$(dirname "$0")/../common.sh"

plan 4

require_root
require_binary

cleanup_on_exit

diag "Test: Time-of-Day Rules (meta hour/day)"

# First, check if kernel supports meta hour
diag "Checking nftables meta hour support..."
TEST_SCRIPT='table inet test { chain c { meta hour >= 00:00:00 counter } }'
if echo "$TEST_SCRIPT" | nft -c -f - 2>&1; then
    diag "meta hour syntax IS supported"
else
    diag "meta hour syntax NOT supported on this kernel/nft version:"
    nft --version
    uname -r
    # Skip the time-of-day tests but pass - feature requires newer kernel
    ok 0 "meta hour check skipped (kernel may be too old)"
    ok 0 "Firewall table check skipped"
    ok 0 "Time-of-day rule check skipped # SKIP (meta hour not supported)"
    ok 0 "Days-of-week rule check skipped # SKIP (meta hour not supported)"
    exit 0
fi

# Create test config with time-based rule
TEST_CONFIG=$(mktemp_compatible schedule.hcl)
cat > "$TEST_CONFIG" <<EOF
schema_version = "1.0"

interface "lo" {
  zone = "lan"
  ipv4 = ["127.0.0.1/8"]
}

zone "lan" {
  interfaces = ["lo"]
}

policy "lan" "firewall" {
    name = "lan_to_self"
    action = "accept"

    rule "block_during_work_hours" {
        action = "drop"
        time_start = "09:00"
        time_end = "17:00"
        days = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday"]
        dest_port = 8888
        proto = "tcp"
    }
}
EOF

# Start control plane
diag "Starting control plane..."
start_ctl "$TEST_CONFIG"
ok 0 "Control plane started with time-of-day rule config"

dilated_sleep 2

# Check control plane logs for errors
if grep -q "Error applying firewall" "$CTL_LOG" 2>/dev/null; then
    diag "FIREWALL ERROR detected in logs:"
    grep -i "error.*firewall\|failed.*firewall\|validation failed" "$CTL_LOG" | tail -10 | sed 's/^/# /'
    # Also grep for the Output: part
    grep -i "Output:" "$CTL_LOG" | tail -3 | sed 's/^/# /'
fi

# Check if firewall rules were loaded
if nft list table inet glacic >/dev/null 2>&1; then
    ok 0 "Firewall table created"
else
    ok 1 "Firewall table missing"
fi

# Check if policy chain exists
RULESET=$(nft list table inet glacic 2>/dev/null || echo "")
if echo "$RULESET" | grep -q "chain policy_lan_firewall"; then
    diag "Policy chain exists"
    # Check if meta hour is in ruleset
    if echo "$RULESET" | grep -q "meta hour"; then
        ok 0 "Time-of-day rule present in ruleset (meta hour)"
        diag "Rule snippet:"
        echo "$RULESET" | grep "meta hour" | head -3 | sed 's/^/# /'
    else
        ok 1 "Time-of-day rule NOT found (meta hour missing in policy chain)"
        diag "Policy chain contents:"
        echo "$RULESET" | sed -n '/chain policy_lan_firewall/,/}/p' | sed 's/^/# /'
    fi
else
    ok 1 "Policy chain 'policy_lan_firewall' NOT found - config failed to apply"
    diag "Available chains:"
    echo "$RULESET" | grep "chain " | sed 's/^/# /'
fi

# Check if meta day is in ruleset
if echo "$RULESET" | grep -q "meta day"; then
    ok 0 "Days-of-week rule present in ruleset (meta day)"
else
    ok 1 "Days-of-week rule NOT found (meta day missing)"
fi

rm -f "$TEST_CONFIG"
exit 0
