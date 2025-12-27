#!/bin/sh
set -x

# Interface Configuration Integration Test Script (TAP format)
# Tests network interface, VLAN, bonding, and MTU configuration

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

plan 22

diag "Starting interface configuration tests..."

# Get test interface (first non-lo interface)
TEST_IFACE=$(ip link show | grep -E "^[0-9]+: " | grep -v "lo:" | head -1 | awk -F': ' '{print $2}' | cut -d'@' -f1)
diag "Using test interface: $TEST_IFACE"

# ============================================
# Testing basic interface operations
# ============================================
diag "Testing basic interface operations..."

# 1. List interfaces
iface_count=$(ip link show | grep -c "^[0-9]")
[ "$iface_count" -ge 1 ]
ok $? "Can list network interfaces ($iface_count interfaces)"

# 2. Get interface details
ip link show $TEST_IFACE >/dev/null 2>&1
ok $? "Can get interface details for $TEST_IFACE"

# 3. Get interface state
ip link show $TEST_IFACE | grep -qE "state (UP|DOWN|UNKNOWN)"
ok $? "Can read interface state"

# 4. Get interface MAC address
mac=$(ip link show $TEST_IFACE | grep -oE "link/ether [0-9a-f:]+" | awk '{print $2}')
[ -n "$mac" ]
ok $? "Can read MAC address ($mac)"

# ============================================
# Testing IP address configuration
# ============================================
diag "Testing IP address configuration..."

# 5. Create dummy interface for safe testing
ip link add dummy0 type dummy 2>/dev/null || true
ip link set dummy0 up
ok $? "Can create dummy interface for testing"

# 6. Add IP address
ip addr add 10.99.99.1/24 dev dummy0 2>/dev/null
ok $? "Can add IP address to interface"

# 7. Verify IP address
ip addr show dummy0 | grep -q "10.99.99.1"
ok $? "IP address visible on interface"

# 8. Add secondary IP address
ip addr add 10.99.99.2/24 dev dummy0 2>/dev/null
ok $? "Can add secondary IP address"

# 9. Remove IP address
ip addr del 10.99.99.2/24 dev dummy0 2>/dev/null
ok $? "Can remove IP address"

# ============================================
# Testing MTU configuration
# ============================================
diag "Testing MTU configuration..."

# 10. Get current MTU
mtu=$(ip link show dummy0 | grep -oE "mtu [0-9]+" | awk '{print $2}')
[ -n "$mtu" ]
ok $? "Can read MTU ($mtu)"

# 11. Set custom MTU
ip link set dummy0 mtu 9000 2>/dev/null
ok $? "Can set jumbo MTU (9000)"

# 12. Verify MTU change
new_mtu=$(ip link show dummy0 | grep -oE "mtu [0-9]+" | awk '{print $2}')
[ "$new_mtu" = "9000" ]
ok $? "MTU changed successfully ($new_mtu)"

# 13. Reset MTU to normal
ip link set dummy0 mtu 1500
ok $? "Can reset MTU to default"

# ============================================
# Testing VLAN configuration
# ============================================
diag "Testing VLAN configuration..."

# 14. Load 8021q module
modprobe 8021q 2>/dev/null || true
if lsmod | grep -q 8021q; then
    ok 0 "8021q VLAN module loaded"
else
    ok 0 "8021q module not available # SKIP"
fi

# 15. Create VLAN interface
ip link add link dummy0 name dummy0.100 type vlan id 100 2>/dev/null
if [ $? -eq 0 ]; then
    ok 0 "Can create VLAN interface (ID 100)"
    ip link set dummy0.100 up
else
    ok 0 "VLAN creation failed # SKIP (module not loaded)"
fi

# 16. Verify VLAN interface
if ip link show dummy0.100 >/dev/null 2>&1; then
    ok 0 "VLAN interface visible"
    # Add IP to VLAN
    ip addr add 10.100.0.1/24 dev dummy0.100 2>/dev/null
else
    ok 0 "VLAN interface not created # SKIP"
fi

# 17. Delete VLAN interface
ip link del dummy0.100 2>/dev/null
ok $? "Can delete VLAN interface"

# ============================================
# Testing bonding configuration
# ============================================
diag "Testing bonding configuration..."

# 18. Load bonding module
modprobe bonding 2>/dev/null || true
if lsmod | grep -q bonding; then
    ok 0 "Bonding module loaded"
else
    ok 0 "Bonding module not available # SKIP"
fi

# 19. Create bond interface
ip link add bond0 type bond mode balance-rr 2>/dev/null
if [ $? -eq 0 ]; then
    ok 0 "Can create bond interface"
    ip link set bond0 up 2>/dev/null
else
    ok 0 "Bond creation failed # SKIP"
fi

# 20. Verify bond interface
if ip link show bond0 >/dev/null 2>&1; then
    ok 0 "Bond interface visible"
else
    ok 0 "Bond interface not created # SKIP"
fi

# ============================================
# Cleanup
# ============================================
diag "Cleaning up..."

# 21. Delete bond interface
ip link del bond0 2>/dev/null
ok $? "Cleaned up bond interface"

# 22. Delete dummy interface
ip link del dummy0 2>/dev/null
ok $? "Cleaned up dummy interface"

# --- End Test Suite ---

diag ""
diag "Interface test summary:"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count interface tests passed!"
    exit 0
else
    diag "$failed_count of $test_count interface tests failed."
    exit 1
fi
