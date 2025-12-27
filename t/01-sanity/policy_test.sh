#!/bin/sh
set -x

# Zone-to-Zone Policy Integration Test Script (TAP format)
# Tests firewall policy chains, jump rules, and zone isolation

test_count=0
failed_count=0

plan() {
    echo "1..$1"
}

ok() {
    test_count=$((test_count + 1))
    if [ "$1" -eq 0 ]; then
        echo "ok $test_count - $2"
    else
        echo "not ok $test_count - $2"
        failed_count=$((failed_count + 1))
    fi
}

diag() {
    echo "# $1"
}

skip() {
    test_count=$((test_count + 1))
    echo "ok $test_count - # SKIP $1"
}

# Check if we're root
if [ "$(id -u)" -ne 0 ]; then
    echo "1..0 # SKIP Must run as root"
    exit 0
fi

# Check if nft is available
if ! command -v nft >/dev/null 2>&1; then
    echo "1..0 # SKIP nft command not found"
    exit 0
fi

# --- Start Test Suite ---

plan 24

diag "Starting zone-to-zone policy tests..."

# Clean up any existing test table
nft delete table inet test_firewall 2>/dev/null

# --- Setup base firewall structure ---

nft add table inet test_firewall

# ============================================
# Testing base chain creation
# ============================================
diag "Testing base chain creation..."

# 1. Create input chain
nft add chain inet test_firewall input '{ type filter hook input priority 0; policy drop; }'
ok $? "Can create input chain with drop policy"

# 2. Create forward chain
nft add chain inet test_firewall forward '{ type filter hook forward priority 0; policy drop; }'
ok $? "Can create forward chain with drop policy"

# 3. Create output chain
nft add chain inet test_firewall output '{ type filter hook output priority 0; policy accept; }'
ok $? "Can create output chain with accept policy"

# ============================================
# Testing conntrack base rules
# ============================================
diag "Testing conntrack base rules..."

# 4. Add established/related accept rule
nft add rule inet test_firewall input ct state established,related accept
ok $? "Can add established/related accept rule (input)"

# 5. Add established/related for forward
nft add rule inet test_firewall forward ct state established,related accept
ok $? "Can add established/related accept rule (forward)"

# 6. Add invalid drop rule
nft add rule inet test_firewall input ct state invalid drop
ok $? "Can add invalid state drop rule"

# ============================================
# Testing policy chains (zone-to-zone)
# ============================================
diag "Testing policy chain creation..."

# 7. Create LAN to WAN policy chain
nft add chain inet test_firewall policy_lan_wan
ok $? "Can create policy_lan_wan chain"

# 8. Create WAN to LAN policy chain
nft add chain inet test_firewall policy_wan_lan
ok $? "Can create policy_wan_lan chain"

# 9. Create LAN to Firewall policy chain
nft add chain inet test_firewall policy_lan_fw
ok $? "Can create policy_lan_fw chain"

# 10. Create DMZ to WAN policy chain
nft add chain inet test_firewall policy_dmz_wan
ok $? "Can create policy_dmz_wan chain"

# ============================================
# Testing policy rules
# ============================================
diag "Testing policy rules..."

# 11. Add allow-all rule to LAN->WAN (outbound)
nft add rule inet test_firewall policy_lan_wan accept
ok $? "Can add accept rule to policy chain"

# 12. Add SSH allow rule with logging
nft add rule inet test_firewall policy_wan_lan tcp dport 22 log prefix '"SSH-WAN: "' accept
ok $? "Can add SSH allow rule with logging"

# 13. Add HTTP/HTTPS rules
nft add rule inet test_firewall policy_lan_fw tcp dport '{ 80, 443 }' accept
ok $? "Can add multi-port rule to policy chain"

# 14. Add rate-limited rule
nft add rule inet test_firewall policy_wan_lan icmp type echo-request limit rate 5/second accept
ok $? "Can add rate-limited ICMP rule"

# 15. Add counter rule
nft add rule inet test_firewall policy_dmz_wan counter accept
ok $? "Can add rule with counter"

# ============================================
# Testing jump rules (interface-based dispatch)
# ============================================
diag "Testing jump rules..."

# 16. Add jump rule for LAN->WAN in forward chain
nft add rule inet test_firewall forward iifname "eth1" oifname "eth0" jump policy_lan_wan
ok $? "Can add iif/oif jump rule (LAN->WAN)"

# 17. Add jump rule for WAN->LAN
nft add rule inet test_firewall forward iifname "eth0" oifname "eth1" jump policy_wan_lan
ok $? "Can add iif/oif jump rule (WAN->LAN)"

# 18. Add jump rule for input (LAN->Firewall)
nft add rule inet test_firewall input iifname "eth1" jump policy_lan_fw
ok $? "Can add iif jump rule for input (LAN->FW)"

# 19. Verify jump rules exist
jump_count=$(nft list chain inet test_firewall forward | grep -c "jump policy_")
[ "$jump_count" -ge 2 ]
ok $? "Jump rules visible in forward chain ($jump_count rules)"

# ============================================
# Testing IPSet integration in policies
# ============================================
diag "Testing IPSet in policies..."

# 20. Create a test IPSet
nft add set inet test_firewall blocked_ips '{ type ipv4_addr; flags interval; }'
nft add element inet test_firewall blocked_ips '{ 192.0.2.0/24, 198.51.100.0/24 }'
ok $? "Can create and populate IPSet for policies"

# 21. Add rule using IPSet
nft add rule inet test_firewall policy_wan_lan ip saddr @blocked_ips drop
ok $? "Can add policy rule referencing IPSet"

# 22. Verify IPSet rule
nft list chain inet test_firewall policy_wan_lan | grep -q "@blocked_ips"
ok $? "IPSet rule visible in policy chain"

# ============================================
# Testing default drop behavior
# ============================================
diag "Testing default drop..."

# 23. Add explicit drop at end of forward chain
nft add rule inet test_firewall forward counter drop
ok $? "Can add final drop rule with counter"

# ============================================
# Cleanup
# ============================================
diag "Cleaning up..."

# 24. Delete test table
nft delete table inet test_firewall
ok $? "Can delete policy test table"

# --- End Test Suite ---

diag ""
diag "Policy test summary:"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count policy tests passed!"
    exit 0
else
    diag "$failed_count of $test_count policy tests failed."
    exit 1
fi
