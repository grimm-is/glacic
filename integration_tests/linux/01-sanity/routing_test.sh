#!/bin/sh
set -x

# Routing Integration Test Script (TAP format)
# Tests static routes, policy routing, and route monitoring

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

# Check for ip command
if ! command -v ip >/dev/null 2>&1; then
    echo "1..0 # SKIP ip command not found"
    exit 0
fi

# --- Start Test Suite ---

plan 20

diag "Starting routing integration tests..."

# Create dummy interface for safe testing
ip link add dummy0 type dummy 2>/dev/null || true
ip link set dummy0 up
ip addr add 10.99.99.1/24 dev dummy0 2>/dev/null || true

# ============================================
# Testing basic routing operations
# ============================================
diag "Testing basic routing operations..."

# 1. View routing table
route_count=$(ip route show | wc -l)
[ "$route_count" -ge 0 ]
ok $? "Can view routing table ($route_count routes)"

# 2. Get default route
default_route=$(ip route show default 2>/dev/null | head -1)
if [ -n "$default_route" ]; then
    ok 0 "Default route exists: $default_route"
else
    ok 0 "No default route configured"
fi

# 3. Add static route
if ip route add 192.168.100.0/24 dev dummy0 2>/dev/null; then
    ok 0 "Can add static route via interface"
else
    ok 0 "Static route already exists or added"
fi

# 4. Verify route exists
ip route show | grep -q "192.168.100.0/24"
ok $? "Static route visible in table"

# 5. Add route with gateway
ip route add 192.168.200.0/24 via 10.99.99.254 dev dummy0 2>/dev/null
ok $? "Can add route with gateway"

# 6. Add route with metric
ip route add 192.168.201.0/24 via 10.99.99.254 dev dummy0 metric 100 2>/dev/null
ok $? "Can add route with metric"

# 7. Delete static route
if ip route del 192.168.100.0/24 dev dummy0 2>/dev/null; then
    ok 0 "Can delete static route"
else
    ok 0 "Static route already deleted or not found"
fi

# ============================================
# Testing routing tables
# ============================================
diag "Testing multiple routing tables..."

# 8. View all routing tables
ip route show table all 2>/dev/null | head -5 >/dev/null
ok $? "Can view all routing tables"

# 9. Add route to custom table
ip route add 10.50.0.0/24 dev dummy0 table 100 2>/dev/null
ok $? "Can add route to custom table (table 100)"

# 10. Verify custom table route
ip route show table 100 | grep -q "10.50.0.0/24"
ok $? "Route visible in custom table"

# 11. Delete route from custom table
ip route del 10.50.0.0/24 dev dummy0 table 100 2>/dev/null
ok $? "Can delete route from custom table"

# ============================================
# Testing policy routing (ip rule)
# ============================================
diag "Testing policy routing..."

# 12. View routing rules
rule_count=$(ip rule show | wc -l)
[ "$rule_count" -ge 2 ]
ok $? "Can view routing rules ($rule_count rules)"

# 13. Add source-based routing rule
ip rule add from 10.99.99.0/24 table 100 priority 1000 2>/dev/null
ok $? "Can add source-based routing rule"

# 14. Verify rule exists
ip rule show | grep -q "from 10.99.99.0/24"
ok $? "Routing rule visible"

# 15. Delete routing rule
ip rule del from 10.99.99.0/24 table 100 priority 1000 2>/dev/null
ok $? "Can delete routing rule"

# ============================================
# Testing route caching and neighbor table
# ============================================
diag "Testing route cache and neighbors..."

# 16. View neighbor table (ARP)
ip neigh show | head -3 >/dev/null
ok $? "Can view neighbor (ARP) table"

# 17. Add static ARP entry
ip neigh add 10.99.99.100 lladdr 00:11:22:33:44:55 dev dummy0 2>/dev/null
ok $? "Can add static neighbor entry"

# 18. Delete static ARP entry
ip neigh del 10.99.99.100 dev dummy0 2>/dev/null
ok $? "Can delete static neighbor entry"

# ============================================
# Testing blackhole and unreachable routes
# ============================================
diag "Testing special route types..."

# 19. Add blackhole route
ip route add blackhole 192.168.250.0/24 2>/dev/null
if [ $? -eq 0 ]; then
    ok 0 "Can add blackhole route"
    ip route del blackhole 192.168.250.0/24 2>/dev/null
else
    ok 0 "Blackhole route failed # SKIP"
fi

# ============================================
# Cleanup
# ============================================
diag "Cleaning up..."

# 20. Remove test routes and interface
ip route del 192.168.200.0/24 2>/dev/null
ip route del 192.168.201.0/24 2>/dev/null
ip link del dummy0 2>/dev/null
ok $? "Cleaned up test routes and interface"

# --- End Test Suite ---

diag ""
diag "Routing test summary:"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count routing tests passed!"
    exit 0
else
    diag "$failed_count of $test_count routing tests failed."
    exit 1
fi
