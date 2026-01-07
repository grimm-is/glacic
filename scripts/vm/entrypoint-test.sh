#!/bin/sh

# VM Test Entrypoint - TAP Output Mode
# Outputs clean TAP format for host-side prove consumption
# All diagnostic output goes to stderr, TAP to stdout

# Brand mount path (matches glacic-builder)
MOUNT_PATH="/mnt/glacic"
BUILD_PATH="$MOUNT_PATH/build"

trap "poweroff" EXIT

# Mount shared folder if not already mounted
if ! mountpoint -q "$MOUNT_PATH" 2>/dev/null; then
    # Mount project root as read-only
    mount -t 9p -o trans=virtio,version=9p2000.L,ro host_share "$MOUNT_PATH" 2>/dev/null || true
fi

# Mount build directory as writable (overlays the read-only mount)
if ! mountpoint -q "$BUILD_PATH" 2>/dev/null; then
    mkdir -p "$BUILD_PATH" 2>/dev/null || true
    mount -t 9p -o trans=virtio,version=9p2000.L build_share "$BUILD_PATH" 2>/dev/null || true
fi

cd "$MOUNT_PATH"

# Set up Go environment
export GOPATH=/tmp/go
export GOCACHE=/tmp/go-cache
export HOME=/root
mkdir -p "$GOPATH" "$GOCACHE" 2>/dev/null

# Diagnostic info to stderr
echo "# VM booted: $(uname -r)" >&2
echo "# Alpine: $(cat /etc/alpine-release 2>/dev/null || echo 'unknown')" >&2

# Determine which test to run based on TEST_NAME env or arg
TEST_NAME="${1:-all}"

case "$TEST_NAME" in
    go)
        # Run Go tests with TAP output
        echo "# Running Go tests..." >&2
        "$MOUNT_PATH/tests/go_tap_test.sh"
        ;;
    nftables)
        "$MOUNT_PATH/tests/nftables_test.sh"
        ;;
    qos)
        "$MOUNT_PATH/tests/qos_test.sh"
        ;;
    protection)
        "$MOUNT_PATH/tests/protection_test.sh"
        ;;
    conntrack)
        "$MOUNT_PATH/tests/conntrack_helpers_test.sh"
        ;;
    all)
        # Run all tests sequentially, output combined TAP
        "$MOUNT_PATH/tests/tap_runner.sh"
        ;;
    *)
        echo "Unknown test: $TEST_NAME" >&2
        exit 1
        ;;
esac
