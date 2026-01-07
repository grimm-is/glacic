#!/bin/sh
set -x

# nftables Firewall Integration Test Script (TAP format)
# Tests firewall rule creation, chains, and filtering with comprehensive coverage
TEST_TIMEOUT=30

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

# Helper function to get rule handle from JSON output
get_rule_handle() {
    local chain="$1"
    local rule_num="$2"
    nft -j list chain inet test_firewall "$chain" | jq -r ".nftables[] | select(.rule) | .rule.handle" 2>/dev/null | sed -n "${rule_num}p"
}

# Helper function to get rule counter values
get_rule_counter() {
    local chain="$1"
    local handle="$2"
    nft -j list chain inet test_firewall "$chain" | jq -r ".nftables[] | select(.rule) | select(.rule.handle == $handle) | .rule.expr[] | select(.counter) | .counter.packets" 2>/dev/null
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

# Check if jq is available for JSON parsing
if ! command -v jq >/dev/null 2>&1; then
    echo "1..0 # SKIP jq command not found (needed for JSON parsing)"
    exit 0
fi

# --- Start Test Suite ---

plan 50

diag "Starting comprehensive nftables integration tests..."

# Clean up any existing test table
nft delete table inet test_firewall 2>/dev/null
nft delete table ip6 test_firewall_v6 2>/dev/null

# --- Table Creation Tests ---

# 1. Create inet table
diag "Testing table creation..."
nft add table inet test_firewall
ok $? "Can create inet family table"

# 2. Verify table exists in JSON output
nft -j list tables | jq -e '.nftables[] | select(.table) | .table | select(.name == "test_firewall")' >/dev/null
ok $? "Table visible in JSON nft list"

# --- Chain Creation Tests ---

# 3. Create input chain with filter hook
diag "Testing chain creation..."
nft add chain inet test_firewall input '{ type filter hook input priority 0; policy accept; }'
ok $? "Can create input filter chain"

# 4. Create forward chain
nft add chain inet test_firewall forward '{ type filter hook forward priority 0; policy accept; }'
ok $? "Can create forward filter chain"

# 5. Create output chain
nft add chain inet test_firewall output '{ type filter hook output priority 0; policy accept; }'
ok $? "Can create output filter chain"

# 6. Create prerouting chain (for NAT)
nft add chain inet test_firewall prerouting '{ type nat hook prerouting priority -100; }'
ok $? "Can create prerouting NAT chain"

# 7. Create postrouting chain (for masquerade)
nft add chain inet test_firewall postrouting '{ type nat hook postrouting priority 100; }'
ok $? "Can create postrouting NAT chain"

# 8. Create custom chain (no hook)
nft add chain inet test_firewall custom_rules
ok $? "Can create custom chain without hook"

# --- Chain Policy Inspection Tests ---

# 9. Verify input chain policy
nft -j list chain inet test_firewall input | jq -e '.nftables[] | select(.chain) | .chain.policy == "accept"' >/dev/null
ok $? "Input chain has accept policy"

# 10. Verify forward chain policy
nft -j list chain inet test_firewall forward | jq -e '.nftables[] | select(.chain) | .chain.policy == "accept"' >/dev/null
ok $? "Forward chain has accept policy"

# 11. Verify chain hook priority
nft -j list chain inet test_firewall input | jq -e '.nftables[] | select(.chain) | .chain.hook == "input"' >/dev/null
ok $? "Input chain has correct hook"

# --- Basic Rule Tests ---

# 12. Add accept rule for established connections
diag "Testing rule creation..."
nft add rule inet test_firewall input ct state established,related accept
ok $? "Can add conntrack state rule"

# 13. Add drop rule for invalid packets
nft add rule inet test_firewall input ct state invalid drop
ok $? "Can add invalid packet drop rule"

# 14. Add ICMP accept rule
nft add rule inet test_firewall input ip protocol icmp accept
ok $? "Can add ICMP accept rule"

# 15. Add TCP port rule
nft add rule inet test_firewall input tcp dport 22 accept
ok $? "Can add TCP port 22 (SSH) accept rule"

# 16. Add UDP port rule
nft add rule inet test_firewall input udp dport 53 accept
ok $? "Can add UDP port 53 (DNS) accept rule"

# 17. Add multi-port rule
nft add rule inet test_firewall input tcp dport '{ 80, 443 }' accept
ok $? "Can add multi-port rule (80, 443)"

# --- Rule Ordering Tests ---

# 18. Add rules in specific order and verify order
diag "Testing rule ordering..."
nft add rule inet test_firewall input tcp dport 8080 accept
nft add rule inet test_firewall input tcp dport 9090 accept
RULE1_HANDLE=$(get_rule_handle "input" 6)
RULE2_HANDLE=$(get_rule_handle "input" 7)
[ -n "$RULE1_HANDLE" ] && [ -n "$RULE2_HANDLE" ]
ok $? "Can extract rule handles from JSON output"

# 19. Insert rule at specific position
nft insert rule inet test_firewall input position 0 tcp dport 9999 accept
INSERTED_HANDLE=$(get_rule_handle "input" 0)
[ -n "$INSERTED_HANDLE" ]
ok $? "Can insert rule at chain position 0"

# 20. Verify rule count after insertion
RULE_COUNT=$(nft -j list chain inet test_firewall input | jq '[.nftables[] | select(.rule) | .rule] | length' 2>/dev/null)
[ "$RULE_COUNT" -eq 9 ]
ok $? "Chain has correct rule count after insertion ($RULE_COUNT rules)"

# --- Advanced Rule Tests ---

# 21. Add rate limiting rule
diag "Testing advanced rules..."
nft add rule inet test_firewall input tcp dport 22 limit rate 5/minute accept
ok $? "Can add rate-limited rule"

# 22. Add logging rule with prefix
nft add rule inet test_firewall input tcp dport 23 log prefix '"TELNET_ATTEMPT: "' drop
ok $? "Can add logging rule with prefix"

# 23. Add counter rule
nft add rule inet test_firewall input tcp dport 25 counter accept
ok $? "Can add rule with counter"

# 24. Verify counter exists in JSON output
# Find the rule with port 25 (the counter rule we just added)
COUNTER_EXISTS=$(nft -j list chain inet test_firewall input | jq -e '.nftables[] | select(.rule) | .rule.expr[] | select(.counter)' 2>/dev/null)
[ -n "$COUNTER_EXISTS" ]
ok $? "Counter is accessible in JSON output"

# 25. Add source IP rule
nft add rule inet test_firewall input ip saddr 10.0.0.0/8 accept
ok $? "Can add source IP CIDR rule"

# 26. Add destination IP rule
nft add rule inet test_firewall forward ip daddr 192.168.1.0/24 accept
ok $? "Can add destination IP CIDR rule"

# 27. Add logical OR rule
nft add rule inet test_firewall input ip saddr { 192.168.1.0/24, 10.0.0.0/8 } accept
ok $? "Can add logical OR rule with multiple CIDRs"

# 28. Add concatenated expression (interface + IP)
nft add rule inet test_firewall forward iifname "eth0" ip daddr 192.168.2.0/24 accept
ok $? "Can add concatenated expression (interface + IP)"

# --- Set Tests with Different Types ---

# 29. Create IPv4 address set with interval flag
diag "Testing advanced sets..."
nft add set inet test_firewall ipv4_whitelist '{ type ipv4_addr; flags interval; }'
ok $? "Can create IPv4 set with interval flag"

# 30. Add CIDR ranges to interval set
nft add element inet test_firewall ipv4_whitelist '{ 10.0.0.0/8, 192.168.0.0/16, 172.16.0.0/12 }'
ok $? "Can add CIDR ranges to interval set"

# 31. Create IPv6 address set
nft add set inet test_firewall ipv6_whitelist '{ type ipv6_addr; }'
ok $? "Can create IPv6 address set"

# 32. Add IPv6 addresses to set
nft add element inet test_firewall ipv6_whitelist '{ 2001:db8::1, 2001:db8::2 }'
ok $? "Can add IPv6 addresses to set"

# 33. Create port set
nft add set inet test_firewall allowed_ports '{ type inet_service; }'
ok $? "Can create port service set"

# 34. Add ports to service set
nft add element inet test_firewall allowed_ports '{ 22, 80, 443, 8080 }'
ok $? "Can add ports to service set"

# 35. Create set with timeout
nft add set inet test_firewall temp_blocked '{ type ipv4_addr; timeout 1h; }'
ok $? "Can create set with timeout flag"

# 36. Use set in rule with match
nft add rule inet test_firewall input ip saddr @ipv4_whitelist accept
ok $? "Can reference IPv4 set in rule"

# 37. Use port set in rule
nft add rule inet test_firewall input tcp dport @allowed_ports accept
ok $? "Can reference port set in rule"

# 38. Verify set element count
SET_COUNT=$(nft -j list set inet test_firewall ipv4_whitelist | jq '.nftables[] | select(.set) | .set.elem | length' 2>/dev/null)
[ "$SET_COUNT" -eq 3 ]
ok $? "Set has correct element count ($SET_COUNT elements)"

# --- IPv6 Specific Tests ---

# 39. Create IPv6 table
diag "Testing IPv6 rules..."
nft add table ip6 test_firewall_v6
ok $? "Can create IPv6 table"

# 40. Create IPv6 chain
nft add chain ip6 test_firewall_v6 input '{ type filter hook input priority 0; }'
ok $? "Can create IPv6 input chain"

# 41. Add IPv6 address rule
nft add rule ip6 test_firewall_v6 input ip6 saddr 2001:db8::/32 accept
ok $? "Can add IPv6 source address rule"

# 42. Add IPv6 ICMP rule
nft add rule ip6 test_firewall_v6 input ip6 nexthdr icmpv6 accept
ok $? "Can add IPv6 ICMPv6 accept rule"

# 43. Add IPv6 destination rule
nft add rule ip6 test_firewall_v6 input ip6 daddr 2001:db8::1 accept
ok $? "Can add IPv6 destination address rule"

# --- NAT Rule Tests ---

# 44. Add masquerade rule
diag "Testing NAT rules..."
nft add rule inet test_firewall postrouting oifname "eth0" masquerade
ok $? "Can add masquerade rule"

# 45. Add DNAT rule (port forward)
nft add rule inet test_firewall prerouting tcp dport 8080 dnat ip to 192.168.1.100:80
ok $? "Can add DNAT port forward rule"

# 46. Add SNAT rule with specific address
nft add rule inet test_firewall postrouting ip saddr 192.168.1.0/24 snat ip to 203.0.113.1
ok $? "Can add SNAT rule with specific address"

# --- Error Handling Tests ---

# 47. Test invalid rule syntax (should fail)
diag "Testing error handling..."
nft add rule inet test_firewall input invalid_syntax 2>/dev/null
[ $? -ne 0 ]
ok $? "Invalid rule syntax correctly rejected"

# 48. Test invalid set element type
nft add element inet test_firewall ipv4_whitelist '{ "invalid_element" }' 2>/dev/null
[ $? -ne 0 ]
ok $? "Invalid set element type correctly rejected"

# --- Verification Tests ---

# 49. List full ruleset in JSON
diag "Testing comprehensive verification..."
nft -j list table inet test_firewall >/dev/null 2>&1
ok $? "Can list table ruleset in JSON format"

# 50. Verify total rule count across all chains
TOTAL_RULES=$(nft -j list table inet test_firewall | jq '[.nftables[] | select(.rule) | .rule] | length' 2>/dev/null)
[ "$TOTAL_RULES" -ge 20 ]
ok $? "Table has expected minimum rule count ($TOTAL_RULES rules)"

# --- Cleanup Tests ---

# Clean up all test tables
diag "Testing cleanup..."
nft delete table inet test_firewall
nft delete table ip6 test_firewall_v6

# --- End Test Suite ---

diag ""
diag "nftables test summary:"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count nftables tests passed!"
    exit 0
else
    diag "$failed_count of $test_count nftables tests failed."
    exit 1
fi
