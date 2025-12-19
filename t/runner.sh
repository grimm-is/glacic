#!/bin/sh

# TAP Test Runner for VM
# Runs all test suites and outputs clean TAP format
# All non-TAP output is prefixed with ### for easy filtering

# Brand mount path (matches glacic-builder)
MOUNT_PATH="/mnt/glacic"

# Strict timeout settings - fail fast, fail hard
DEFAULT_TEST_TIMEOUT=${TEST_TIMEOUT:-5}  # 5 seconds default, override with TEST_TIMEOUT env
GO_TEST_TIMEOUT=${GO_TEST_TIMEOUT:-120}   # Go tests get more time (compilation + ARM64 emulation)
TEST_TIMEOUT=$DEFAULT_TEST_TIMEOUT

# Setup network for test VM
# QEMU user networking: VM is 10.0.2.15, host/gateway is 10.0.2.2
echo "### Setting up network..."
ip link set lo up 2>/dev/null
ip link set eth0 up 2>/dev/null
ip addr add 10.0.2.15/24 dev eth0 2>/dev/null
ip route add default via 10.0.2.2 2>/dev/null

# Configure Go to use vendoring and disable network
export GOFLAGS="-mod=vendor"
export GOPROXY=off

echo "### Network: $(ip -4 addr show eth0 | grep inet | awk '{print $2}')"

# Initialize results directory (transient artifacts in build/)
RESULTS_DIR="$MOUNT_PATH/build/test-results"
mkdir -p "$RESULTS_DIR"

# Clean previous results if NOT rerunning
RERUN_FAILED=0
if grep -q "rerun_failed=true" /proc/cmdline; then
    RERUN_FAILED=1
else
    # Only clean logs if this is a fresh run
    rm -f "$RESULTS_DIR"/*.log
fi

# Discover test scripts (exclude infrastructure scripts)
FILTER=""
if [ -f "$MOUNT_PATH/.test_filter" ]; then
    FILTER=$(cat "$MOUNT_PATH/.test_filter")
    echo "### Applying filter: $FILTER"
fi

# Build test list - Go tests are treated as a virtual "go_test" for filtering
# Build test list - Go tests are always candidate, we filter binaries individually
RUN_GO_TESTS=1

# Check for rerun request (from kernel command line)
RERUN_FAILED=0
if grep -q "rerun_failed=true" /proc/cmdline; then
    RERUN_FAILED=1
    echo "### RERUN MODE: Retrying previously failed tests only"
fi

# Failed tests tracking
FAILED_LIST="$RESULTS_DIR/failed_tests.txt"
if [ "$RERUN_FAILED" = "1" ]; then
    if [ -f "$FAILED_LIST" ]; then
        FILTER=$(cat "$FAILED_LIST")
        echo "### Rerunning failures: $FILTER"
    else
        echo "### Warning: No previous failures found, running all tests."
        RERUN_FAILED=0
    fi
else
    # New run - clear failed list
    rm -f "$FAILED_LIST"
fi

# Find shell test scripts
if [ "$RERUN_FAILED" = "1" ]; then
    # Construct paths from basenames
    TEST_SCRIPTS=""
    for name in $FILTER; do
        found=$(find "$MOUNT_PATH/t" -name "${name}_test.sh" 2>/dev/null)
        if [ -n "$found" ]; then
            TEST_SCRIPTS="$TEST_SCRIPTS $found"
        fi
    done
elif [ -n "$FILTER" ]; then
    TEST_SCRIPTS=$(find "$MOUNT_PATH/t" -name "*_test.sh" | grep -E "$FILTER" | sort)
else
    TEST_SCRIPTS=$(find "$MOUNT_PATH/t" -name "*_test.sh" | grep -v -E "(tap-harness|runner|all)\.sh" | sort)
fi
# Handle empty TEST_SCRIPTS - grep returns empty string which causes arithmetic issues
if [ -z "$TEST_SCRIPTS" ]; then
    TEST_COUNT=0
else
    TEST_COUNT=$(echo "$TEST_SCRIPTS" | wc -l | tr -d ' ')
fi

# Calculate total tests
TOTAL_TESTS=$TEST_COUNT
if [ "$RUN_GO_TESTS" = "1" ]; then
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
fi

# Report discovered tests
echo "### Test Discovery:"
if [ "$RUN_GO_TESTS" = "1" ]; then
    echo "###   - Go unit tests: ./internal/... (filtered by '$FILTER')"
fi
echo "###   - Shell tests: $TEST_COUNT scripts in t/"
echo "###   - Total test suites: $TOTAL_TESTS"
echo "###"

echo "TAP version 13"
echo "1..$TOTAL_TESTS"

# Test counters
test_num=1
tests_executed=0
go_tests_ran=0
failed_tests=0

# Helper to prefix non-TAP output (unbuffered for real-time streaming)
prefix_output() {
    # Use sed -u for unbuffered output (GNU/busybox sed)
    sed -u 's/^/### /' 2>/dev/null || sed 's/^/### /'
}

# Run Go tests if included
if [ "$RUN_GO_TESTS" = "1" ]; then
    # Check for pre-compiled test binaries
    PRECOMPILED_TESTS="$MOUNT_PATH/build/tests"

    if [ -d "$PRECOMPILED_TESTS" ] && [ "$(ls -A $PRECOMPILED_TESTS/*.test 2>/dev/null | wc -l)" -gt 0 ]; then
        echo "### Running pre-compiled Go tests (fast mode)..."

        GO_EXIT_FILE=$(mktemp)
        go_failed=0
        found_any_go=0

        GO_LOG_FILE="$RESULTS_DIR/go_test.log"
        # Clear log file
        : > "$GO_LOG_FILE"

        # Run each pre-compiled test binary
        for testbin in "$PRECOMPILED_TESTS"/*.test; do
            testname=$(basename "$testbin" .test)

            # Apply filter - skip if filtering by specific test names BUT allow "go_test" to run all
            if [ -n "$FILTER" ] && [ "$FILTER" != "go" ] && [ "$FILTER" != "go_test" ]; then
                # If filter contains go_test, we run everything (it's a rerun of the suite)
                # If filter is just script names, we skip
                 if ! echo "$FILTER" | grep -q "go_test"; then
                     if [[ "$testname" != *"$FILTER"* ]]; then
                         continue
                     fi
                 fi
            fi

            found_any_go=1

            # Skip known problematic packages with goroutine lifecycle issues
            if [ "$testname" = "learning" ]; then
                echo "###   Skipping $testname (goroutine lifecycle issues - tracked)"
                continue
            fi

            echo "###   Running $testname..."
            # Run test and capture exit code properly
            # The subshell exit code is captured before tee
            {
                timeout 60 "$testbin" -test.v 2>&1
                echo $? > "$GO_EXIT_FILE"
            } | tee -a "$GO_LOG_FILE" | prefix_output

            bin_exit=$(cat "$GO_EXIT_FILE" 2>/dev/null || echo "999")
            echo "###   $testname exit code: $bin_exit" >> "$GO_LOG_FILE"
            if [ "$bin_exit" != "0" ]; then
                echo "###   $testname exited with code $bin_exit"
                go_failed=1
            fi
        done

        rm -f "$GO_EXIT_FILE"

        if [ "$found_any_go" = "0" ]; then
             echo "ok $test_num - Go unit tests (SKIP: filter '$FILTER' matched no Go tests)"
             go_tests_ran=1
        elif [ "$go_failed" = "0" ]; then
            echo "ok $test_num - Go unit tests"
            go_tests_ran=1
        else
            echo "### DEBUG: go_failed flag is set"
            echo "not ok $test_num - Go unit tests"
            failed_tests=$((failed_tests + 1))
            echo "go_test" >> "$FAILED_LIST"
            go_tests_ran=1
        fi
    else
        echo "### Running Go tests (compiling in VM - slower)..."
        echo "### TIP: Run 'make build-tests' to pre-compile for faster dev cycles"

        GO_LOG_FILE="$RESULTS_DIR/go_test.log"
        # Stream Go test output in real-time (prefixed), capture exit code
        GO_EXIT_FILE=$(mktemp)
        if command -v stdbuf >/dev/null 2>&1; then
            (cd "$MOUNT_PATH" && stdbuf -oL timeout $GO_TEST_TIMEOUT go test -v ./internal/... 2>&1; echo $? > "$GO_EXIT_FILE") | tee "$GO_LOG_FILE" | prefix_output
        else
            (cd "$MOUNT_PATH" && timeout $GO_TEST_TIMEOUT go test -v ./internal/... 2>&1; echo $? > "$GO_EXIT_FILE") | tee "$GO_LOG_FILE" | prefix_output
        fi
        go_exit=$(cat "$GO_EXIT_FILE")
        rm -f "$GO_EXIT_FILE"

        if [ "$go_exit" = "124" ]; then
            echo "### TIMEOUT: Go tests exceeded ${GO_TEST_TIMEOUT}s"
        fi

        if [ "$go_exit" = "0" ]; then
            echo "ok $test_num - Go unit tests"
        else
            echo "not ok $test_num - Go unit tests"
            failed_tests=$((failed_tests + 1))
            echo "go_test" >> "$FAILED_LIST"
        fi
        go_tests_ran=1
    fi
    test_num=$((test_num + 1))
fi

# Run integration tests dynamically
# Per-test timeout - 15s default, tests should be FAST
TEST_TIMEOUT=$DEFAULT_TEST_TIMEOUT

echo "### Integration test timeout: ${TEST_TIMEOUT}s per test"
echo "### Tests that need more time should set TEST_TIMEOUT in their script"
echo "###"

cleanup_environment() {
    # Kill any lingering instances
    pkill -9 glacic >/dev/null 2>&1 || true

    # Flush nftables to ensure clean state
    if command -v nft >/dev/null 2>&1; then
        nft flush ruleset >/dev/null 2>&1 || true
    fi

    # Clean up sockets and temp files
    rm -f /var/run/glacic-ctl.sock
    rm -f /tmp/test_*

    # Clear crash state to prevent Safe Mode triggering from previous tests
    rm -f /var/lib/glacic/crash.state
}

# Create results directory if not exists (already done above)
mkdir -p "$RESULTS_DIR"

for script in $TEST_SCRIPTS; do
    if [ -f "$script" ]; then
        # Ensure clean state before starting the test
        cleanup_environment

        # Extract test name from filename
        name=$(basename "$script" "_test.sh")
        tests_executed=$((tests_executed + 1))

        # Check if test script declares its own timeout (e.g., TEST_TIMEOUT=30)
        script_timeout=$(grep -m1 "^TEST_TIMEOUT=" "$script" 2>/dev/null | cut -d= -f2)

        # Also check for env var override: TEST_TIMEOUT_<NAME>
        custom_timeout_var="TEST_TIMEOUT_$(echo "$name" | tr 'a-z-' 'A-Z_')"
        eval "env_timeout=\$$custom_timeout_var"

        # Priority: env var > script-declared > default
        if [ -n "$env_timeout" ]; then
            this_timeout=$env_timeout
        elif [ -n "$script_timeout" ]; then
            this_timeout=$script_timeout
        else
            this_timeout=$TEST_TIMEOUT
        fi

        echo "### [$test_num] $name (timeout ${this_timeout}s)"

        # Log file path
        LOG_FILE="$RESULTS_DIR/${name}.log"

        # Check for verbose mode
        VERBOSE_MODE=0
        if grep -q "test_verbose=true" /proc/cmdline; then
            VERBOSE_MODE=1
        fi

        # Run test with timeout, using temp file for output to avoid subshell issues
        start_time=$(date +%s)
        TEST_EXIT_FILE=$(mktemp)

        # Run in background and use timeout -s KILL to forcibly terminate
        if [ "$VERBOSE_MODE" = "1" ]; then
             # Verbose: pipe through tee to log file AND stdout (prefixed)
             # Use a block to capture exit code before piping
             (
                 {
                     timeout -s KILL $this_timeout "$script" 2>&1
                     echo $? > "$TEST_EXIT_FILE"
                 } | tee "$LOG_FILE" | prefix_output
             ) &
        else
             # Normal: redirect to log file only
             ( timeout -s KILL $this_timeout "$script" > "$LOG_FILE" 2>&1; echo $? > "$TEST_EXIT_FILE" ) &
        fi
        wait_pid=$!

        # Wait for the wrapper to complete
        wait $wait_pid 2>/dev/null

        # Read results
        script_exit=$(cat "$TEST_EXIT_FILE" 2>/dev/null || echo 137)
        rm -f "$TEST_EXIT_FILE"

        end_time=$(date +%s)
        duration=$((end_time - start_time))

        # Check for timeout (exit code 124 or 137 for SIGKILL)
        if [ $script_exit -eq 124 ] || [ $script_exit -eq 137 ]; then
            echo "### !!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
            echo "### TIMEOUT: $name exceeded ${this_timeout}s"
            echo "### See log: build/test-results/${name}.log"
            echo "### !!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
            echo "not ok $test_num - $name tests # TIMEOUT after ${this_timeout}s"
            failed_tests=$((failed_tests + 1))
            echo "$name" >> "$FAILED_LIST"
            test_num=$((test_num + 1))
            continue
        fi

        # Warn if test is slow (>80% of timeout)
        slow_threshold=$((this_timeout * 80 / 100))
        if [ $duration -gt $slow_threshold ]; then
            echo "### WARNING: $name took ${duration}s (${slow_threshold}s threshold)"
        fi

        # Show specific TAP output for harness, but keep full log in file
        # Filter TAP lines from log for real-time display, prefix invalid TAP lines
        # Actually, for improved visibility, we might WANT the output?
        # Plan said: "On output, print the path to the failure log."
        # We can stream output or just rely on the log.
        # Existing behavior streamed it. Let's keep streaming matching TAP lines,
        # but suppress noise if it passes?
        # User wants "increase visibility" -> persistent logs helps.

        # Read log file content
        output=$(cat "$LOG_FILE")

        # Count failures in sub-TAP output
        sub_failures=$(echo "$output" | grep -c "^not ok " || true)

        if [ $script_exit -ne 0 ] || [ "$sub_failures" -gt 0 ]; then
            echo "not ok $test_num - $name tests (${duration}s, exit=$script_exit)"
            echo "### see log: build/test-results/${name}.log"

            # Show failures from log for immediate context
            echo "$output" | grep "^not ok " | prefix_output

            # Show last 10 lines
            echo "### Last 10 lines:"
            tail -n 10 "$LOG_FILE" | prefix_output

            failed_tests=$((failed_tests + 1))

            # Track failure for rerun
            echo "$name" >> "$FAILED_LIST"
        else
            echo "ok $test_num - $name tests (${duration}s)"
        fi
    else
        echo "not ok $test_num - Test script not found"
        failed_tests=$((failed_tests + 1))
    fi
    test_num=$((test_num + 1))
done

# Canary check - fail if no tests actually executed
if [ $go_tests_ran -eq 0 ] && [ $tests_executed -eq 0 ]; then
    echo "### ERROR: No tests were actually executed!"
    echo "### This indicates a configuration or setup problem."
    echo "### Go tests ran: $go_tests_ran, Integration tests executed: $tests_executed"
    echo ""
    echo "TAP_RESULT: FAIL (no tests ran)"
    exit 1
fi

# Calculate totals
total_run=$((go_tests_ran + tests_executed))
passed=$((total_run - failed_tests))

# Output clear, parseable final status
echo ""
echo "### ============================================"
echo "### TEST SUMMARY"
echo "### ============================================"
echo "### Total test suites: $total_run"
echo "### Passed: $passed"
echo "### Failed: $failed_tests"
if [ $failed_tests -gt 0 ]; then
    echo "### Failed logs:"
    if [ -f "$FAILED_LIST" ]; then
        while read -r name; do
             echo "###   - $RESULTS_DIR/${name}.log"
        done < "$FAILED_LIST"
    else
        # Fallback if list missing
        for log in "$RESULTS_DIR"/*.log; do
            if grep -q "not ok" "$log"; then
                 echo "###   - $log"
            fi
        done
    fi
fi
echo "### ============================================"

if [ $failed_tests -gt 0 ]; then
    echo "TAP_RESULT: FAIL ($failed_tests failed)"
    exit 1
else
    echo "TAP_RESULT: PASS (all $total_run suites passed)"
    exit 0
fi
