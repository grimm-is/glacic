#!/bin/sh

# Firewall VM Entrypoint for Zone Testing

# Brand mount path (matches glacic-builder)
MOUNT_PATH="/mnt/glacic"

trap "poweroff" EXIT

echo "===== VM START ====="

echo ">> Starting Glacic Firewall with Zone Configuration..."

if [ -f "$MOUNT_PATH/tests/firewall_zone_test.sh" ]; then
    sh "$MOUNT_PATH/tests/firewall_zone_test.sh"
    TEST_EXIT_CODE=$?
else
    echo "❌ Firewall zone test harness not found!"
    TEST_EXIT_CODE=1
fi

echo "===== VM EXIT: $TEST_EXIT_CODE ====="

# Check if running in automated test mode
if cat /proc/cmdline | grep -q "autotest=true"; then
    echo "⚡ Automated test mode detected. Shutting down..."
    exit $TEST_EXIT_CODE
fi

# Keep firewall running for client VM tests - don't power off
while true; do
    sleep 60
done
