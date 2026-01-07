#!/bin/sh
set -x

# NAT Integration Test Script (TAP format)
# Tests SNAT, DNAT, masquerade, and port forwarding

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

plan 18

diag "Starting NAT integration tests..."

# Clean up any existing test table
nft delete table ip test_nat 2>/dev/null

# --- Setup ---

nft add table ip test_nat

# ============================================
# Testing NAT chain creation
# ============================================
diag "Testing NAT chain creation..."

# 1. Create prerouting chain (for DNAT)
nft add chain ip test_nat prerouting '{ type nat hook prerouting priority -100; policy accept; }'
ok $? "Can create prerouting NAT chain"

# 2. Create postrouting chain (for SNAT/masquerade)
nft add chain ip test_nat postrouting '{ type nat hook postrouting priority 100; policy accept; }'
ok $? "Can create postrouting NAT chain"

# 3. Verify chains exist
nft list table ip test_nat | grep -q "prerouting"
ok $? "Prerouting chain visible"

nft list table ip test_nat | grep -q "postrouting"
ok $? "Postrouting chain visible"

# ============================================
# Testing Masquerade (dynamic SNAT)
# ============================================
diag "Testing masquerade..."

# 5. Add masquerade rule for outbound interface
nft add rule ip test_nat postrouting oifname "eth0" masquerade
ok $? "Can add masquerade rule"

# 6. Verify masquerade rule exists
nft list chain ip test_nat postrouting | grep -q "masquerade"
ok $? "Masquerade rule visible"

# ============================================
# Testing SNAT (static source NAT)
# ============================================
diag "Testing SNAT..."

# 7. Add SNAT rule with specific IP
nft add rule ip test_nat postrouting ip saddr 192.168.1.0/24 oifname "eth0" snat to 203.0.113.1
ok $? "Can add SNAT rule with specific address"

# 8. Add SNAT rule with IP range
nft add rule ip test_nat postrouting ip saddr 10.0.0.0/8 oifname "eth0" snat to 203.0.113.10-203.0.113.20
ok $? "Can add SNAT rule with address range"

# 9. Verify SNAT rules
snat_count=$(nft list chain ip test_nat postrouting | grep -c "snat to")
[ "$snat_count" -ge 2 ]
ok $? "SNAT rules visible ($snat_count rules)"

# ============================================
# TEST_TIMEOUT=30
# Testing DNAT (destination NAT / port forwarding)
# ============================================
diag "Testing DNAT (port forwarding)..."

# 10. Add DNAT rule for port forward (HTTP -> internal server)
nft add rule ip test_nat prerouting iifname "eth0" tcp dport 80 dnat to 192.168.1.100
ok $? "Can add DNAT port forward (80 -> 192.168.1.100)"

# 11. Add DNAT rule with port translation
nft add rule ip test_nat prerouting iifname "eth0" tcp dport 8080 dnat to 192.168.1.100:80
ok $? "Can add DNAT with port translation (8080 -> 192.168.1.100:80)"

# 12. Add DNAT rule for UDP (DNS)
nft add rule ip test_nat prerouting iifname "eth0" udp dport 53 dnat to 192.168.1.10
ok $? "Can add DNAT for UDP (DNS)"

# 13. Add DNAT rule for port range
nft add rule ip test_nat prerouting iifname "eth0" tcp dport 3000-3010 dnat to 192.168.1.50
ok $? "Can add DNAT for port range"

# 14. Verify DNAT rules
dnat_count=$(nft list chain ip test_nat prerouting | grep -c "dnat to")
[ "$dnat_count" -ge 4 ]
ok $? "DNAT rules visible ($dnat_count rules)"

# ============================================
# Testing Redirect (local port redirect)
# ============================================
diag "Testing redirect..."

# 15. Add redirect rule (transparent proxy)
nft add rule ip test_nat prerouting tcp dport 80 redirect to :3128
ok $? "Can add redirect rule (transparent proxy)"

# 16. Verify redirect rule
nft list chain ip test_nat prerouting | grep -q "redirect to"
ok $? "Redirect rule visible"

# ============================================
# Testing NAT with connection tracking
# ============================================
diag "Testing NAT with conntrack..."

# 17. Verify conntrack is loaded (required for NAT)
if [ -f /proc/net/nf_conntrack ] || lsmod | grep -q nf_conntrack; then
    ok 0 "Connection tracking available for NAT"
else
    ok 1 "Connection tracking not available"
fi

# ============================================
# Cleanup
# ============================================
diag "Cleaning up..."

# 18. Delete test table
nft delete table ip test_nat
ok $? "Can delete NAT test table"

# --- End Test Suite ---

diag ""
diag "NAT test summary:"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count NAT tests passed!"
    exit 0
else
    diag "$failed_count of $test_count NAT tests failed."
    exit 1
fi
