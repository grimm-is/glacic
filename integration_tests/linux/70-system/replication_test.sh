#!/bin/sh
set -x
# Replication Integration Test

TEST_TIMEOUT=120
. "$(dirname "$0")/../common.sh"
export GLACIC_LOG_FILE=stdout

plan 6

diag "Starting Replication Test..."

# Define state dirs
PRIM_DIR="/tmp/glacic_prim"
REPL_DIR="/tmp/glacic_repl"
rm -rf $PRIM_DIR $REPL_DIR
mkdir -p $PRIM_DIR/state
mkdir -p $REPL_DIR/state

# Setup Namespaces
# ns_prim: Primary Node
# ns_repl: Replica Node
# ns_cli:  DHCP Client (simulating user)
ip netns add ns_prim
ip netns add ns_repl
ip netns add ns_cli

# Bring up loopback in all namespaces
ip netns exec ns_prim ip link set lo up
ip netns exec ns_repl ip link set lo up
ip netns exec ns_cli ip link set lo up

cleanup() {
    pkill -F $PRIM_DIR/pid 2>/dev/null
    pkill -F $REPL_DIR/pid 2>/dev/null
    ip netns del ns_prim 2>/dev/null
    ip netns del ns_repl 2>/dev/null
    ip netns del ns_cli 2>/dev/null
    rm -rf $PRIM_DIR $REPL_DIR
    rm -f /tmp/primary.hcl /tmp/replica.hcl
}
trap cleanup EXIT

fail() {
    echo "### TEST FAILED: $1"
    echo "### PRIMARY LOG:"
    cat $PRIM_DIR/log
    echo "### REPLICA LOG 1:"
    cat $REPL_DIR/log
    echo "### REPLICA LOG 2:"
    cat $REPL_DIR/log2 2>/dev/null
    exit 1
}

# Network Topology:
# 1. Replication Link: v-prim (192.168.1.1) <---> v-repl (192.168.1.2)
ip link add v-prim type veth peer name v-repl
ip link set v-prim netns ns_prim
ip link set v-repl netns ns_repl

ip netns exec ns_prim ip addr add 192.168.1.1/24 dev v-prim
ip netns exec ns_prim ip link set v-prim up

ip netns exec ns_repl ip addr add 192.168.1.2/24 dev v-repl
ip netns exec ns_repl ip link set v-repl up

# 2. Client Link: v-sys (10.0.0.1) <---> v-cli (DHCP)
ip link add v-sys type veth peer name v-cli
ip link set v-sys netns ns_prim
ip link set v-cli netns ns_cli

ip netns exec ns_prim ip addr add 10.0.0.1/24 dev v-sys
ip netns exec ns_prim ip link set v-sys up

# Client side needs UP but no IP (will get via DHCP)
ip netns exec ns_cli ip link set v-cli up

# Configs
# Primary: Serves DHCP on v-sys (LAN). Replicates on v-prim (Sync).
cat > /tmp/primary.hcl <<EOF
interface "lo" {
    ipv4 = ["127.0.0.1/8"]
}
interface "v-prim" {
    ipv4 = ["192.168.1.1/24"]
    zone = "sync"
}
interface "v-sys" {
    ipv4 = ["10.0.0.1/24"]
    zone = "lan"
}
zone "lan" {
    services {
        dhcp = true
        dns = true
    }
}
zone "sync" {
    management {
        api = true
    }
}
policy "lan" "firewall" {
    name = "lan_to_firewall"
    action = "accept"
}
policy "sync" "Firewall" {
    name = "sync_to_firewall"
    action = "accept"
}
api {
    enabled = true
    listen = "192.168.1.1:8080"
}
dhcp {
    enabled = true
    scope "client-lan" {
        interface = "v-sys"
        range_start = "10.0.0.100"
        range_end = "10.0.0.200"
        router = "10.0.0.1"
    }
}
replication {
    mode = "primary"
    listen_addr = "192.168.1.1:9001"
}
state_dir = "/tmp/glacic_prim/state"
EOF

# Replica: Connects to Primary.
cat > /tmp/replica.hcl <<EOF
interface "lo" {
    ipv4 = ["127.0.0.1/8"]
}
interface "v-repl" {
    ipv4 = ["192.168.1.2/24"]
    zone = "sync"
}
zone "sync" {
    management {
        api = true
    }
}
policy "sync" "Firewall" {
    name = "sync_to_firewall"
    action = "accept"
}
api {
    enabled = true
    listen = "192.168.1.2:8080"
}
# Replica config matches Primary for logic, but normally wouldn't serve DHCP if active-passive?
# For now, enabled=true but interface v-sys doesn't exist on Replica so it won't bind DHCP there.
# It might log errors.
# To verify Sync, we just need the DB to update.
replication {
    mode = "replica"
    primary_addr = "192.168.1.1:9001"
}
state_dir = "/tmp/glacic_repl/state"

dhcp {
    enabled = true
    scope "verify_state" {
        interface = "lo"
        range_start = "127.0.0.10"
        range_end = "127.0.0.20"
        router = "127.0.0.1"
    }
}
EOF

# Start Primary
diag "Starting Primary..."
export GLACIC_CTL_SOCKET="$PRIM_DIR/ctl.sock"
ip netns exec ns_prim $APP_BIN ctl --state-dir $PRIM_DIR/state /tmp/primary.hcl > $PRIM_DIR/log 2>&1 &
PID_PRIM=$!
echo $PID_PRIM > $PRIM_DIR/pid
dilated_sleep 2

# Verify Primary Started
if ! kill -0 $PID_PRIM 2>/dev/null; then
    diag "Primary failed to start"
    cat $PRIM_DIR/log
    exit 1
fi
ok 0 "Primary started"

# Start DHCP Client in ns_cli
diag "Requesting DHCP lease..."
ip netns exec ns_cli udhcpc -i v-cli -n -q -f -t 15 > $PRIM_DIR/client.log 2>&1 || true

# Check if client got IP
ip netns exec ns_cli ip addr show v-cli | grep "10.0.0." >/dev/null 2>&1
if [ $? -eq 0 ]; then
    ok 0 "Client acquired Lease"
else
    ok 1 "Client acquired Lease (Failed)"
    diag "DHCP failed, falling back to static IP for connectivity test..."
    ip netns exec ns_cli ip addr add 10.0.0.100/24 dev v-cli
    ip netns exec ns_cli ip route add default via 10.0.0.1
fi

dilated_sleep 2

# Debug: Inspect Primary DB
echo "### DEBUG: Primary DB Stats"
ls -l $PRIM_DIR/state/
if command -v sqlite3 >/dev/null 2>&1; then
    sqlite3 $PRIM_DIR/state/state.db "SELECT 'Buckets:', count(*) FROM buckets;"
    sqlite3 $PRIM_DIR/state/state.db "SELECT 'Entries:', count(*) FROM entries;"
    sqlite3 $PRIM_DIR/state/state.db "SELECT * FROM entries;"
else
    echo "sqlite3 not found"
fi

grep "Allocated lease" $PRIM_DIR/log >/dev/null 2>&1
# ok $? "Primary recorded allocation"

# Helper: Wait for log entry
wait_for_log() {
    pattern="$1"
    logfile="$2"
    timeout=${3:-30}

    diag "Waiting for '$pattern' in $(basename "$logfile")..."
    count=0
    while ! grep -q "$pattern" "$logfile" 2>/dev/null; do
        sleep 1
        count=$((count + 1))
        if [ $count -ge $timeout ]; then
            fail "Timeout waiting for '$pattern' in $logfile"
        fi
    done
}

# Start Replica
diag "Starting Replica..."
export GLACIC_CTL_SOCKET="$REPL_DIR/ctl.sock"
ip netns exec ns_repl $APP_BIN ctl --state-dir $REPL_DIR/state /tmp/replica.hcl > $REPL_DIR/log 2>&1 &
PID_REPL=$!
echo $PID_REPL > $REPL_DIR/pid

# Check Sync (Poll instead of sleep)
wait_for_log "Connected to primary" "$REPL_DIR/log"
ok 0 "Replica connected to Primary"

wait_for_log "Applied incremental changes\|Restored snapshot" "$REPL_DIR/log"
ok 0 "Replica received sync data"

# Restart Replica to verify persistence
kill $PID_REPL
wait $PID_REPL 2>/dev/null
diag "Restarting Replica..."
ip netns exec ns_repl $APP_BIN ctl --state-dir $REPL_DIR/state /tmp/replica.hcl > $REPL_DIR/log2 2>&1 &
PID_REPL2=$!
echo $PID_REPL2 > $REPL_DIR/pid

# Check loaded leases (Poll)
wait_for_log "Loaded .* leases from state store" "$REPL_DIR/log2"
ok 0 "Replica loaded state from DB"

# Verify count > 0.
grep -E "Loaded [1-9][0-9]* leases" $REPL_DIR/log2 >/dev/null 2>&1
if [ $? -ne 0 ]; then
    fail "Replica loaded 0 leases (expected >= 1)"
fi
ok 0 "Replica has >= 1 lease"

exit 0
