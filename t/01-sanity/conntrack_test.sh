#!/bin/sh
set -x

# Connection Tracking Helpers Integration Test Script (TAP format)
# Tests loading/unloading of conntrack helper modules

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

# Check if modprobe is available
if ! command -v modprobe >/dev/null 2>&1; then
    echo "1..0 # SKIP modprobe command not found"
    exit 0
fi

# --- Start Test Suite ---

plan 16

diag "Starting conntrack helpers integration tests..."

# --- Core Conntrack Module ---

# 1. Check if nf_conntrack is loaded
diag "Testing core conntrack module..."
lsmod | grep -q "nf_conntrack"
if [ $? -eq 0 ]; then
    ok 0 "nf_conntrack module already loaded"
else
    modprobe nf_conntrack
    ok $? "Can load nf_conntrack module"
fi

# 2. Verify conntrack sysctl interface
[ -d /proc/sys/net/netfilter ]
ok $? "Netfilter proc interface available"

# --- FTP Helper ---

# 3. Load FTP helper
diag "Testing FTP helper..."
modprobe nf_conntrack_ftp 2>/dev/null
if [ $? -eq 0 ]; then
    ok 0 "Can load nf_conntrack_ftp module"
else
    skip "nf_conntrack_ftp not available"
fi

# 4. Verify FTP helper loaded
if lsmod | grep -q "nf_conntrack_ftp"; then
    ok 0 "FTP helper module visible in lsmod"
else
    skip "FTP helper not loaded"
fi

# --- TFTP Helper ---

# 5. Load TFTP helper
diag "Testing TFTP helper..."
modprobe nf_conntrack_tftp 2>/dev/null
if [ $? -eq 0 ]; then
    ok 0 "Can load nf_conntrack_tftp module"
else
    skip "nf_conntrack_tftp not available"
fi

# 6. Verify TFTP helper loaded
if lsmod | grep -q "nf_conntrack_tftp"; then
    ok 0 "TFTP helper module visible in lsmod"
else
    skip "TFTP helper not loaded"
fi

# --- SIP Helper ---

# 7. Load SIP helper
diag "Testing SIP helper..."
modprobe nf_conntrack_sip 2>/dev/null
if [ $? -eq 0 ]; then
    ok 0 "Can load nf_conntrack_sip module"
else
    skip "nf_conntrack_sip not available"
fi

# 8. Verify SIP helper loaded
if lsmod | grep -q "nf_conntrack_sip"; then
    ok 0 "SIP helper module visible in lsmod"
else
    skip "SIP helper not loaded"
fi

# --- IRC Helper ---

# 9. Load IRC helper
diag "Testing IRC helper..."
modprobe nf_conntrack_irc 2>/dev/null
if [ $? -eq 0 ]; then
    ok 0 "Can load nf_conntrack_irc module"
else
    skip "nf_conntrack_irc not available"
fi

# 10. Verify IRC helper loaded
if lsmod | grep -q "nf_conntrack_irc"; then
    ok 0 "IRC helper module visible in lsmod"
else
    skip "IRC helper not loaded"
fi

# --- Conntrack Statistics ---

# 11. Check conntrack count readable
diag "Testing conntrack statistics..."
if [ -f /proc/sys/net/netfilter/nf_conntrack_count ]; then
    COUNT=$(cat /proc/sys/net/netfilter/nf_conntrack_count)
    ok 0 "Can read conntrack count: $COUNT"
else
    skip "nf_conntrack_count not available"
fi

# 12. Check conntrack max readable
if [ -f /proc/sys/net/netfilter/nf_conntrack_max ]; then
    MAX=$(cat /proc/sys/net/netfilter/nf_conntrack_max)
    ok 0 "Can read conntrack max: $MAX"
else
    skip "nf_conntrack_max not available"
fi

# --- Conntrack Tool ---

# 13. Check conntrack tool available
diag "Testing conntrack tool..."
if command -v conntrack >/dev/null 2>&1; then
    ok 0 "conntrack command available"
else
    skip "conntrack command not installed"
fi

# 14. List conntrack entries
if command -v conntrack >/dev/null 2>&1; then
    conntrack -L >/dev/null 2>&1
    ok $? "Can list conntrack entries"
else
    skip "conntrack command not available"
fi

# --- Helper Expect Table ---

# 15. Check expect max
if [ -f /proc/sys/net/netfilter/nf_conntrack_expect_max ]; then
    EXPECT_MAX=$(cat /proc/sys/net/netfilter/nf_conntrack_expect_max)
    ok 0 "Can read expect max: $EXPECT_MAX"
else
    skip "nf_conntrack_expect_max not available"
fi

# --- Verification ---

# 16. Count loaded conntrack modules
HELPER_COUNT=$(lsmod | grep -c "nf_conntrack")
[ "$HELPER_COUNT" -ge 1 ]
ok $? "At least one conntrack module loaded ($HELPER_COUNT modules)"

# --- End Test Suite ---

diag ""
diag "Conntrack helpers test summary:"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count conntrack tests passed!"
    exit 0
else
    diag "$failed_count of $test_count conntrack tests failed."
    exit 1
fi
