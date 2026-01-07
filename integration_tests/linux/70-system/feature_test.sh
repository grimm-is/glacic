#!/bin/sh
set -x

# TAP Test Harness - Feature Tests
# Verifies the Firewall Appliance application features.
# Uses :8080 to bind all interfaces (127.0.0.1 may not be up yet)

# Needs time for service startup
# Needs time for service startup
TEST_TIMEOUT=30

# Source common functions
. "$(dirname "$0")/../common.sh"


wait_for_interfaces() {
    local interfaces="$@"
    local timeout=30
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

cleanup() {
    if [ -n "$API_PID" ]; then
        kill $API_PID 2>/dev/null
    fi
    if [ -n "$CTL_PID" ]; then
        kill $CTL_PID 2>/dev/null
    fi
    rm -f $CTL_SOCKET 2>/dev/null
    rm -f /tmp/api.log 2>/dev/null
}
trap cleanup EXIT

# --- Start Test Suite ---

plan 8

diag "Starting Feature Tests..."

# Create dummy SPA asset
mkdir -p dist
echo "<html><title>Glacic Dashboard</title></html>" > dist/index.html

# 1. Start Application
diag "Starting glacic in daemon mode..."
wait_for_interfaces eth1 eth2 || {
    ok 1 "Required interfaces missing"
    exit 1
}

# Start Control Plane
FIREWALL_CONFIG="$MOUNT_PATH/tests/test-vm.hcl"
start_ctl "$FIREWALL_CONFIG"

# Start API Server
export GLACIC_UI_DIST="./dist"
export GLACIC_NO_SANDBOX=1
start_api -listen :8080

diag "Services started (CTL: $CTL_PID, API: $API_PID)"

# 2. Check Static IP Assignments (from test-vm.hcl)
# Assuming test-vm.hcl configures 10.1.0.1 on eth1
ip addr show eth1 | grep "10.1.0.1" >/dev/null 2>&1
ok $? "eth1 has configured IP (10.1.0.1)"

# 3. Check DHCP Server Status
# Check for listening port 67 (DHCP)
if netstat -uln 2>/dev/null | grep ":67" >/dev/null 2>&1 || ss -uln 2>/dev/null | grep ":67" >/dev/null 2>&1; then
    ok 0 "DHCP server listening on port 67"
else
    ok 1 "DHCP server NOT listening on port 67"
fi

# 4. Check DNS Server Status
# Check for listening port 53 (DNS)
if netstat -uln 2>/dev/null | grep ":53" >/dev/null 2>&1 || ss -uln 2>/dev/null | grep ":53" >/dev/null 2>&1; then
    ok 0 "DNS server listening on port 53"
else
    ok 1 "DNS server NOT listening on port 53"
fi

# Wrapper for HTTP requests
http_get() {
    url="$1"
    if command -v curl >/dev/null 2>&1; then
        curl -s --max-time 5 "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O - "$url"
    else
        return 1
    fi
}

# 5. Check API Status
http_get http://localhost:8080/api/status | grep "online" >/dev/null 2>&1
ok $? "API /api/status returns online"

# 6. Check API Config
http_get http://localhost:8080/api/config | grep "ip_forwarding" >/dev/null 2>&1
ok $? "API /api/config returns config JSON"

# 7. Check SPA Serving
http_get http://localhost:8080/ | grep -i "glacic\|firewall\|dashboard" >/dev/null 2>&1
ok $? "API serves SPA index.html"

# 8. Check Prometheus Metrics
http_get http://localhost:8080/metrics | grep "# HELP" >/dev/null 2>&1
ok $? "API /metrics returns Prometheus metrics"

# Cleanup
kill $CTL_PID $API_PID 2>/dev/null
rm -f $CTL_SOCKET

if [ $failed_count -eq 0 ]; then
    exit 0
else
    exit 1
fi
