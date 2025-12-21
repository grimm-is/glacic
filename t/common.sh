#!/bin/sh

# Common functions and environment setup for Glacic Firewall tests
# Handles dynamic branding resolution and common utilities
#
# TIMEOUT POLICY: Tests should complete in <15 seconds
# If your test needs more time, set at the top of your test:
#   TEST_TIMEOUT=30  # seconds
#
# CLEANUP: Always use cleanup_on_exit or manually kill background processes

# Dynamic Branding Resolution
# We need to find the project root and read brand.json
# In the VM, the project is mounted at /mnt/<brand_lower> or /mnt/glacic usually.
# But we can find it relative to this script.

# Determine script directory
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# Project root is usually 1 level up from t/
# But inside VM, we might be running from /mnt/glacic/t/
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

BRAND_ENV="$PROJECT_ROOT/internal/brand/brand.env"

if [ -f "$BRAND_ENV" ]; then
    # Source the generated brand.env file (much cleaner than parsing JSON)
    . "$BRAND_ENV"

    # Map BRAND_* vars to our expected names
    BRAND_NAME="${BRAND_NAME:-Glacic}"
    BRAND_LOWER="${BRAND_LOWER_NAME:-glacic}"
    BINARY_NAME="${BRAND_BINARY_NAME:-glacic}"
    STATE_DIR="${BRAND_DEFAULT_STATE_DIR:-/var/lib/glacic}"
    LOG_DIR="${BRAND_DEFAULT_LOG_DIR:-/var/log/glacic}"
    RUN_DIR="${BRAND_DEFAULT_RUN_DIR:-/var/run}"
    SOCKET_NAME="${BRAND_SOCKET_NAME:-ctl.sock}"
else
    # Fallback defaults if brand.env doesn't exist (run 'make brand-env' to generate)
    BRAND_NAME="Glacic"
    BRAND_LOWER="glacic"
    BINARY_NAME="glacic"
    STATE_DIR="/var/lib/glacic"
    LOG_DIR="/var/log/glacic"
    RUN_DIR="/var/run"
    SOCKET_NAME="ctl.sock"
fi

# Construct CTL_SOCKET path: /var/run/glacic-ctl.sock
CTL_SOCKET="$RUN_DIR/$BRAND_LOWER-$SOCKET_NAME"

# Set common variables
MOUNT_PATH="/mnt/$BRAND_LOWER"
APP_BIN="$MOUNT_PATH/build/$BINARY_NAME"
# Fallback to just binary name if building in path or typical location
if [ ! -x "$APP_BIN" ]; then
    # Maybe we are running outside VM or path is different?
    # Try finding it in build dir relative to project root
    APP_BIN="$PROJECT_ROOT/build/$BINARY_NAME"
fi

# ============================================================================
# Global State Reset (Ensure clean slate for each test)
# ============================================================================
reset_state() {
    # Only run in VM environment (check for nft/ip availability)
    if [ "$(id -u)" -eq 0 ]; then
        # Flush nftables to remove any leftover rules from previous tests
        if command -v nft >/dev/null 2>&1; then
             nft flush ruleset 2>/dev/null || true
        fi
        
        # Cleanup test namespaces and interfaces
        ip netns del test_client 2>/dev/null || true
        ip netns del test_server 2>/dev/null || true
        ip link del veth-lan 2>/dev/null || true
        ip link del veth-wan 2>/dev/null || true
        
        # Reset IP forwarding
        echo 0 > /proc/sys/net/ipv4/ip_forward 2>/dev/null || true
        
        # Cleanup State Directory (prevents false positive crash loops)
        # The crash loop detector looks at recent restarts. We must wipe this memory.
        rm -rf "$STATE_DIR"/* 2>/dev/null
        rm -f "/var/run/glacic"*.pid 2>/dev/null

        # Ensure tun device exists (for VPN tests)
        if [ ! -c /dev/net/tun ]; then
            mkdir -p /dev/net
            mknod /dev/net/tun c 10 200 2>/dev/null || true
        fi
    fi
}
# Execute reset immediately
reset_state

# Export for tests
export BRAND_NAME
export BRAND_LOWER
export BINARY_NAME
export MOUNT_PATH
export APP_BIN
export STATE_DIR
export LOG_DIR
export RUN_DIR
export CTL_SOCKET
# Also export as GLACIC_* for the Go binary to pick up
export GLACIC_STATE_DIR="$STATE_DIR"
export GLACIC_LOG_DIR="$LOG_DIR"
export GLACIC_RUN_DIR="$RUN_DIR"
export GLACIC_CTL_SOCKET="$CTL_SOCKET"

# Parse kernel parameters
if grep -q "glacic.run_skipped=1" /proc/cmdline 2>/dev/null; then
    export GLACIC_RUN_SKIPPED=1
fi

# Debug output (only shown if DEBUG=1)
if [ "${DEBUG:-0}" = "1" ]; then
    echo "# Debug: PROJECT_ROOT=$PROJECT_ROOT"
    echo "# Debug: APP_BIN=$APP_BIN"
fi

# ============================================================================
# Process Management - CRITICAL for avoiding test hangs
# ============================================================================

# Track background PIDs for cleanup
BACKGROUND_PIDS=""

# Register a PID for cleanup on exit
track_pid() {
    BACKGROUND_PIDS="$BACKGROUND_PIDS $1"
}

# Kill all tracked background processes
cleanup_processes() {
    for pid in $BACKGROUND_PIDS; do
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null
            # Give it a moment, then force kill
            sleep 0.5
            kill -9 "$pid" 2>/dev/null || true
        fi
    done
    BACKGROUND_PIDS=""
}

# Set up automatic cleanup on exit (call this at start of test)
cleanup_on_exit() {
    trap cleanup_processes EXIT INT TERM
}

# ============================================================================
# Common Test Functions (TAP)
# ============================================================================
test_count=0
failed_count=0

plan() {
    echo "1..$1"
}

ok() {
    test_count=$((test_count + 1))
    if [ "$1" -eq 0 ]; then
        echo "ok $test_count - $2"
    else
        echo "not ok $test_count - $2"
        failed_count=$((failed_count + 1))
    fi
}

diag() {
    echo "# $1"
}

skip() {
    test_count=$((test_count + 1))
    echo "ok $test_count - # SKIP $1"
}

pass() {
    test_count=$((test_count + 1))
    echo "ok $test_count - $1"
}

# Helper to require root
require_root() {
    if [ "$(id -u)" -ne 0 ]; then
        echo "1..0 # SKIP Must run as root"
        exit 0
    fi
}

# Helper to require firewall binary
require_binary() {
    if [ ! -x "$APP_BIN" ] && ! command -v "$APP_BIN" >/dev/null 2>&1; then
        echo "1..0 # SKIP firewall binary not found at $APP_BIN"
        exit 0
    fi
}

# Helper for temp files
mktemp_compatible() {
    suffix="${1:-tmp}"
    echo "/tmp/test_${$}_$(date +%s)_$suffix"
}

# ============================================================================
# Fail-Fast Helpers
# ============================================================================

# Immediately fail the test with a message
fail() {
    echo "not ok - FATAL: $1"
    cleanup_processes
    sleep 1 # Ensure output flushes before exit
    exit 1
}

# Assert a condition, fail if false
assert() {
    if ! eval "$1"; then
        fail "Assertion failed: $1"
    fi
}

# Run a command with a timeout (default 5 seconds)
# Usage: run_with_timeout <timeout_seconds> <command...>
# Returns: command exit code, or 124 if timed out
run_with_timeout() {
    _timeout="$1"
    shift

    # timeout command is available on most systems
    if command -v timeout >/dev/null 2>&1; then
        timeout "$_timeout" "$@"
        return $?
    fi

    # Fallback: run in background and kill if needed
    "$@" &
    _pid=$!
    _i=0
    while [ $_i -lt "$_timeout" ]; do
        if ! kill -0 $_pid 2>/dev/null; then
            wait $_pid
            return $?
        fi
        sleep 1
        _i=$((_i + 1))
    done

    # Timed out - kill the process
    kill -9 $_pid 2>/dev/null
    wait $_pid 2>/dev/null
    echo "# TIMEOUT: Command timed out after ${_timeout}s: $*"
    return 124
}

# ============================================================================
# Process Startup Helpers (with timeout detection)
# ============================================================================

# Wait for a port to be listening (max 5 seconds by default)
wait_for_port() {
    _port="$1"
    _timeout="${2:-5}"
    _i=0
    _max=$((_timeout * 5))  # 5 checks per second (0.2s sleep)

    while [ $_i -lt $_max ]; do
        # Try nc first, then netstat as fallback
        if nc -z 127.0.0.1 "$_port" 2>/dev/null; then
            return 0
        elif netstat -tln 2>/dev/null | grep -q ":$_port "; then
            return 0
        elif ss -tln 2>/dev/null | grep -q ":$_port "; then
            return 0
        fi
        sleep 0.2
        _i=$((_i + 1))
    done

    echo "# TIMEOUT waiting for port $_port after ${_timeout}s"
    return 1
}

# Wait for a file to exist (max 5 seconds by default)
wait_for_file() {
    _file="$1"
    _timeout="${2:-5}"
    _i=0
    _max=$((_timeout * 5))  # 5 checks per second (0.2s sleep)

    while [ $_i -lt $_max ]; do
        if [ -e "$_file" ]; then
            return 0
        fi
        sleep 0.2
        _i=$((_i + 1))
    done

    echo "# TIMEOUT waiting for file $_file after ${_timeout}s"
    return 1
}

# Wait for a command to succeed (max 5 seconds by default)
# Usage: wait_for_condition "command" [timeout_seconds]
wait_for_condition() {
    _cmd="$1"
    _timeout="${2:-5}"
    _i=0
    _max=$((_timeout * 5))  # 5 checks per second (0.2s sleep)

    while [ $_i -lt $_max ]; do
        if eval "$_cmd"; then
            return 0
        fi
        sleep 0.2
        _i=$((_i + 1))
    done

    echo "# TIMEOUT waiting for condition: $_cmd after ${_timeout}s"
    return 1
}

# Start the control plane and track its PID
# Usage: start_ctl <config_file> [extra_args...]
# Sets: CTL_PID, CTL_LOG
start_ctl() {
    _config="$1"
    shift
    CTL_LOG=$(mktemp_compatible ctl.log)

    diag "Starting control plane with config: $_config"

    # Pre-check configuration (if binary supports it)
    if $APP_BIN --help 2>&1 | grep -q "check"; then
        if ! $APP_BIN check "$_config" >> "$CTL_LOG" 2>&1; then
            echo "# Config check failed:"
            sed 's/^/# /' "$CTL_LOG"
            fail "Config check failed for $_config"
        fi
    fi

    $APP_BIN ctl "$_config" "$@" > "$CTL_LOG" 2>&1 &
    CTL_PID=$!
    track_pid $CTL_PID

    # Wait for socket to appear (control plane needs socket to accept CLI commands)
    # Use CTL_SOCKET which is derived from brand settings
    if ! wait_for_file "$CTL_SOCKET" 10; then
        echo "# Control plane failed to create socket:"
        cat "$CTL_LOG" | head -30
        fail "Control plane socket $CTL_SOCKET did not appear within 10 seconds"
    fi

    # Verify process is still alive after socket appears
    if ! kill -0 $CTL_PID 2>/dev/null; then
        echo "# Control plane crashed after creating socket:"
        cat "$CTL_LOG" | head -30
        fail "Control plane crashed"
    fi

    diag "Control plane started (PID $CTL_PID, socket $CTL_SOCKET)"
}

# Stop the control plane
stop_ctl() {
    if [ -n "$CTL_PID" ]; then
        diag "Stopping control plane (PID $CTL_PID)..."
        kill $CTL_PID 2>/dev/null || true
        wait $CTL_PID 2>/dev/null || true
    fi
}

# Start the API server and track its PID
# Usage: start_api [extra_args...]
# Sets: API_PID, API_LOG
# Uses 'test-api' command which runs API without sandbox isolation
start_api() {
    API_LOG=$(mktemp_compatible api.log)

    $APP_BIN test-api "$@" > "$API_LOG" 2>&1 &
    API_PID=$!
    track_pid $API_PID

    # Wait for API to be ready (port 8080)
    if ! wait_for_port 8080 10; then
        echo "# API server failed to start:"
        cat "$API_LOG" | head -20
    fi
}

# Login to API (returns token)
# Usage: TOKEN=$(login_api)
login_api() {
    curl -k -s -X POST https://127.0.0.1:8443/api/auth/login \
        -H "Content-Type: application/json" \
        -d '{"username":"admin","password":"password"}' | \
        grep -o '"token":"[^"]*"' | cut -d'"' -f4
}


# ============================================================================
# HTTP Helpers
# ============================================================================

# Simple HTTP GET, returns body (fails test on non-2xx)
http_get() {
    _url="$1"
    _response=""
    _response=$(curl -sf "$_url" 2>/dev/null) || fail "HTTP GET failed: $_url"
    echo "$_response"
}

# HTTP GET that just checks for success (2xx)
http_ok() {
    _url="$1"
    curl -sf -o /dev/null "$_url" 2>/dev/null
}

# ============================================================================
# Firewall Rule Application (fire-and-forget)
# ============================================================================

# Apply firewall rules using test mode (synchronous, exits after applying)
# Usage: apply_firewall_rules <config_file> [log_file]
# Returns: 0 on success, 1 on failure
apply_firewall_rules() {
    _config_file="$1"
    _log_file="${2:-/tmp/firewall_apply.log}"

    diag "Applying firewall rules from $_config_file..."
    $APP_BIN test "$_config_file" > "$_log_file" 2>&1
    _exit_code=$?

    if [ $_exit_code -eq 0 ]; then
        diag "Firewall rules applied successfully"
        return 0
    else
        diag "Failed to apply firewall rules (exit=$_exit_code)"
        diag "Log output:"
        cat "$_log_file" | head -30
        return 1
    fi
}

# ============================================================================
# Network Namespace Helpers (for traffic behavior tests)
# ============================================================================

# Create test topology: [client_ns]--veth-lan--[router]--veth-wan--[server_ns]
# Router is the host (where glacic runs), client simulates LAN, server simulates WAN
setup_test_topology() {
    diag "Setting up network namespace test topology..."

    # Create namespaces
    ip netns add test_client 2>/dev/null || true
    ip netns add test_server 2>/dev/null || true

    # Client (LAN) veth pair
    ip link add veth-client type veth peer name veth-lan 2>/dev/null || true
    ip link set veth-client netns test_client
    ip addr add 192.168.100.1/24 dev veth-lan 2>/dev/null || true
    ip link set veth-lan up

    # Server (WAN) veth pair
    ip link add veth-server type veth peer name veth-wan 2>/dev/null || true
    ip link set veth-server netns test_server
    ip addr add 10.99.99.1/24 dev veth-wan 2>/dev/null || true
    ip link set veth-wan up

    # Configure client namespace (LAN side)
    ip netns exec test_client ip addr add 192.168.100.100/24 dev veth-client
    ip netns exec test_client ip link set veth-client up
    ip netns exec test_client ip link set lo up
    ip netns exec test_client ip route add default via 192.168.100.1

    # Configure server namespace (WAN side)
    ip netns exec test_server ip addr add 10.99.99.100/24 dev veth-server
    ip netns exec test_server ip link set veth-server up
    ip netns exec test_server ip link set lo up
    ip netns exec test_server ip route add default via 10.99.99.1

    # Enable IP forwarding on the host (router)
    echo 1 > /proc/sys/net/ipv4/ip_forward

    diag "Test topology created: client(192.168.100.100) <-> router <-> server(10.99.99.100)"
}

teardown_test_topology() {
    diag "Tearing down test topology..."
    ip netns del test_client 2>/dev/null || true
    ip netns del test_server 2>/dev/null || true
    ip link del veth-lan 2>/dev/null || true
    ip link del veth-wan 2>/dev/null || true
}

# Run command in client namespace (LAN side)
run_client() {
    ip netns exec test_client "$@"
}

# Run command in server namespace (WAN side)
run_server() {
    ip netns exec test_server "$@"
}

# ============================================================================
# Log Verification Helpers
# ============================================================================

# Clear kernel log buffer (for fresh log capture)
clear_kernel_log() {
    dmesg -C 2>/dev/null || true
}

# Check if kernel log contains expected message pattern
# Usage: check_log_contains "PATTERN" "description"
# Returns: 0 if found, 1 if not
check_log_contains() {
    _pattern="$1"
    _description="${2:-Log contains pattern}"

    if dmesg | grep -qE "$_pattern"; then
        return 0
    else
        diag "Log pattern not found: $_pattern"
        return 1
    fi
}

# Wait for log message to appear (with timeout)
# Usage: wait_for_log "PATTERN" [timeout_seconds]
wait_for_log() {
    _pattern="$1"
    _timeout="${2:-5}"
    _i=0

    while [ $_i -lt $_timeout ]; do
        if dmesg | grep -qE "$_pattern"; then
            return 0
        fi
        sleep 1
        _i=$((_i + 1))
    done

    diag "TIMEOUT waiting for log: $_pattern"
    return 1
}

# Functions are automatically available after sourcing
