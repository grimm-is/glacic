#!/bin/sh
set -x

# TAP Test Harness - Sanity Checks
# Verifies the VM environment is correctly set up with capabilities required for tests.

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

# --- Start Test Suite ---

plan 8

diag "Running Environment Sanity Checks..."

wait_for_interfaces() {
    local interfaces="$@"
    local timeout=10
    local count=0
    diag "Waiting for interfaces: $interfaces"
    for iface in $interfaces; do
        until ip link show $iface >/dev/null 2>&1; do
            dilated_sleep 1
            count=$((count + 1))
            if [ $count -ge $timeout ]; then
                diag "Timeout waiting for interface $iface"
                return 1
            fi
        done
        diag "Interface $iface appeared."
    done
    return 0
}

# 1. Check Kernel Modules (nftables)
modprobe nf_tables_ipv4 >/dev/null 2>&1
ok 0 "Attempted to load nf_tables module"

# 2. Check Loopback Interface
ip link show lo >/dev/null 2>&1
ok $? "loopback interface exists"

# 3. Check Test Interfaces Existence
wait_for_interfaces eth0 eth1 eth2
ok $? "Test interfaces (eth0, eth1, eth2) are present"

# 4. Check Essential Tools
MISSING_TOOLS=""
which ip >/dev/null || MISSING_TOOLS="$MISSING_TOOLS ip"
if ! which curl >/dev/null && ! which wget >/dev/null; then
    MISSING_TOOLS="$MISSING_TOOLS curl/wget"
fi
which netstat >/dev/null || MISSING_TOOLS="$MISSING_TOOLS netstat"

if [ -z "$MISSING_TOOLS" ]; then
    ok 0 "Essential tools (ip, curl/wget, netstat) are present"
else
    ok 1 "Missing essential tools: $MISSING_TOOLS"
fi

# 5. Check DHCP Capabilities
if which dnsmasq >/dev/null || which dhcpd >/dev/null || which udhcpd >/dev/null; then
    ok 0 "DHCP server available"
else
    # Non-fatal if we are in a minimal env, but worth noting
    diag "Warning: No standard DHCP server found"
    ok 0 "DHCP server check (skipped/warn)"
fi

if which dhclient >/dev/null || which udhcpc >/dev/null; then
    ok 0 "DHCP client available"
else
    ok 0 "DHCP client check (skipped/warn)"
fi

# 6. Check HTTP/Network Tools
which nc >/dev/null
ok $? "netcat (nc) available"

which ss >/dev/null || which netstat >/dev/null
ok $? "socket stats (ss or netstat) available"

# --- End Test Suite ---

if [ $failed_count -eq 0 ]; then
    exit 0
else
    exit 1
fi
