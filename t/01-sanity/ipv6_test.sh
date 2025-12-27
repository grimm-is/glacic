#!/bin/sh
set -x

# IPv6 Integration Test Script (TAP format)
# Tests IPv6 networking, nftables rules, and routing

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

# Check for IPv6 support
if [ ! -d /proc/sys/net/ipv6 ]; then
    echo "1..0 # SKIP IPv6 not enabled in kernel"
    exit 0
fi

# --- Start Test Suite ---

plan 22

diag "Starting IPv6 integration tests..."

# ============================================
# Testing IPv6 kernel support
# ============================================
diag "Testing IPv6 kernel support..."

# 1. Check IPv6 module/support
if [ -d /proc/sys/net/ipv6 ]; then
    ok 0 "IPv6 enabled in kernel"
else
    ok 1 "IPv6 not enabled"
fi

# 2. Check IPv6 forwarding setting
if [ -f /proc/sys/net/ipv6/conf/all/forwarding ]; then
    ok 0 "IPv6 forwarding sysctl exists"
else
    ok 1 "IPv6 forwarding sysctl missing"
fi

# 3. Read IPv6 forwarding state
ipv6_fwd=$(cat /proc/sys/net/ipv6/conf/all/forwarding 2>/dev/null)
ok $? "Can read IPv6 forwarding state ($ipv6_fwd)"

# ============================================
# Testing IPv6 address configuration
# ============================================
diag "Testing IPv6 address configuration..."

# Create dummy interface for testing
ip link add dummy6 type dummy 2>/dev/null || true
ip link set dummy6 up

# 4. Add IPv6 address
ip -6 addr add 2001:db8::1/64 dev dummy6 2>/dev/null
ok $? "Can add IPv6 address"

# 5. Verify IPv6 address
ip -6 addr show dummy6 | grep -q "2001:db8::1"
ok $? "IPv6 address visible on interface"

# 6. Add link-local address (usually auto-configured)
ip -6 addr show dummy6 | grep -q "fe80::"
ok $? "Link-local IPv6 address present"

# 7. Add secondary IPv6 address
ip -6 addr add 2001:db8::2/64 dev dummy6 2>/dev/null
ok $? "Can add secondary IPv6 address"

# ============================================
# Testing IPv6 routing
# ============================================
diag "Testing IPv6 routing..."

# 8. Add IPv6 route
ip -6 route add 2001:db8:1::/48 dev dummy6 2>/dev/null
ok $? "Can add IPv6 route"

# 9. View IPv6 routing table
route_count=$(ip -6 route show | wc -l)
[ "$route_count" -ge 1 ]
ok $? "Can view IPv6 routing table ($route_count routes)"

# 10. Delete IPv6 route
ip -6 route del 2001:db8:1::/48 dev dummy6 2>/dev/null
ok $? "Can delete IPv6 route"

# ============================================
# Testing IPv6 nftables
# ============================================
diag "Testing IPv6 nftables..."

# Clean up any existing test table
nft delete table ip6 test_ipv6 2>/dev/null
nft delete table inet test_inet6 2>/dev/null

# 11. Create IPv6-only table
nft add table ip6 test_ipv6
ok $? "Can create ip6 family table"

# 12. Create inet (dual-stack) table
nft add table inet test_inet6
ok $? "Can create inet family table"

# 13. Add IPv6 filter chain
nft add chain ip6 test_ipv6 input '{ type filter hook input priority 0; }'
ok $? "Can create IPv6 filter chain"

# 14. Add IPv6 source address rule
nft add rule ip6 test_ipv6 input ip6 saddr 2001:db8::/32 accept
ok $? "Can add IPv6 source address rule"

# 15. Add IPv6 destination address rule
nft add rule ip6 test_ipv6 input ip6 daddr ::1 accept
ok $? "Can add IPv6 destination rule (loopback)"

# 16. Add ICMPv6 rule
nft add rule ip6 test_ipv6 input icmpv6 type echo-request accept
ok $? "Can add ICMPv6 rule"

# ============================================
# Testing IPv6 NAT (NAT66)
# ============================================
diag "Testing IPv6 NAT..."

# 17. Create NAT66 chain
nft add chain ip6 test_ipv6 postrouting '{ type nat hook postrouting priority 100; }'
ok $? "Can create IPv6 NAT chain"

# 18. Add IPv6 masquerade (if supported)
nft add rule ip6 test_ipv6 postrouting oifname "dummy6" masquerade 2>/dev/null
if [ $? -eq 0 ]; then
    ok 0 "Can add IPv6 masquerade rule"
else
    ok 0 "IPv6 masquerade not supported # SKIP"
fi

# ============================================
# Testing IPv6 sets
# ============================================
diag "Testing IPv6 sets..."

# 19. Create IPv6 address set
nft add set inet test_inet6 blocked6 '{ type ipv6_addr; flags interval; }'
ok $? "Can create IPv6 address set"

# 20. Add IPv6 addresses to set
nft add element inet test_inet6 blocked6 '{ 2001:db8:bad::/48, ::ffff:192.0.2.0/120 }'
ok $? "Can add IPv6 addresses to set"

# 21. Use set in rule
nft add chain inet test_inet6 input '{ type filter hook input priority 0; }'
nft add rule inet test_inet6 input ip6 saddr @blocked6 drop
ok $? "Can use IPv6 set in rule"

# ============================================
# Cleanup
# ============================================
diag "Cleaning up..."

# 22. Delete test tables and interface
nft delete table ip6 test_ipv6 2>/dev/null
nft delete table inet test_inet6 2>/dev/null
ip link del dummy6 2>/dev/null
ok $? "Cleaned up test resources"

# --- End Test Suite ---

diag ""
diag "IPv6 test summary:"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count IPv6 tests passed!"
    exit 0
else
    diag "$failed_count of $test_count IPv6 tests failed."
    exit 1
fi
