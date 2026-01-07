#!/bin/sh
set -x

# Learning Traffic Test (TAP format)
# Tests that actual traffic is captured by the learning service via nflog/ctlplane bridge.

TEST_TIMEOUT=30

# Source common functions
. "$(dirname "$0")/../common.sh"

require_root
require_binary

if ! command -v ip >/dev/null 2>&1; then
    echo "1..0 # SKIP iproute2 (ip command) not found"
    exit 0
fi

# Need log directory
mkdir -p /var/log/glacic
LOG_FILE="/var/log/glacic/glacic.log"
# Clear log file
: > "$LOG_FILE"

# --- Test Suite ---
plan 4

diag "================================================"
diag "Learning Traffic Test"
diag "Tests e2e flow capture (Log Verification)"
diag "================================================"

STATE_DIR="/tmp/glacic-learning-test"
mkdir -p "$STATE_DIR"
FLOW_DB="$STATE_DIR/flow.db"

# Register cleanup handler
cleanup_on_exit

# --- Setup ---
TEST_CONFIG=$(mktemp_compatible "learning_traffic.hcl")
cat > "$TEST_CONFIG" << EOF
schema_version = "1.0"
ip_forwarding = true
state_dir = "$STATE_DIR"

rule_learning {
  enabled = true
  learning_mode = true
  log_group = 100
}

zone "lan" {
  interfaces = ["veth-lan"]
}

zone "wan" {
  interfaces = ["veth-wan"]
}

interface "veth-lan" {
  zone = "lan"
  ipv4 = ["192.168.100.1/24"]
}

interface "veth-wan" {
  zone = "wan"
  ipv4 = ["10.99.99.1/24"]
}

# policy "lan" "wan" removed to force drop (and learning)
# Traffic will hit default drop rule (group 0)


nat "masq" {
  type = "masquerade"
  out_interface = "veth-wan"
}
EOF

# Test 1: Setup Topology
diag "Creating topology..."
setup_test_topology
ok $? "Topology created"

# Test 2: Start Firewall
diag "Starting firewall..."
start_ctl "$TEST_CONFIG"
ok $? "Firewall started"

# Wait for API/Learning to be ready (poll for log activity)
diag "Waiting for learning service..."
count=0
while ! grep -qE "Learning service started|starting .* learning engine" "$CTL_LOG" 2>/dev/null; do
    dilated_sleep 0.2
    count=$((count + 1))
    if [ $count -ge 25 ]; then  # 5s max
        diag "Timeout waiting for learning service"
        break
    fi
done
diag "Learning ready after $((count * 200))ms"

# Test 3: Generate Traffic
diag "Generating traffic (LAN -> WAN)..."
# Ping server (10.99.99.100) from client (192.168.100.100)
# run_client is a helper in common.sh
run_client ping -c 1 -W 1 10.99.99.100 >/dev/null 2>&1
dilated_sleep 2

# Test 4: Verify Logs
diag "Verifying logs for captured flow..."
if grep -q "new flow detected" "$CTL_LOG"; then
    ok 0 "Flow detection confirmed in logs"
    grep "new flow detected" "$CTL_LOG" | head -n 1 | sed 's/^/# /'
else
    ok 1 "Flow detection NOT found in logs"
    diag "Glacic Log content (tail):"
    tail -n 20 "$LOG_FILE" | sed 's/^/# /'

    if [ -n "$CTL_LOG" ] && [ -f "$CTL_LOG" ]; then
        diag "Control Plane Log (all):"
        cat "$CTL_LOG" | sed 's/^/# /'
    else
        diag "Control Plane Log not found (CTL_LOG=$CTL_LOG)"
    fi

    diag "NFTables Ruleset:"
    nft list ruleset 2>/dev/null | head -n 50 | sed 's/^/# /' || diag "Failed to list ruleset"
fi
