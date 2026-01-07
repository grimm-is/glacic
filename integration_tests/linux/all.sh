#!/bin/sh

# Run All Integration Tests
# Outputs combined TAP results with prove-compatible format
# Runs both shell TAP tests and Go unit tests

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$(dirname "$SCRIPT_DIR")")"

# Colors for terminal
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

echo "============================================="
echo "  Firewall Integration Test Suite"
echo "============================================="
echo ""

# Check if running as root
if [ "$(id -u)" -ne 0 ]; then
    echo "${YELLOW}Warning: Not running as root. Some tests will be skipped.${NC}"
    echo ""
fi

# Check for prove (TAP harness)
USE_PROVE=0
if command -v prove >/dev/null 2>&1; then
    USE_PROVE=1
    echo "${GREEN}✓ prove available${NC}"
fi

# Check for Go
HAS_GO=0
if command -v go >/dev/null 2>&1; then
    HAS_GO=1
    GO_VERSION=$(go version | awk '{print $3}')
    echo "${GREEN}✓ Go available ($GO_VERSION)${NC}"
fi

echo ""

total_pass=0
total_fail=0
total_skip=0

run_test() {
    name="$1"
    script="$2"

    echo "----------------------------------------"
    echo "Running: $name"
    echo "----------------------------------------"

    if [ ! -x "$script" ]; then
        chmod +x "$script"
    fi

    if [ $USE_PROVE -eq 1 ]; then
        prove -v "$script"
        result=$?
    else
        # Run directly and parse TAP output
        output=$("$script" 2>&1)
        result=$?
        echo "$output"

        # Count results
        pass=$(echo "$output" | grep -c "^ok" || true)
        fail=$(echo "$output" | grep -c "^not ok" || true)
        skip=$(echo "$output" | grep -c "# SKIP" || true)

        total_pass=$((total_pass + pass))
        total_fail=$((total_fail + fail))
        total_skip=$((total_skip + skip))
    fi

    echo ""
    return $result
}

# Track overall status
overall_status=0

# =============================================
# Part 1: Go Unit Tests (Source or Binary)
# =============================================

# Path to pre-compiled tests (from make build-tests)
TEST_BIN_DIR="$PROJECT_DIR/build/tests"

if [ -d "$TEST_BIN_DIR" ] && [ "$(ls -A "$TEST_BIN_DIR"/*.test 2>/dev/null)" ]; then
    echo "============================================="
    echo "  ${CYAN}Go Unit Tests (Pre-compiled)${NC}"
    echo "============================================="
    echo ""

    # Check for filter
    FILTER_FILE="$PROJECT_DIR/.test_filter"
    FILTER=""
    if [ -f "$FILTER_FILE" ]; then
        FILTER=$(cat "$FILTER_FILE")
        echo "Applying filter: $FILTER"
    fi

    for test_bin in "$TEST_BIN_DIR"/*.test; do
        test_name=$(basename "$test_bin" .test)

        # Apply filter if set
        if [ -n "$FILTER" ]; then
            # Simple substring match on package name
            if [[ "$test_name" != *"$FILTER"* ]]; then
                continue
            fi
        fi

        echo "Running: $test_name"
        echo "----------------------------------------"
        if "$test_bin" -test.v 2>&1; then
            echo ""
            echo "${GREEN}✓ $test_name passed${NC}"
        else
            echo ""
            echo "${RED}✗ $test_name failed${NC}"
            overall_status=1
        fi
        echo ""
    done

elif [ $HAS_GO -eq 1 ]; then
    echo "============================================="
    echo "  ${CYAN}Go Unit Tests (Source)${NC}"
    echo "============================================="
    echo ""

    cd "$PROJECT_DIR"

    # Run Go tests with verbose output
    echo "Running: go test ./..."
    echo "----------------------------------------"

    if go test -v ./... 2>&1; then
        echo ""
        echo "${GREEN}✓ Go tests passed${NC}"
    else
        echo ""
        echo "${RED}✗ Go tests failed${NC}"
        overall_status=1
    fi

    echo ""
fi

# =============================================
# Part 2: TAP Shell Integration Tests
# =============================================

echo "============================================="
echo "  ${CYAN}TAP Integration Tests${NC}"
echo "============================================="
echo ""

# Unit tests (nftables)
if [ -f "$SCRIPT_DIR/sanity/nftables_test.sh" ]; then
    run_test "nftables Basic Operations" "$SCRIPT_DIR/sanity/nftables_test.sh" || overall_status=1
fi

# Protection features
if [ -f "$SCRIPT_DIR/sanity/protection_test.sh" ]; then
    run_test "Protection Features" "$SCRIPT_DIR/sanity/protection_test.sh" || overall_status=1
fi

# QoS tests
if [ -f "$SCRIPT_DIR/sanity/qos_test.sh" ]; then
    run_test "QoS Traffic Shaping" "$SCRIPT_DIR/sanity/qos_test.sh" || overall_status=1
fi

# Conntrack helpers
if [ -f "$SCRIPT_DIR/sanity/conntrack_helpers_test.sh" ]; then
    run_test "Conntrack Helpers" "$SCRIPT_DIR/sanity/conntrack_helpers_test.sh" || overall_status=1
fi

# Features folder tests
for test_script in "$SCRIPT_DIR/features/"*_test.sh; do
    if [ -f "$test_script" ]; then
        test_name=$(basename "$test_script")

        # Apply filter if set
        if [ -n "$FILTER" ]; then
            if [[ "$test_name" != *"$FILTER"* ]]; then
                continue
            fi
        fi

        run_test "Feature: $test_name" "$test_script" || overall_status=1
    fi
done

# Zone firewall tests (requires multi-interface setup)
# if [ -f "$SCRIPT_DIR/firewall_zone_test.sh" ]; then
#     run_test "Firewall Zone Configuration" "$SCRIPT_DIR/firewall_zone_test.sh" || overall_status=1
# fi

# --- Summary ---

echo "============================================="
echo "  Test Summary"
echo "============================================="

if [ $USE_PROVE -eq 0 ]; then
    echo "TAP Tests:"
    echo "  Passed:  $total_pass"
    echo "  Failed:  $total_fail"
    echo "  Skipped: $total_skip"
    echo ""
fi

if [ $overall_status -eq 0 ]; then
    echo "${GREEN}All test suites passed!${NC}"
else
    echo "${RED}Some test suites failed.${NC}"
fi

exit $overall_status
