#!/bin/sh

# Client VM Test Script for Zone Testing
# This script runs on client VMs to test zone-based connectivity

# Brand mount path (matches glacic-builder)
MOUNT_PATH="/mnt/glacic"

trap "poweroff" EXIT

echo "===== VM START ====="

# Get zone name from kernel command line
ZONE_NAME=$(cat /proc/cmdline | tr ' ' '\n' | grep client_vm | cut -d= -f2)
echo ">> Starting Zone Client Test for: $ZONE_NAME"

if [ -f "$MOUNT_PATH/tests/zone_client_test.sh" ]; then
    sh "$MOUNT_PATH/tests/zone_client_test.sh" "$ZONE_NAME"
    TEST_EXIT_CODE=$?
else
    echo "❌ Zone client test harness not found!"
    TEST_EXIT_CODE=1
fi

echo "===== VM EXIT: $TEST_EXIT_CODE ====="
