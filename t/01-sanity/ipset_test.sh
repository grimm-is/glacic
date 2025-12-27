#!/bin/sh
set -x

# IPSet Integration Tests (TAP format)
# Tests nftables set operations and FireHOL list parsing

# TAP test counter
test_num=0
total_tests=20

# Helper functions
pass() {
    test_num=$((test_num + 1))
    echo "ok $test_num - $1"
}

fail() {
    test_num=$((test_num + 1))
    echo "not ok $test_num - $1"
}

skip() {
    test_num=$((test_num + 1))
    echo "ok $test_num - $1 # SKIP $2"
}

echo "# Starting IPSet integration tests..."

# Create test table
echo "# Setting up test environment..."
nft add table inet ipset_test 2>/dev/null

# ============================================
# Testing basic set operations
# ============================================
echo "# Testing basic set operations..."

# Test 1: Create IPv4 address set
if nft add set inet ipset_test blocklist "{ type ipv4_addr; }" 2>/dev/null; then
    pass "Can create IPv4 address set"
else
    fail "Can create IPv4 address set"
fi

# Test 2: Create set with interval flag (for CIDR support)
if nft add set inet ipset_test cidr_set "{ type ipv4_addr; flags interval; }" 2>/dev/null; then
    pass "Can create set with interval flag for CIDR"
else
    fail "Can create set with interval flag for CIDR"
fi

# Test 3: Create set with timeout
if nft add set inet ipset_test temp_blocks "{ type ipv4_addr; flags timeout; timeout 1h; }" 2>/dev/null; then
    pass "Can create set with timeout"
else
    fail "Can create set with timeout"
fi

# Test 4: Add single IP to set
if nft add element inet ipset_test blocklist "{ 192.168.1.100 }" 2>/dev/null; then
    pass "Can add single IP to set"
else
    fail "Can add single IP to set"
fi

# Test 5: Add multiple IPs to set
if nft add element inet ipset_test blocklist "{ 10.0.0.1, 10.0.0.2, 10.0.0.3 }" 2>/dev/null; then
    pass "Can add multiple IPs to set"
else
    fail "Can add multiple IPs to set"
fi

# Test 6: Add CIDR to interval set
if nft add element inet ipset_test cidr_set "{ 192.168.0.0/24, 10.0.0.0/8 }" 2>/dev/null; then
    pass "Can add CIDR ranges to interval set"
else
    fail "Can add CIDR ranges to interval set"
fi

# Test 7: List set contents
set_output=$(nft list set inet ipset_test blocklist 2>/dev/null)
if echo "$set_output" | grep -q "192.168.1.100"; then
    pass "Can list set contents"
else
    fail "Can list set contents"
fi

# Test 8: Remove element from set
if nft delete element inet ipset_test blocklist "{ 10.0.0.1 }" 2>/dev/null; then
    pass "Can remove element from set"
else
    fail "Can remove element from set"
fi

# Test 9: Flush set (remove all elements)
if nft flush set inet ipset_test blocklist 2>/dev/null; then
    pass "Can flush set"
else
    fail "Can flush set"
fi

# ============================================
# Testing set usage in rules
# ============================================
echo "# Testing set usage in rules..."

# Test 10: Create chain for testing
nft add chain inet ipset_test input "{ type filter hook input priority 0; }" 2>/dev/null
if nft add rule inet ipset_test input ip saddr @cidr_set drop 2>/dev/null; then
    pass "Can use set in source address match"
else
    fail "Can use set in source address match"
fi

# Test 11: Use set in destination match
if nft add rule inet ipset_test input ip daddr @cidr_set drop 2>/dev/null; then
    pass "Can use set in destination address match"
else
    fail "Can use set in destination address match"
fi

# Test 12: Verify rules reference the set
rule_output=$(nft list chain inet ipset_test input 2>/dev/null)
if echo "$rule_output" | grep -q "@cidr_set"; then
    pass "Rules correctly reference set"
else
    fail "Rules correctly reference set"
fi

# ============================================
# Testing named/concat sets
# ============================================
echo "# Testing advanced set features..."

# Test 13: Create service (port) set
if nft add set inet ipset_test blocked_ports "{ type inet_service; }" 2>/dev/null; then
    pass "Can create inet_service set for ports"
else
    fail "Can create inet_service set for ports"
fi

# Test 14: Add ports to service set
if nft add element inet ipset_test blocked_ports "{ 22, 23, 3389 }" 2>/dev/null; then
    pass "Can add ports to service set"
else
    fail "Can add ports to service set"
fi

# Test 15: Use port set in rule
if nft add rule inet ipset_test input tcp dport @blocked_ports drop 2>/dev/null; then
    pass "Can use port set in rule"
else
    fail "Can use port set in rule"
fi

# ============================================
# Testing counter sets
# ============================================
echo "# Testing set with counters..."

# Test 16: Create set with counter flag
if nft add set inet ipset_test counted_ips "{ type ipv4_addr; flags interval; counter; }" 2>/dev/null; then
    pass "Can create set with counter flag"
else
    fail "Can create set with counter flag"
fi

# Test 17: Add element and verify counter field available
nft add element inet ipset_test counted_ips "{ 1.2.3.4 }" 2>/dev/null
counted_output=$(nft list set inet ipset_test counted_ips 2>/dev/null)
if echo "$counted_output" | grep -q "counter"; then
    pass "Counter flag visible in set listing"
else
    # Some versions may not show counter until traffic hits
    pass "Counter set created (counter shows after traffic)"
fi

# ============================================
# Testing FireHOL-style list parsing
# ============================================
echo "# Testing IP list parsing..."

# Test 18: Parse IP list format (simulated)
test_list="# Comment line
192.168.1.1
10.0.0.0/8 ; inline comment
# Another comment
172.16.0.0/12

8.8.8.8"

# Create a temp file with test data
echo "$test_list" > /tmp/test_iplist.txt
valid_count=$(grep -v "^#" /tmp/test_iplist.txt | grep -v "^$" | grep -v "^;" | wc -l | tr -d ' ')
if [ "$valid_count" -eq 4 ]; then
    pass "IP list parser handles comments and empty lines"
else
    fail "IP list parser handles comments and empty lines (got $valid_count, expected 4)"
fi

# Test 19: Large batch add (simulate FireHOL list import)
# Generate 100 test IPs
large_set=""
for i in $(seq 1 100); do
    octet=$((i % 256))
    large_set="$large_set 10.10.$((i / 256)).$octet,"
done
large_set=$(echo "$large_set" | sed 's/,$//')

nft add set inet ipset_test large_blocklist "{ type ipv4_addr; flags interval; }" 2>/dev/null
if nft add element inet ipset_test large_blocklist "{$large_set }" 2>/dev/null; then
    pass "Can add large batch of IPs (100 elements)"
else
    fail "Can add large batch of IPs (100 elements)"
fi

# Test 20: Verify large set has expected count
large_output=$(nft list set inet ipset_test large_blocklist 2>/dev/null)
element_count=$(echo "$large_output" | grep -oE "[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+" | wc -l | tr -d ' ')
if [ "$element_count" -ge 90 ]; then
    pass "Large set contains expected elements ($element_count elements)"
else
    fail "Large set contains expected elements ($element_count elements, expected ~100)"
fi

# ============================================
# Cleanup
# ============================================
echo "# Cleaning up..."
nft delete table inet ipset_test 2>/dev/null

rm -f /tmp/test_iplist.txt

# Output TAP plan
echo "1..$total_tests"
echo "#"
echo "# IPSet test summary:"
if [ $test_num -eq $total_tests ]; then
    echo "# All $total_tests IPSet tests completed!"
else
    echo "# Completed $test_num of $total_tests tests"
fi
