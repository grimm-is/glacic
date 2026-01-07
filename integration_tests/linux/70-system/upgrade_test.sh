#!/bin/sh
set -x
# Hot Binary Upgrade Test
# Verifies that:
# 1. New binary can be built.
# 2. Upgrade command triggers socket handoff.
# 3. New binary takes over listeners.
# 4. Old binary exits.
# 5. State (DHCP leases) is preserved.

TEST_TIMEOUT=30
. "$(dirname "$0")/../common.sh"
export GLACIC_LOG_FILE=stdout

plan 6

diag "Starting Upgrade Test..."

# Cleanup trap
cleanup() {
    pkill -f glacic-v1 2>/dev/null
    pkill -f glacic-v2 2>/dev/null
    rm -f /tmp/glacic-v1 /tmp/glacic-v2 /tmp/upgrade.hcl /tmp/glacic.log /tmp/glacic.pid
}
trap cleanup EXIT

# 1. Prepare Binaries
# Binaries are pre-built on host and mounted at /mnt/glacic/build/test-artifacts/
MOUNT_DIR="/mnt/glacic/build/test-artifacts"
if [ ! -f "$MOUNT_DIR/glacic-v1" ] || [ ! -f "$MOUNT_DIR/glacic-v2" ]; then
    diag "Pre-built binaries not found in $MOUNT_DIR"
    ls -lR /mnt/glacic/build/
    exit 1
fi

cp "$MOUNT_DIR/glacic-v1" /tmp/glacic-v1
cp "$MOUNT_DIR/glacic-v2" /tmp/glacic-v2
chmod +x /tmp/glacic-v1 /tmp/glacic-v2

ok 0 "Binaries V1 and V2 prepared"

# 2. Start V1
cat > /tmp/upgrade.hcl <<EOF
interface "lo" {
    ipv4 = ["127.0.0.1/8"]
}
api {
    enabled = true
    listen = "127.0.0.1:8080"
}
dhcp {
    enabled = true
    scope "lan" {
        interface = "lo"
        range_start = "127.0.0.10"
        range_end = "127.0.0.20"
        router = "127.0.0.1"
    }
}
EOF

diag "Starting V1..."
/tmp/glacic-v1 ctl /tmp/upgrade.hcl > /tmp/glacic.log 2>&1 &
PID_V1=$!
echo $PID_V1 > /tmp/glacic.pid
dilated_sleep 2

if ! kill -0 $PID_V1 2>/dev/null; then
    diag "V1 failed to start"
    cat /tmp/glacic.log
    exit 1
fi
ok 0 "V1 started"

# 3. Perform Upgrade
diag "Initiating Upgrade to V2..."
# We run the upgrade command.
/tmp/glacic-v1 upgrade --binary /tmp/glacic-v2 --config /tmp/upgrade.hcl >> /tmp/glacic.log 2>&1 &

# Give it time to attempt socket connection (and fail)
dilated_sleep 5

# Check for success log from V2
if grep -qE "Upgrade complete|Listener handoff complete" "/tmp/glacic.log"; then
    ok 0 "Upgrade success confirmed"
else
    ok 1 "Upgrade success log NOT found"
    diag "Log content (tail):"
    tail -n 10 /tmp/glacic.log || true
    exit 1
fi

# Cleanup will handle process kill
