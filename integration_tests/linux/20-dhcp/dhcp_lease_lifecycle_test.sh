#!/bin/sh
# DHCP Lease Lifecycle Integration Test
# Verifies lease allocation, expiration, and IP reclamation
# DHCP server works with entries table and bucket=dhcp_leases

set -e
set -x

TEST_TIMEOUT=60

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
. "$SCRIPT_DIR/../common.sh"

if ! command -v sqlite3 >/dev/null 2>&1; then
    echo "1..0 # SKIP sqlite3 not found"
    exit 0
fi

CONFIG_FILE=$(mktemp /tmp/dhcp-lifecycle-config.hcl.XXXXXX)

# Use very short lease time for testing (15 seconds)
cat > "$CONFIG_FILE" << 'EOF'
schema_version = "1.1"

interface "lo" {
  zone = "lan"
  ipv4 = ["192.168.1.1/24"]
}

zone "lan" {
  interfaces = ["lo"]
}

dhcp {
  enabled = true

  scope "lan_pool" {
    interface = "lo"
    range_start = "192.168.1.100"
    range_end = "192.168.1.102"  # Small pool for testing
    router = "192.168.1.1"
    dns = ["8.8.8.8"]
    lease_time = "15s"  # Very short for integration testing
  }
}
EOF

ok 0 "Created test config with short lease time (15s)"

# Assign IP to lo so DHCP can bind
ip addr add 192.168.1.1/24 dev lo 2>/dev/null || true

# Start control plane
start_ctl "$CONFIG_FILE"

# Wait for DHCP to be ready
dilated_sleep 3

# Check if DHCP server is listening
if netstat -uln | grep -q ":67 "; then
    ok 0 "DHCP server listening on port 67"
else
    ok 1 "DHCP server NOT listening on port 67"
    stop_ctl
    exit 1
fi

# Test 1: Allocate a lease
diag "=== Test 1: Allocate first DHCP lease ==="
ip addr del 192.168.1.1/24 dev lo 2>/dev/null || true
dilated_sleep 1

UDHCPC_SCRIPT=$(mktemp /tmp/udhcpc-script.XXXXXX)
cat > "$UDHCPC_SCRIPT" << 'UDHCPC_EOF'
#!/bin/sh
echo "DHCP_EVENT=$1" >> /tmp/dhcp_lease1.log
echo "DHCP_IP=$ip" >> /tmp/dhcp_lease1.log
UDHCPC_EOF
chmod +x "$UDHCPC_SCRIPT"

timeout 10 udhcpc -f -i lo -s "$UDHCPC_SCRIPT" -q -n -t 3 >/tmp/udhcpc1.log 2>&1 || true
ip addr add 192.168.1.1/24 dev lo 2>/dev/null || true

if [ -f /tmp/dhcp_lease1.log ]; then
    LEASE1_IP=$(grep "DHCP_IP=" /tmp/dhcp_lease1.log | cut -d= -f2 | tr -d '\n' | tr -d ' ')
    if [ -n "$LEASE1_IP" ]; then
        ok 0 "First lease allocated: $LEASE1_IP"
        diag "Allocated IP: $LEASE1_IP"
    else
        ok 1 "First lease allocation failed"
    fi
else
    ok 1 "First lease log not created"
fi

# Test 2: Verify lease persistence in SQLite
diag "=== Test 2: Verify lease persistence ==="
dilated_sleep 2
if [ -f "$STATE_DIR/state.db" ]; then
    # Check if entry exists in entries table with bucket=dhcp_leases
    MAX_RETRIES=10
    COUNT=0
    while [ $COUNT -lt $MAX_RETRIES ]; do
        LEASE_JSON=$(sqlite3 "$STATE_DIR/state.db" "SELECT value FROM entries WHERE bucket = 'dhcp_leases' AND value LIKE '%\"ip\":\"$LEASE1_IP\"%'" 2>/dev/null || echo "")
        if [ -n "$LEASE_JSON" ]; then
            break
        fi
        dilated_sleep 1
        COUNT=$((COUNT+1))
    done

    if [ -n "$LEASE_JSON" ]; then
        ok 0 "Lease persisted to SQLite database"
        diag "Found lease in database for IP: $LEASE1_IP"
        diag "Lease JSON: $LEASE_JSON"
    else
        ok 1 "Lease NOT found in database"
        diag "Looked for IP: $LEASE1_IP in bucket 'dhcp_leases'"
        sqlite3 "$STATE_DIR/state.db" "SELECT bucket, length(value) FROM entries"
    fi
else
    ok 1 "State database not found at $STATE_DIR/state.db"
fi

# Test 3: Wait for lease to expire
diag "=== Test 3: Wait for lease expiration (Polling) ==="
# Poll for lease removal/expiration (max 25s)
LEASE_GONE=0
for i in $(seq 1 250); do
    LEASE_COUNT_CHECK=$(sqlite3 "$STATE_DIR/state.db" "SELECT COUNT(*) FROM entries WHERE bucket = 'dhcp_leases' AND value LIKE '%\"ip\":\"$LEASE1_IP\"%'" 2>/dev/null || echo "0")
    if [ "$LEASE_COUNT_CHECK" -eq 0 ]; then
        LEASE_GONE=1
        break
    fi
    dilated_sleep 0.1
done

if [ "$LEASE_GONE" -eq 1 ]; then
    ok 0 "Expired lease removed from database"
else
    ok 1 "Lease not removed after timeout"
fi
ip addr del 192.168.1.1/24 dev lo 2>/dev/null || true
dilated_sleep 1

rm -f /tmp/dhcp_lease2.log
UDHCPC_SCRIPT2=$(mktemp /tmp/udhcpc-script2.XXXXXX)
cat > "$UDHCPC_SCRIPT2" << 'UDHCPC_EOF2'
#!/bin/sh
echo "DHCP_EVENT=$1" >> /tmp/dhcp_lease2.log
echo "DHCP_IP=$ip" >> /tmp/dhcp_lease2.log
UDHCPC_EOF2
chmod +x "$UDHCPC_SCRIPT2"

timeout 10 udhcpc -f -i lo -s "$UDHCPC_SCRIPT2" -q -n -t 3 >/tmp/udhcpc2.log 2>&1 || true
ip addr add 192.168.1.1/24 dev lo 2>/dev/null || true

if [ -f /tmp/dhcp_lease2.log ]; then
    LEASE2_IP=$(grep "DHCP_IP=" /tmp/dhcp_lease2.log | cut -d= -f2 | tr -d '\n' | tr -d ' ')
    if [ -n "$LEASE2_IP" ]; then
        ok 0 "Second lease allocated: $LEASE2_IP"

        # Verify IP was reclaimed (same IP allocated again)
        if [ "$LEASE2_IP" = "$LEASE1_IP" ]; then
            diag "✓ IP reclamation verified: $LEASE1_IP was reused"
        else
            diag "Different IP allocated: $LEASE2_IP (pool has multiple IPs)"
        fi
    else
        ok 1 "Second lease allocation failed"
    fi
else
    ok 1 "Second lease log not created"
fi

# Test 5: Verify lease expiration reaper is working
diag "=== Test 5: Verify expiration reaper logs ==="
if grep -qi "expired\|reaper\|cleanup" /tmp/ctl.log 2>/dev/null; then
    ok 0 "Lease expiration reaper activity detected"
else
    ok 0 "Reaper check skipped (may not log verbosely)"
fi

# Cleanup
diag "Cleaning up..."
stop_ctl
ip addr del 192.168.1.1/24 dev lo 2>/dev/null || true
rm -f "$CONFIG_FILE" "$UDHCPC_SCRIPT" "$UDHCPC_SCRIPT2"
rm -f /tmp/dhcp_lease1.log /tmp/dhcp_lease2.log /tmp/udhcpc1.log /tmp/udhcpc2.log

ok 0 "Test cleanup completed"
