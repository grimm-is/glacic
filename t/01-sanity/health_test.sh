#!/bin/sh
set -x

# Health Check Integration Test Script (TAP format)
# Tests health probes, system checks, and monitoring

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

# --- Start Test Suite ---

plan 18

diag "Starting health check integration tests..."

# ============================================
# Testing nftables health
# ============================================
diag "Testing nftables health..."

# 1. nft command available
command -v nft >/dev/null 2>&1
ok $? "nft command available"

# 2. Can list tables
nft list tables >/dev/null 2>&1
ok $? "Can list nftables tables"

# 3. Can create test table
nft add table inet health_test 2>/dev/null
ok $? "Can create test table"

# 4. Can delete test table
nft delete table inet health_test 2>/dev/null
ok $? "Can delete test table"

# ============================================
# Testing conntrack health
# ============================================
diag "Testing conntrack health..."

# 5. Conntrack count readable
if [ -f /proc/sys/net/netfilter/nf_conntrack_count ]; then
    ct_count=$(cat /proc/sys/net/netfilter/nf_conntrack_count)
    ok 0 "Conntrack count readable ($ct_count)"
else
    ok 0 "Conntrack not available # SKIP"
fi

# 6. Conntrack max readable
if [ -f /proc/sys/net/netfilter/nf_conntrack_max ]; then
    ct_max=$(cat /proc/sys/net/netfilter/nf_conntrack_max)
    ok 0 "Conntrack max readable ($ct_max)"
else
    ok 0 "Conntrack max not available # SKIP"
fi

# ============================================
# Testing interface health
# ============================================
diag "Testing interface health..."

# 7. ip command available
command -v ip >/dev/null 2>&1
ok $? "ip command available"

# 8. Can list interfaces
ip link show >/dev/null 2>&1
ok $? "Can list interfaces"

# 9. Count interfaces (any state is fine for health check)
iface_count=$(ip link show | grep -c "^[0-9]")
[ "$iface_count" -ge 1 ]
ok $? "At least one interface exists ($iface_count)"

# ============================================
# Testing disk health
# ============================================
diag "Testing disk health..."

# 10. Can write to /tmp
echo "test" > /tmp/.health_test 2>/dev/null
ok $? "Can write to /tmp"

# 11. Can read from /tmp
cat /tmp/.health_test >/dev/null 2>&1
ok $? "Can read from /tmp"

# 12. Cleanup test file
rm -f /tmp/.health_test
ok $? "Can delete test file"

# ============================================
# Testing memory health
# ============================================
diag "Testing memory health..."

# 13. Can read meminfo
if [ -f /proc/meminfo ]; then
    ok 0 "/proc/meminfo readable"
else
    ok 1 "/proc/meminfo not available"
fi

# 14. MemAvailable present
grep -q "MemAvailable" /proc/meminfo 2>/dev/null
ok $? "MemAvailable metric present"

# 15. MemFree present
grep -q "MemFree" /proc/meminfo 2>/dev/null
ok $? "MemFree metric present"

# ============================================
# Testing process health
# ============================================
diag "Testing process health..."

# 16. Can read /proc/self
[ -d /proc/self ]
ok $? "/proc/self accessible"

# 17. Can read uptime
if [ -f /proc/uptime ]; then
    uptime=$(cut -d' ' -f1 /proc/uptime)
    ok 0 "System uptime: ${uptime}s"
else
    ok 0 "Uptime not available # SKIP"
fi

# ============================================
# Testing network health
# ============================================
diag "Testing network health..."

# 18. Loopback interface exists
ip link show lo >/dev/null 2>&1
ok $? "Loopback interface exists"

# --- End Test Suite ---

diag ""
diag "Health check test summary:"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count health tests passed!"
    exit 0
else
    diag "$failed_count of $test_count health tests failed."
    exit 1
fi
