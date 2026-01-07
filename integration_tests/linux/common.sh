#!/bin/sh

# Common functions and environment setup for Glacic Firewall tests
# Handles dynamic branding resolution and common utilities
#
# TIMEOUT POLICY: Tests should complete in <15 seconds
# If your test needs more time, set at the top of your test:
#   TEST_TIMEOUT=30  # seconds
#
# TIME_DILATION: The agent exports TIME_DILATION (e.g., "1.00" or "2.50")
# based on CPU performance. Use scale_timeout to scale durations accordingly.
#
# CLEANUP: Always use cleanup_on_exit or manually kill background processes

# Scale a timeout value by TIME_DILATION factor (returns integer)
# Usage: scaled=$(scale_timeout 30)
# If TIME_DILATION is unset or invalid, defaults to 1 (no scaling)
# Validate TIME_DILATION once at startup
case "${TIME_DILATION:-}" in
    ''|*[!0-9.]*) TIME_DILATION=1 ;;
esac

# Scale a timeout value (seconds) by TIME_DILATION
# Usage: local timeout=$(scale_timeout 5)
# Scale a timeout value (seconds) by TIME_DILATION
# Usage: local timeout=$(scale_timeout 5)
scale_timeout() {
    local base="$1"
    # Use awk for floating point math
    echo "$base $TIME_DILATION" |
        awk '{ r = $1 * $2; print r }'
}

# Sleep for a dilated duration (supports floats)
# Usage: dilated_sleep 0.5
dilated_sleep() {
    local base="$1"
    sleep $(scale_timeout "$base")
}

# Dynamic Branding Resolution
# We need to find the project root and read brand.json
# In the VM, the project is mounted at /mnt/<brand_lower> or /mnt/glacic usually.
# But we can find it relative to this script.

# Determine script directory
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Project root detection (git-based with fallback)
if git rev-parse --show-toplevel >/dev/null 2>&1; then
    PROJECT_ROOT="$(git rev-parse --show-toplevel)"
else
    # Fallback: traverse up until we find .git or reach root
    _dir="$SCRIPT_DIR"
    while [ "$_dir" != "/" ]; do
        if [ -d "$_dir/.git" ]; then
            PROJECT_ROOT="$_dir"
            break
        fi
        _dir="$(dirname "$_dir")"
    done
    # Final fallback if no .git found (e.g. inside VM mount)
    PROJECT_ROOT="${PROJECT_ROOT:-$(dirname "$(dirname "$SCRIPT_DIR")")}"
fi

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
# Set common variables
MOUNT_PATH="/mnt/$BRAND_LOWER"
RAW_ARCH=$(uname -m)

# Normalize ARCH to match Go/Build naming (arm64, amd64)
case "$RAW_ARCH" in
    aarch64) ARCH="arm64" ;;
    x86_64)  ARCH="amd64" ;;
    *)       ARCH="$RAW_ARCH" ;;
esac

# Prefer architecture-specific linux binary
if [ -x "$MOUNT_PATH/build/${BINARY_NAME}-linux-${ARCH}" ]; then
    APP_BIN="$MOUNT_PATH/build/${BINARY_NAME}-linux-${ARCH}"
elif [ -x "$PROJECT_ROOT/build/${BINARY_NAME}-linux-${ARCH}" ]; then
    APP_BIN="$PROJECT_ROOT/build/${BINARY_NAME}-linux-${ARCH}"
else
    # Fallback to standard name
    APP_BIN="$MOUNT_PATH/build/$BINARY_NAME"
    if [ ! -x "$APP_BIN" ]; then
        APP_BIN="$PROJECT_ROOT/build/$BINARY_NAME"
    fi
fi

# ============================================================================
# Minimal Utility Wrappers (Fallback to toolbox)
# ============================================================================
# Prefer architecture-specific linux binary (like APP_BIN)
if [ -x "$MOUNT_PATH/build/toolbox-linux-${ARCH}" ]; then
    TOOLBOX_BIN="$MOUNT_PATH/build/toolbox-linux-${ARCH}"
elif [ -x "$PROJECT_ROOT/build/toolbox-linux-${ARCH}" ]; then
    TOOLBOX_BIN="$PROJECT_ROOT/build/toolbox-linux-${ARCH}"
else
    # Fallback to standard name (for local testing)
    TOOLBOX_BIN="$MOUNT_PATH/build/toolbox"
    if [ ! -x "$TOOLBOX_BIN" ]; then
        TOOLBOX_BIN="$PROJECT_ROOT/build/toolbox"
    fi
fi

link_toolbox() {
    # If binary missing, use toolbox
    if ! command -v "$1" >/dev/null 2>&1; then
        eval "$1() { \"$TOOLBOX_BIN\" \"$1\" \"\$@\"; }"
    fi
}

link_toolbox dig
link_toolbox nc
link_toolbox jq
link_toolbox curl

# Pre-determine the best port checker? No, try all for robustness.
# Some environments might fail nc (IPv4 vs IPv6) but succeed with netstat.
check_port() {
    # Try nc (fastest)
    if command -v nc >/dev/null 2>&1; then
        if nc -z -w 1 127.0.0.1 "$1" 2>/dev/null; then return 0; fi
    fi
    # Fallback to netstat
    if command -v netstat >/dev/null 2>&1; then
        if netstat -tln 2>/dev/null | grep -q ":$1 "; then return 0; fi
    fi
    # Fallback to ss
    if command -v ss >/dev/null 2>&1; then
        if ss -tln 2>/dev/null | grep -q ":$1 "; then return 0; fi
    fi
    return 1
}

# ============================================================================
# Test Configuration Defaults
# ============================================================================

# Default API port for tests (can be overridden per-test)
# Currently one app per VM, so static port is fine. Variable for future flexibility.
TEST_API_PORT="${TEST_API_PORT:-8080}"

# ============================================================================
# Network Setup Helpers
# ============================================================================

# Enable IP forwarding (required for routing/NAT tests)
# Usage: enable_ip_forward
enable_ip_forward() {
    echo 1 > /proc/sys/net/ipv4/ip_forward
    echo 1 > /proc/sys/net/ipv6/conf/all/forwarding 2>/dev/null || true
}

# ============================================================================
# File/Log Polling Helpers
# ============================================================================

# Wait for a string to appear in a log file
# Usage: wait_for_log_entry "$LOG_FILE" "pattern" [timeout_sec]
wait_for_log_entry() {
    local log_file="$1"
    local pattern="$2"
    local timeout=$(scale_timeout "${3:-10}")
    local i=0
    local max=$((timeout * 5))  # 5 checks per second

    while [ $i -lt $max ]; do
        if grep -qE "$pattern" "$log_file" 2>/dev/null; then
            return 0
        fi
        sleep 0.2
        i=$((i + 1))
    done
    diag "TIMEOUT waiting for '$pattern' in $log_file"
    return 1
}

# ============================================================================
# HCL Configuration Templates
# ============================================================================

# Generate minimal HCL config with standard loopback
# Usage: config=$(minimal_config)
# Returns HCL string that can be written to a file
minimal_config() {
    cat <<'EOF'
schema_version = "1.0"

interface "lo" {
    ipv4 = ["127.0.0.1/8"]
}

zone "local" {}
EOF
}

# Generate minimal HCL config with API enabled
# Usage: config=$(minimal_config_with_api [port])
minimal_config_with_api() {
    local port="${1:-$TEST_API_PORT}"
    cat <<EOF
schema_version = "1.0"

api {
    enabled = true
    listen = "0.0.0.0:$port"
    require_auth = false
}

interface "lo" {
    ipv4 = ["127.0.0.1/8"]
}

zone "local" {}
EOF
}
# ============================================================================
# Global State Reset (Ensure clean slate for each test)
# ============================================================================
reset_state() {
    # Suppress output to prevent serial buffer deadlocks
    case $- in
        *x*) TRACE_WAS_ON=1 ;;
    esac
    set +x

    # Only run in VM environment (check for nft/ip availability)
    if [ "$(id -u)" -eq 0 ]; then
        # Use the dedicated cleanup script if available
        # Ensure we target the exact binary name to avoid killing agents
        export BINARY_NAME=$(basename "$APP_BIN")
        # Use MOUNT_PATH (not PROJECT_ROOT) because PROJECT_ROOT is derived from
        # the test directory and may be wrong (e.g., /mnt/glacic/t instead of /mnt/glacic)
        CLEANUP_SCRIPT="$MOUNT_PATH/scripts/vm_cleanup.sh"
        if [ -x "$CLEANUP_SCRIPT" ]; then
            "$CLEANUP_SCRIPT" >/dev/null 2>&1
            # Fallback inline cleanup if script not found (e.g. during minimal mount)
            pkill -9 -f "$BINARY_NAME" 2>/dev/null || true
            # Wait for processes to actually exit
            for i in $(seq 1 20); do
                if ! pgrep -f "$BINARY_NAME" >/dev/null; then break; fi
                sleep 0.1
            done

            # Kill potential leaking test processes
            pkill -9 -x udhcpc 2>/dev/null || true
            pkill -9 -x sqlite3 2>/dev/null || true

            # Flush global IPs from loopback (leftover from dhcp tests)
            ip addr flush dev lo scope global 2>/dev/null || true

            nft flush ruleset 2>/dev/null || true
            rm -rf "$STATE_DIR"/* 2>/dev/null

            # Clean up API runtime files (stale locks block new API instances)
            rm -rf /var/run/glacic/api 2>/dev/null || true
            rm -f /var/run/glacic-ctl.sock 2>/dev/null || true
        fi

        # Always ensure loopback is healthy (even if cleanup script didn't run)
        ip link set lo up 2>/dev/null || true
        ip addr add 127.0.0.1/8 dev lo 2>/dev/null || true
        ip addr add ::1/128 dev lo 2>/dev/null || true
    fi

    if [ "$TRACE_WAS_ON" = "1" ]; then set -x; fi
}

# ============================================================================
# Process Leak Detection
# ============================================================================
# Capture process snapshot to file (excludes kernel threads, self, and known safe procs)
snapshot_procs() {
    # Format: PID PPID PGID CMD
    # Exclude: kernel threads (ppid=2), our shell tree, toolbox agent
    ps -eo pid,ppid,pgid,comm 2>/dev/null | \
        grep -v '^\s*PID' | \
        grep -vE '^\s*(1|2)\s' | \
        grep -vE 'ps|grep|sh|ash|bash|sleep|cat|awk|sed|toolbox|tail|head' | \
        sort -n > "${1:-/tmp/procs_snapshot.txt}"
}

# Diff current processes against snapshot, log any new ones
diff_procs() {
    local snapshot="${1:-/tmp/procs_snapshot.txt}"
    local current="/tmp/procs_current.txt"

    [ ! -f "$snapshot" ] && return 0

    snapshot_procs "$current"

    # Find new processes (in current but not in snapshot)
    local leaked=$(comm -13 "$snapshot" "$current" 2>/dev/null | grep -vE '^\s*$')

    if [ -n "$leaked" ]; then
        echo "# WARNING: Leaked processes detected after test:"
        echo "$leaked" | while read -r line; do
            echo "#   $line"
        done
    fi

    rm -f "$current"
}

# Execute reset immediately
reset_state

# Take process snapshot AFTER cleanup (baseline for this test)
PROC_SNAPSHOT="/tmp/procs_baseline_$$.txt"
snapshot_procs "$PROC_SNAPSHOT"

# Add EXIT handler to check for leaked processes
_check_leaks_on_exit() {
    diff_procs "$PROC_SNAPSHOT"
    rm -f "$PROC_SNAPSHOT"
}
trap '_check_leaks_on_exit' EXIT

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
export GLACIC_STATE_DIR="${GLACIC_STATE_DIR:-$STATE_DIR}"
export GLACIC_LOG_DIR="${GLACIC_LOG_DIR:-$LOG_DIR}"
export GLACIC_RUN_DIR="${GLACIC_RUN_DIR:-$RUN_DIR}"
export GLACIC_CTL_SOCKET="${GLACIC_CTL_SOCKET:-$CTL_SOCKET}"

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
# Note: The agent also performs cleanup after TAP_END, but we still need
# aggressive cleanup here because tests may call this before exiting.
cleanup_processes() {
    for pid in $BACKGROUND_PIDS; do
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null
            sleep 0.1
            kill -9 "$pid" 2>/dev/null || true
        fi
    done
    BACKGROUND_PIDS=""

    # Blanket kill of glacic processes (catches untracked children)
    pkill -9 -f "$BINARY_NAME" 2>/dev/null || true
    for i in $(seq 1 20); do
        if ! pgrep -f "$BINARY_NAME" >/dev/null; then break; fi
        sleep 0.1
    done

    # Kill any process on listening ports (prevents port conflicts)
    if command -v ss >/dev/null 2>&1; then
        # Check TCP (-t) and UDP (-u), listening (-l), numeric (-n), process (-p)
        for pid in $(ss -tulnp 2>/dev/null | grep -o 'pid=[0-9]*' | cut -d= -f2 | sort -u); do
            kill -9 "$pid" 2>/dev/null || true
        done
    fi

    # Explicitly kill common interfering services
    pkill -9 -x dnsmasq 2>/dev/null || true
    pkill -9 -x coredns 2>/dev/null || true
}

# Set up automatic cleanup on exit (call this at start of test)
cleanup_on_exit() {
    trap cleanup_processes EXIT INT TERM
}

# ============================================================================
# Service Management - Structured background process control
# ============================================================================

# Named services registry (for better diagnostics)
SERVICE_PIDS=""
SERVICE_NAMES=""

# Start a named background service
# Usage: run_service "http-server" nc -l -p 8080
# Returns: 0 on success, sets SERVICE_PID to the background PID
run_service() {
    _name="$1"
    shift

    # Start the service in background
    "$@" &
    SERVICE_PID=$!

    # Track for cleanup
    track_pid $SERVICE_PID
    SERVICE_PIDS="$SERVICE_PIDS $SERVICE_PID"
    SERVICE_NAMES="$SERVICE_NAMES $_name:$SERVICE_PID"

    diag "Started service '$_name' (PID: $SERVICE_PID)"
    return 0
}

# Stop a specific named service
# Usage: stop_service "http-server"
stop_service() {
    _name="$1"
    _found=0

    for entry in $SERVICE_NAMES; do
        case "$entry" in
            ${_name}:*)
                _pid="${entry#*:}"
                if kill -0 "$_pid" 2>/dev/null; then
                    kill "$_pid" 2>/dev/null
                    sleep 0.1
                    kill -9 "$_pid" 2>/dev/null || true
                    diag "Stopped service '$_name' (PID: $_pid)"
                fi
                _found=1
                ;;
        esac
    done

    if [ "$_found" -eq 0 ]; then
        diag "Service '$_name' not found"
        return 1
    fi
    return 0
}

# List running services (for debugging)
list_services() {
    diag "Running services:"
    for entry in $SERVICE_NAMES; do
        _name="${entry%:*}"
        _pid="${entry#*:}"
        if kill -0 "$_pid" 2>/dev/null; then
            diag "  - $_name (PID: $_pid) [running]"
        else
            diag "  - $_name (PID: $_pid) [stopped]"
        fi
    done
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
    _status=$1
    shift
    _desc="$1"
    shift

    if [ "$_status" -eq 0 ]; then
        echo "ok $test_count - $_desc"
    else
        echo "not ok $test_count - $_desc"
        failed_count=$((failed_count + 1))
        if [ "$#" -gt 0 ]; then
            yaml_diag "$@"
        fi
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

# Helper to require Linux (these tests won't work on BSD/macOS)
require_linux() {
    if [ "$(uname -s)" != "Linux" ]; then
        echo "1..0 # SKIP Requires Linux (got: $(uname -s))"
        exit 0
    fi
}

# Helper to require VM environment (checks for test mount or specific env var)
require_vm() {
    # Check if we're in the expected VM mount or agent sets this
    if [ ! -d "$MOUNT_PATH" ] && [ -z "${GLACIC_TEST_VM:-}" ]; then
        echo "1..0 # SKIP Must run in test VM"
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

# Helper for temp files - uses /tmp with unique naming to prevent collisions
# Format: /tmp/test_${PID}_${timestamp}_${random}_${suffix}
mktemp_compatible() {
    suffix="${1:-tmp}"
    # Add random component to prevent collision when parallel tests have same PID+timestamp
    random=$(head -c 4 /dev/urandom | od -An -tx1 | tr -d ' \n')
    echo "/tmp/test_${$}_$(date +%s)_${random}_$suffix"
}

# ============================================================================
# Fail-Fast Helpers
# ============================================================================

# Helper to output TAP-14 header
tap_version_14() {
    echo "TAP version 14"
}

# Helper to output YAML diagnostics
# Usage: yaml_diag "key" "value" "key2" "value2" ...
yaml_diag() {
    echo "  ---"
    while [ "$#" -gt 0 ]; do
        key="$1"
        shift
        val="$1"
        shift
        # Simple quoting for one-liners, block for multi-line
        if echo "$val" | grep -q "\n"; then
            echo "  $key: |"
            echo "$val" | sed 's/^/    /'
        else
            echo "  $key: \"$val\""
        fi
    done
    echo "  ..."
}

# Immediately fail the test with a message and optional diagnostics
# Usage: fail "message" ["key" "val"...]
fail() {
    echo "not ok - FATAL: $1"
    shift
    if [ "$#" -gt 0 ]; then
        yaml_diag "severity" "fail" "error" "Fatal Error" "$@"
    fi
    cleanup_processes
    sleep 1 # Ensure output flushes before exit
    exit 1
}

# Assert a condition, fail if false
# Usage: assert "condition" ["failure_message"]
assert() {
    if ! eval "$1"; then
        msg="${2:-Assertion failed: $1}"
        fail "$msg" "condition" "$1"
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
# Optimized for low-resource environments (reduced polling)
wait_for_port() {
    _port="$1"
    _timeout=$(scale_timeout "${2:-5}")
    _i=0
    _max=$((_timeout * 2))  # 2 checks per second (0.5s sleep)

    while [ $_i -lt $_max ]; do
        if check_port "$_port"; then
            return 0
        fi
        sleep 0.5
        _i=$((_i + 1))
    done

    echo "# TIMEOUT waiting for port $_port after ${_timeout}s"
    return 1
}

# Wait for a file to exist (max 5 seconds by default)
wait_for_file() {
    _file="$1"
    _timeout=$(scale_timeout "${2:-5}")
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
    _timeout=$(scale_timeout "${2:-5}")
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
    export CTL_LOG=$(mktemp_compatible ctl.log)
    echo "# DEBUG: CTL_LOG path is: $CTL_LOG"

    diag "Starting control plane with config: $_config"

    # Ensure clean slate
    pkill -x "$BINARY_NAME" 2>/dev/null || true
    rm -f "$CTL_SOCKET" 2>/dev/null

    # Pre-check configuration (if binary supports it)
    if $APP_BIN --help 2>&1 | grep -q "check"; then
        if ! $APP_BIN check "$_config" >> "$CTL_LOG" 2>&1; then
            echo "# Config check failed:"
            sed 's/^/# /' "$CTL_LOG"
            fail "Config check failed for $_config"
        fi
    fi

    # Inject state-dir if GLACIC_STATE_DIR is set
    # Note: main.go parses --state-dir flag for ctl command
    if [ -n "$GLACIC_STATE_DIR" ]; then
        set -- --state-dir "$GLACIC_STATE_DIR" "$@"
    fi

    # Force logging to stdout so we can capture it
    export GLACIC_LOG_FILE=stdout

    # Skip automatic API/Proxy spawning - tests will start their own test-api server
    # Unless test explicitly sets GLACIC_SKIP_API=0 to test proxy functionality
    export GLACIC_SKIP_API="${GLACIC_SKIP_API:-1}"

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
    export API_LOG=$(mktemp_compatible api.log)

    # Try to extract port from args (default 8080)
    _port=8080
    case "$*" in
        *"-listen "*":"*)
            _port=$(echo "$*" | sed 's/.*-listen [^:]*:\([0-9]*\).*/\1/')
            ;;
        *"-listen :"[0-9]*)
            _port=$(echo "$*" | sed 's/.*-listen :\([0-9]*\).*/\1/')
            ;;
    esac

    diag "Starting API server on $_port (logs: $API_LOG)"
    # Use -no-tls to force HTTP mode for reliable testing
    $APP_BIN test-api -no-tls "$@" > "$API_LOG" 2>&1 &
    API_PID=$!
    track_pid $API_PID

    # Wait for API to be actually responsive (HTTP ready)
    # Use 90s timeout for high-load parallel tests
    if ! wait_for_api_ready "$_port" 90; then
        _log=""
        [ -f "$API_LOG" ] && _log=$(head -n 50 "$API_LOG")
        fail "API server failed to start" port "$_port" log_head "$_log"
    fi
}

# Wait for API to respond to HTTP requests
# Usage: wait_for_api_ready <port> [timeout_sec]
wait_for_api_ready() {
    local port="$1"
    local timeout=$(scale_timeout "${2:-10}") # Default 10s, scaled
    local url="http://127.0.0.1:$port/api/status"  # Use /api/status which is always available

    diag "Waiting for API readiness at $url..."

    local i=0
    # Check 2x per second
    local max=$((timeout * 2))

    while [ $i -lt $max ]; do
        # Fail fast if API process has died
        if [ -n "$API_PID" ] && ! kill -0 "$API_PID" 2>/dev/null; then
            diag "API process (PID $API_PID) died unexpectedly during startup"
            echo "# API LOG OUTPUT:"
            if [ -n "$API_LOG" ] && [ -f "$API_LOG" ]; then
                cat "$API_LOG"
            fi
            return 1
        fi

        # Check if we get ANY HTTP response (even 404 is fine, just not connection reset/empty)
        if command -v curl >/dev/null 2>&1; then
            # curl returns 0 on HTTP success (even 404), non-zero on conn refused/empty
            if curl -s -m 1 "$url" >/dev/null 2>&1; then
                return 0
            fi
        elif command -v wget >/dev/null 2>&1; then
             # wget returns 0 on 200, but non-zero on 404.
             # We want to accept 404 as "server ready".
             # wget -S prints headers to stderr.
             if wget -q -S -O - "$url" 2>&1 | grep -q "HTTP/"; then
                return 0
             fi
        fi

        sleep 0.5
        i=$((i + 1))
    done

    return 1
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
    if command -v timeout >/dev/null 2>&1; then
        # Capture hang logs by timing out
        timeout -s 9 20s $APP_BIN test "$_config_file" > "$_log_file" 2>&1
    else
        $APP_BIN test "$_config_file" > "$_log_file" 2>&1
    fi
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
    enable_ip_forward

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
    # Default 15s timeout (scaled) to prevent hangs
    run_with_timeout "$(scale_timeout "${CLIENT_TIMEOUT:-15}")" ip netns exec test_client "$@"
}

# Run command in server namespace (WAN side)
run_server() {
    # Default 15s timeout (scaled) to prevent hangs
    run_with_timeout "$(scale_timeout "${SERVER_TIMEOUT:-15}")" ip netns exec test_server "$@"
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
    _timeout=$(scale_timeout "${2:-5}")
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
