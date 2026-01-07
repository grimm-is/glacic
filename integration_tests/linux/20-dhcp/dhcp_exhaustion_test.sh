#!/bin/sh
# DHCP Pool Exhaustion Integration Test
# Verifies behavior when the IP pool is completely full.

set -e
set -x

TEST_TIMEOUT=60
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
. "$SCRIPT_DIR/../common.sh"

if ! command -v sqlite3 >/dev/null 2>&1; then
    echo "1..0 # SKIP sqlite3 not found"
    exit 0
fi

CONFIG_FILE=$(mktemp /tmp/dhcp-exhaustion-config.hcl.XXXXXX)

# Helper to clean links
clean_links() {
    ip link del br-test 2>/dev/null || true
    ip link del veth-c1 2>/dev/null || true
    ip link del veth-c2 2>/dev/null || true
    ip link del veth-c3 2>/dev/null || true
}

clean_links

# Create Bridge for Server
ip link add name br-test type bridge
ip link set br-test up
ip addr add 192.168.1.1/24 dev br-test

# Create Clients (veth pairs)
# Client 1
ip link add veth-c1 type veth peer name veth-c1-br
ip link set veth-c1-br master br-test
ip link set veth-c1-br up
ip link set dev veth-c1 address 02:00:00:00:00:01
ip link set veth-c1 up

# Client 2
ip link add veth-c2 type veth peer name veth-c2-br
ip link set veth-c2-br master br-test
ip link set veth-c2-br up
ip link set dev veth-c2 address 02:00:00:00:00:02
ip link set veth-c2 up

# Client 3
ip link add veth-c3 type veth peer name veth-c3-br
ip link set veth-c3-br master br-test
ip link set veth-c3-br up
ip link set dev veth-c3 address 02:00:00:00:00:03
ip link set veth-c3 up

# Update config to use bridge
cat > "$CONFIG_FILE" << 'EOF'
schema_version = "1.1"

interface "br-test" {
  zone = "lan"
  ipv4 = ["192.168.1.1/24"]
}

zone "lan" {
  interfaces = ["br-test"]
}

dhcp {
  enabled = true

  scope "tiny_pool" {
    interface = "br-test"
    range_start = "192.168.1.100"
    range_end = "192.168.1.101"
    router = "192.168.1.1"
    lease_time = "1h"
  }
}
EOF

ok 0 "Created test config with tiny pool (2 IPs) on bridge"

# Start control plane
start_ctl "$CONFIG_FILE"
dilated_sleep 5 # increased wait

# Client 1
rm -f /tmp/lease1.log
UDHCPC_SCRIPT1=$(mktemp /tmp/script1.XXXXXX)
cat > "$UDHCPC_SCRIPT1" <<EOF
#!/bin/sh
if [ "\$1" = "bound" ]; then echo "IP=\$ip" > /tmp/lease1.log; fi
EOF
chmod +x "$UDHCPC_SCRIPT1"

diag "Requesting IP for Client 1 (veth-c1)..."
timeout 5 udhcpc -f -i veth-c1 -s "$UDHCPC_SCRIPT1" -q -n -t 3 >/dev/null 2>&1 || true

if grep -q "IP=" /tmp/lease1.log 2>/dev/null; then
    ok 0 "Client 1 got IP: $(cat /tmp/lease1.log)"
else
    ok 1 "Client 1 failed to get IP"
    diag "--- CTL LOG DUMP ---"
    cat "$CTL_LOG"
    diag "--- FIREWALL RULES ---"
    nft list ruleset
    diag "--- SOCKETS ---"
    ss -ulnp
    diag "--- INTERFACES ---"
    ip addr
fi

# Client 2
rm -f /tmp/lease2.log
UDHCPC_SCRIPT2=$(mktemp /tmp/script2.XXXXXX)
cat > "$UDHCPC_SCRIPT2" <<EOF
#!/bin/sh
if [ "\$1" = "bound" ]; then echo "IP=\$ip" > /tmp/lease2.log; fi
EOF
chmod +x "$UDHCPC_SCRIPT2"

diag "Requesting IP for Client 2 (veth-c2)..."
timeout 5 udhcpc -f -i veth-c2 -s "$UDHCPC_SCRIPT2" -q -n -t 3 >/dev/null 2>&1 || true

if grep -q "IP=" /tmp/lease2.log 2>/dev/null; then
    ok 0 "Client 2 got IP: $(cat /tmp/lease2.log)"
else
    ok 1 "Client 2 failed to get IP"
fi

# Client 3 (Should FAIL)
rm -f /tmp/lease3.log
UDHCPC_SCRIPT3=$(mktemp /tmp/script3.XXXXXX)
cat > "$UDHCPC_SCRIPT3" <<EOF
#!/bin/sh
if [ "\$1" = "bound" ]; then echo "IP=\$ip" > /tmp/lease3.log; fi
EOF
chmod +x "$UDHCPC_SCRIPT3"

diag "Requesting IP for Client 3 (veth-c3) - EXPECT FAILURE..."
# We expect this to execute but NOT write to log
timeout 5 udhcpc -f -i veth-c3 -s "$UDHCPC_SCRIPT3" -q -n -t 3 >/dev/null 2>&1 || true

if [ -f /tmp/lease3.log ]; then
    ok 1 "Client 3 got IP (UNEXPECTED): $(cat /tmp/lease3.log)"
    # Dump leases for debugging
    sqlite3 "$STATE_DIR/state.db" "SELECT value FROM entries WHERE bucket='dhcp_leases'"
else
    ok 0 "Client 3 failed to get IP (Expected: Pool Exhausted)"
fi

# Cleanup
stop_ctl
clean_links
rm -f "$CONFIG_FILE" "$UDHCPC_SCRIPT1" "$UDHCPC_SCRIPT2" "$UDHCPC_SCRIPT3"
rm -f /tmp/lease1.log /tmp/lease2.log /tmp/lease3.log

