#!/bin/sh
set -x

# Firewall Rules Integration Test (TAP format)
# Tests that applying firewall config produces expected nftables rules
# This is the core value test - does our config translate to correct rules?

# Request longer timeout - test includes socket wait and rule verification
TEST_TIMEOUT=30

# Source common functions
. "$(dirname "$0")/../common.sh"

require_root
require_binary

if ! command -v nft >/dev/null 2>&1; then
    echo "1..0 # SKIP nft command not found"
    exit 0
fi

# --- Test Suite ---
plan 15

diag "Testing firewall rule generation"

# Capture initial nftables state
BEFORE_RULES=$(nft list ruleset 2>/dev/null)

# Test 1: Ensure clean state by deleting any existing glacic table
diag "Checking initial state..."
# Clean up any existing glacic tables from previous test runs
nft delete table inet glacic 2>/dev/null || true
nft delete table ip nat 2>/dev/null || true
# Verify cleanup worked
echo "$(nft list ruleset 2>/dev/null)" | grep -q "table inet glacic"
[ $? -ne 0 ]
ok $? "Clean state - no glacic table exists"

# Create minimal test config
TEST_CONFIG=$(mktemp_compatible "fw_rules.hcl")
cat > "$TEST_CONFIG" << 'EOF'
ip_forwarding = true

zone "lan" {
  interfaces = ["eth1"]
}

zone "wan" {
  interfaces = ["eth0"]
}

interface "eth0" {
  zone = "wan"
  ipv4 = ["10.0.2.15/24"]
}

interface "eth1" {
  zone = "lan"
  ipv4 = ["192.168.1.1/24"]
}

policy "lan" "wan" {
  name = "lan_to_wan"
  action = "accept"
}

policy "wan" "lan" {
  name = "wan_to_lan"
  action = "drop"

  rule "allow_established" {
    conn_state = "established,related"
    action = "accept"
  }

  rule "allow_ssh" {
    dest_port = 22
    proto = "tcp"
    action = "accept"
  }
}

nat "masq" {
  type = "masquerade"
  out_interface = "eth0"
}
EOF

cleanup() {
    rm -f "$TEST_CONFIG"
    # Clean up nftables
    nft delete table inet glacic 2>/dev/null
    # Kill any firewall process we started
    [ -n "$CTL_PID" ] && kill $CTL_PID 2>/dev/null
}
trap cleanup EXIT

# Test 2: Start firewall with test config
diag "Starting firewall with test config..."
$APP_BIN ctl "$TEST_CONFIG" > /tmp/fw_rules_test.log 2>&1 &
CTL_PID=$!

# Wait for socket
count=0
while [ ! -S $CTL_SOCKET ]; do
    dilated_sleep 1
    count=$((count + 1))
    if [ $count -ge 15 ]; then
        diag "Timeout waiting for firewall - log output:"
        cat /tmp/fw_rules_test.log
        ok 1 "Firewall started"
        exit 1
    fi
done
ok 0 "Firewall started successfully"

# Give it a moment to apply rules
dilated_sleep 2

# Capture post-apply state
AFTER_RULES=$(nft list ruleset 2>/dev/null)

# Test 3: glacic table now exists
diag "Checking glacic table was created..."
echo "$AFTER_RULES" | grep -q "table inet glacic"
ok $? "glacic table created"

# Test 4: Forward chain exists
diag "Checking forward chain..."
nft list chain inet glacic forward >/dev/null 2>&1
ok $? "forward chain exists"

# Test 5: Input chain exists
diag "Checking input chain..."
nft list chain inet glacic input >/dev/null 2>&1
ok $? "input chain exists"

# Test 6: Output chain exists
diag "Checking output chain..."
nft list chain inet glacic output >/dev/null 2>&1
ok $? "output chain exists"

# Test 7: Check for established/related rule
diag "Checking established/related rule..."
nft list chain inet glacic forward | grep -E "ct state established,related.*accept"
ok $? "established/related rule present and accepting"

# Test 8: Check for SSH allow rule (port 22)
diag "Checking SSH allow rule..."
nft list ruleset | grep -E "tcp dport 22.*accept"
ok $? "SSH port 22 rule present and accepting"

# Test 9: Check for drop rule (default action for wan_to_lan)
diag "Checking default drop rule..."
# Since it's a default drop policy on the chain, we might not see an explicit rule if implemented via policy on the base chain.
# But our policy engine creates jump chains.
# Let's check the policy_wan_lan chain has a default 'drop' counter rule at the end.
nft list chain inet glacic policy_wan_lan | grep -E "counter.*drop"
ok $? "drop verdict present in policy_wan_lan"

# Test 10: Check for accept rule (default action for lan_to_wan)
diag "Checking default accept rule..."
nft list chain inet glacic policy_lan_wan | grep -E "counter.*accept"
ok $? "accept verdict present in policy_lan_wan"

# Test 11: Verify IP forwarding is enabled
diag "Checking IP forwarding..."
IPV4_FWD=$(cat /proc/sys/net/ipv4/ip_forward)
[ "$IPV4_FWD" = "1" ]
ok $? "IPv4 forwarding enabled"

# Test 12: Verify rule count is reasonable (not empty, not excessive)
diag "Checking rule count..."
RULE_COUNT=$(nft list table inet glacic | grep -c "^[[:space:]]*\(accept\|drop\|reject\|counter\)" || echo 0)
[ "$RULE_COUNT" -gt 0 ] && [ "$RULE_COUNT" -lt 100 ]
ok $? "Rule count is reasonable ($RULE_COUNT rules)"

# Test 13: Check NAT table exists (for masquerade)
# NAT is in a separate 'ip nat' table, not 'inet glacic'
diag "Checking NAT configuration..."
nft list chain ip nat postrouting >/dev/null 2>&1
ok $? "postrouting chain exists for NAT (ip nat table)"

# Test 14: Verify no duplicate tables
diag "Checking for duplicate tables..."
TABLE_COUNT=$(nft list tables | grep -c "table inet glacic$" || echo 0)
    if [ "$TABLE_COUNT" -eq 1 ]; then
        ok 0 "Exactly one glacic table exists"
    else
        ok 1 "Exactly one glacic table exists (Count: $TABLE_COUNT)"
        diag "nft list tables output:"
        nft list tables | sed 's/^/# /'
    fi

# Test 15: Verify chains have correct policy/type
diag "Checking chain types..."
nft list chain inet glacic forward | grep -q "type filter"
ok $? "forward chain is filter type"

# --- Summary ---
diag ""
if [ $failed_count -eq 0 ]; then
    diag "All $test_count tests passed"
    exit 0
else
    diag "$failed_count of $test_count tests failed"
    diag ""
    diag "Current nftables state:"
    nft list table inet glacic 2>/dev/null | head -50
    diag "Application Log:"
    cat /tmp/fw_rules_test.log | sed 's/^/# /'
    exit 1
fi
