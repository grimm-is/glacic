#!/bin/sh
set -x

# QoS Integration Test Script (TAP format)
# Tests traffic shaping configuration via tc/netlink

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

# Check if tc is available
if ! command -v tc >/dev/null 2>&1; then
    echo "1..0 # SKIP tc command not found"
    exit 0
fi

# --- Start Test Suite ---

plan 18

diag "Starting QoS integration tests..."

# Get test interface (use lo for safe testing, or first non-lo interface)
TEST_IFACE="lo"
if ip link show eth0 >/dev/null 2>&1; then
    TEST_IFACE="eth0"
fi
diag "Using interface: $TEST_IFACE"

# --- Basic tc Infrastructure Tests ---

# 1. Test tc qdisc add/delete
diag "Testing basic tc qdisc operations..."
tc qdisc del dev $TEST_IFACE root 2>/dev/null  # Clear any existing
tc qdisc add dev $TEST_IFACE root handle 1: htb default 99
ok $? "Can add HTB root qdisc"

# 2. Verify qdisc exists
tc qdisc show dev $TEST_IFACE | grep -q "htb 1:"
ok $? "HTB qdisc visible in tc output"

# 3. Test root class creation
tc class add dev $TEST_IFACE parent 1: classid 1:1 htb rate 100mbit ceil 100mbit
ok $? "Can add root HTB class (100mbit)"

# 4. Test child class with priority
tc class add dev $TEST_IFACE parent 1:1 classid 1:10 htb rate 50mbit ceil 100mbit prio 1
ok $? "Can add high-priority child class (1:10)"

# 5. Test another child class
tc class add dev $TEST_IFACE parent 1:1 classid 1:20 htb rate 30mbit ceil 80mbit prio 3
ok $? "Can add normal-priority child class (1:20)"

# 6. Test default/bulk class
tc class add dev $TEST_IFACE parent 1:1 classid 1:99 htb rate 10mbit ceil 50mbit prio 7
ok $? "Can add low-priority default class (1:99)"

# 7. Verify classes exist
CLASS_COUNT=$(tc class show dev $TEST_IFACE | grep -c "class htb 1:")
[ "$CLASS_COUNT" -ge 4 ]
ok $? "All HTB classes visible ($CLASS_COUNT classes)"

# --- Filter Tests ---

# 8. Test u32 filter for port matching (SSH - port 22)
tc filter add dev $TEST_IFACE parent 1: protocol ip prio 1 u32 \
    match ip dport 22 0xffff match ip protocol 6 0xff flowid 1:10
ok $? "Can add u32 filter for SSH (port 22 -> class 1:10)"

# 9. Test u32 filter for HTTP
tc filter add dev $TEST_IFACE parent 1: protocol ip prio 1 u32 \
    match ip dport 80 0xffff match ip protocol 6 0xff flowid 1:20
ok $? "Can add u32 filter for HTTP (port 80 -> class 1:20)"

# 10. Test u32 filter for HTTPS
tc filter add dev $TEST_IFACE parent 1: protocol ip prio 1 u32 \
    match ip dport 443 0xffff match ip protocol 6 0xff flowid 1:20
ok $? "Can add u32 filter for HTTPS (port 443 -> class 1:20)"

# 11. Test DSCP filter (EF = 46, TOS = 184)
tc filter add dev $TEST_IFACE parent 1: protocol ip prio 1 u32 \
    match ip tos 0xb8 0xfc flowid 1:10
ok $? "Can add DSCP filter for EF (expedited forwarding)"

# 12. Verify filters exist
FILTER_COUNT=$(tc filter show dev $TEST_IFACE | grep -c "filter")
[ "$FILTER_COUNT" -ge 4 ]
ok $? "Filters visible ($FILTER_COUNT filters)"

# --- SFQ Leaf Qdisc Tests ---

# 13. Add SFQ to high-priority class
tc qdisc add dev $TEST_IFACE parent 1:10 handle 10: sfq perturb 10
ok $? "Can add SFQ qdisc to class 1:10"

# 14. Add SFQ to normal class
tc qdisc add dev $TEST_IFACE parent 1:20 handle 20: sfq perturb 10
ok $? "Can add SFQ qdisc to class 1:20"

# --- Statistics Tests ---

# 15. Test class statistics retrieval
tc -s class show dev $TEST_IFACE | grep -q "Sent"
ok $? "Can retrieve class statistics"

# 16. Test qdisc statistics retrieval
tc -s qdisc show dev $TEST_IFACE | grep -q "Sent"
ok $? "Can retrieve qdisc statistics"

# --- Cleanup Tests ---

# 17. Test filter deletion
tc filter del dev $TEST_IFACE parent 1: 2>/dev/null
ok $? "Can delete filters"

# 18. Test qdisc deletion (removes all)
tc qdisc del dev $TEST_IFACE root
ok $? "Can delete root qdisc (cleans up everything)"

# --- End Test Suite ---

diag ""
diag "QoS test summary:"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count QoS tests passed!"
    exit 0
else
    diag "$failed_count of $test_count QoS tests failed."
    exit 1
fi
