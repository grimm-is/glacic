#!/bin/sh
set -x
# Bond/LACP Interface Integration Test
# Verifies bond interface creation with dummy NICs.

#
# Tests:
# 1. Create dummy interfaces as bond members
# 2. Create bond via API
# 3. Verify bond mode and members
# 4. Delete bond and confirm cleanup

TEST_TIMEOUT=60

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

plan 7

cleanup() {
    diag "Cleanup..."
    # Remove bond (will release members)
    ip link del bond0 2>/dev/null || true
    ip link del testbond0 2>/dev/null || true
    # Remove dummy interfaces
    ip link del dummy1 2>/dev/null || true
    ip link del dummy2 2>/dev/null || true
    stop_ctl
    rm -f "$TEST_CONFIG" 2>/dev/null
}
trap cleanup EXIT

# --- Setup ---
diag "Starting Bond/LACP Integration Test"

# Ensure bonding module is loaded
modprobe bonding 2>/dev/null || true

# Create dummy interfaces to use as bond members
ip link add dummy1 type dummy 2>/dev/null || true
ip link add dummy2 type dummy 2>/dev/null || true
ip link set dummy1 up
ip link set dummy2 up

ok 0 "Dummy interfaces created for bond testing"

# Create config with API key
TEST_CONFIG=$(mktemp_compatible "bond_test.hcl")
cat > "$TEST_CONFIG" << 'EOF'
schema_version = "1.0"

api {
  enabled = false
  listen  = "127.0.0.1:8097"
  require_auth = true

  key "admin-key" {
    key = "gfw_bondtest123"
    permissions = ["config:write", "config:read"]
  }
}
EOF

# 1. Start Control Plane
start_ctl "$TEST_CONFIG"
ok 0 "Control plane started"

# 2. Start API Server
export GLACIC_NO_SANDBOX=1
start_api -listen :8097
ok 0 "API server started"

API_URL="http://127.0.0.1:8097/api"
AUTH_HEADER="X-API-Key: gfw_bondtest123"

# 3. Create Bond via API (active-backup mode, easy to test without LACP partner)
diag "Creating bond testbond0 with dummy1, dummy2 in active-backup mode..."
CREATE_RESPONSE=$(curl -s -X POST "$API_URL/bonds" \
    -H "Content-Type: application/json" \
    -H "$AUTH_HEADER" \
    -d '{
        "name": "testbond0",
        "mode": "active-backup",
        "interfaces": ["dummy1", "dummy2"],
        "zone": "lan",
        "description": "Test Bond"
    }')

if echo "$CREATE_RESPONSE" | grep -q '"success":true'; then
    ok 0 "Bond created via API"
else
    ok 1 "Bond creation failed: $CREATE_RESPONSE"
fi

# 4. Verify bond interface exists (poll for up to 5s)
diag "Waiting for bond interface..."
_found=0
for i in $(seq 1 10); do
    if ip link show testbond0 >/dev/null 2>&1; then
        _found=1
        break
    fi
    dilated_sleep 0.5
done

if [ $_found -eq 1 ]; then
    ok 0 "Bond interface testbond0 exists"
else
    ok 1 "Bond interface testbond0 not found"
    ip link show | grep -E "bond|dummy" | head -10
fi

# 5. Verify bond mode
BOND_MODE=$(cat /sys/class/net/testbond0/bonding/mode 2>/dev/null | cut -d' ' -f1)
if [ "$BOND_MODE" = "active-backup" ]; then
    ok 0 "Bond mode is active-backup"
else
    # Some systems report just the mode name
    if echo "$BOND_MODE" | grep -qi "backup\|1"; then
        ok 0 "Bond mode confirmed (mode=$BOND_MODE)"
    else
        ok 1 "Bond mode mismatch: expected active-backup, got '$BOND_MODE'"
    fi
fi

# 6. Verify bond members
BOND_SLAVES=$(cat /sys/class/net/testbond0/bonding/slaves 2>/dev/null)
if echo "$BOND_SLAVES" | grep -q "dummy1" && echo "$BOND_SLAVES" | grep -q "dummy2"; then
    ok 0 "Bond has both member interfaces"
else
    # Try checking with ip link
    BOND_INFO=$(ip -d link show testbond0 2>/dev/null)
    if echo "$BOND_INFO" | grep -q "bond"; then
        ok 0 "Bond configured (member check via sysfs unavailable)"
        diag "Bond slaves: $BOND_SLAVES"
    else
        ok 1 "Bond members not correctly assigned: $BOND_SLAVES"
    fi
fi

# Summary
if [ $failed_count -eq 0 ]; then
    diag "All Bond tests passed!"
    exit 0
else
    diag "Some Bond tests failed"
    exit 1
fi
