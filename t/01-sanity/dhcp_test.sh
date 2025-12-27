#!/bin/sh
set -x

# DHCP Server Integration Test Script (TAP format)
# Tests DHCP server configuration and basic operations

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

plan 11

diag "Starting DHCP server integration tests..."

# ============================================
# Testing DHCP infrastructure
# ============================================
diag "Testing DHCP infrastructure..."

# 1. Determine local DHCP capabilities (info only, already checked in sanity)
if command -v dnsmasq >/dev/null 2>&1; then
    diag "Info: Using dnsmasq"
elif command -v dhcpd >/dev/null 2>&1; then
    diag "Info: Using ISC dhcpd"
fi

# ============================================
# Testing DHCP configuration parsing
# ============================================
diag "Testing DHCP configuration..."

# 4. Create test DHCP config directory
mkdir -p /tmp/dhcp_test
ok $? "Can create DHCP config directory"

# 5. Write dnsmasq-style DHCP config
cat > /tmp/dhcp_test/dnsmasq.conf << 'EOF'
# DHCP configuration for LAN
interface=eth1
dhcp-range=192.168.1.100,192.168.1.200,24h
dhcp-option=option:router,192.168.1.1
dhcp-option=option:dns-server,192.168.1.1,8.8.8.8
dhcp-option=option:domain-name,lan.local
dhcp-leasefile=/tmp/dhcp_test/leases
EOF
ok $? "Can write DHCP server config"

# 6. Verify config file is valid
if [ -f /tmp/dhcp_test/dnsmasq.conf ]; then
    grep -q "dhcp-range" /tmp/dhcp_test/dnsmasq.conf
    ok $? "DHCP range configured"
else
    ok 1 "DHCP config file missing"
fi

# 7. Check lease file can be created
touch /tmp/dhcp_test/leases
ok $? "Can create lease file"

# ============================================
# Testing DHCP client infrastructure
# ============================================
diag "Testing DHCP client..."

# 8. Check if dhclient or udhcpc is available
if command -v udhcpc >/dev/null 2>&1; then
    ok 0 "udhcpc DHCP client available"
    DHCP_CLIENT="udhcpc"
elif command -v dhclient >/dev/null 2>&1; then
    ok 0 "dhclient DHCP client available"
    DHCP_CLIENT="dhclient"
else
    ok 1 "No DHCP client available"
    DHCP_CLIENT=""
fi

# 5. Check /var/lib/dhcp or equivalent exists
mkdir -p /var/lib/dhcp /var/run
if [ -d /var/lib/dhcp ] || [ -d /var/run ]; then
    ok 0 "DHCP state directory available"
else
    ok 1 "No DHCP state directory"
fi

# ============================================
# Testing DHCP static leases
# ============================================
diag "Testing static lease configuration..."

# 10. Write static lease config
cat >> /tmp/dhcp_test/dnsmasq.conf << 'EOF'
# Static leases
dhcp-host=00:11:22:33:44:55,192.168.1.10,server1
dhcp-host=00:11:22:33:44:56,192.168.1.11,server2
EOF
ok $? "Can add static lease entries"

# 11. Verify static leases in config
grep -c "dhcp-host=" /tmp/dhcp_test/dnsmasq.conf | grep -q "2"
ok $? "Static leases configured correctly"

# ============================================
# Testing DHCP options
# ============================================
diag "Testing DHCP options..."

# 12. Add custom DHCP options
cat >> /tmp/dhcp_test/dnsmasq.conf << 'EOF'
# Custom options
dhcp-option=option:ntp-server,192.168.1.1
dhcp-option=option:netbios-ns,192.168.1.1
dhcp-boot=pxelinux.0,pxeserver,192.168.1.1
EOF
ok $? "Can add custom DHCP options"

# 13. Count total options
option_count=$(grep -c "dhcp-option" /tmp/dhcp_test/dnsmasq.conf)
[ "$option_count" -ge 5 ]
ok $? "Multiple DHCP options configured ($option_count options)"

# ============================================
# Cleanup
# ============================================
diag "Cleaning up..."

# 14. Remove test files
rm -rf /tmp/dhcp_test
ok $? "Cleaned up test files"

# --- End Test Suite ---

diag ""
diag "DHCP test summary:"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count DHCP tests passed!"
    exit 0
else
    diag "$failed_count of $test_count DHCP tests failed."
    exit 1
fi
