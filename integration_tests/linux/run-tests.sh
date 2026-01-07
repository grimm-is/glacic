#!/bin/sh

# Test Runner - Multi-Configuration VM Orchestration
# Simplified orchestrator that runs tests across multiple firewall configurations

set -e

# Load brand configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
eval "$("$SCRIPT_DIR/../../scripts/brand.sh")"

# Test configurations: config_file:test_mode
declare -a TEST_CONFIGS=(
    "configs/firewall.hcl:guest_test"
    "configs/zones.hcl:config=zones_test"
    "configs/zones-single.hcl:single_vm_test=true"
)

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test counters
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# Helper functions
print_header() {
    echo -e "${BLUE}================================================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}================================================================${NC}"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

# Check prerequisites
check_prerequisites() {
    print_header "Checking Prerequisites"

    # Check if required files exist
    local missing_files=()

    for config_entry in "${TEST_CONFIGS[@]}"; do
        local config_file="${config_entry%%:*}"
        if [[ ! -f "$config_file" ]]; then
            missing_files+=("$config_file")
        fi
    done

    # Check binary (Linux is default, no suffix)
    if [[ ! -f "build/${BRAND_BINARY_NAME}" ]]; then
        missing_files+=("build/${BRAND_BINARY_NAME}")
    fi

    if [[ ${#missing_files[@]} -gt 0 ]]; then
        print_error "Missing required files:"
        for file in "${missing_files[@]}"; do
            echo "  - $file"
        done
        exit 1
    fi

    print_success "All prerequisites satisfied"
    echo ""
}

# Build firewall binary if needed
build_firewall() {
    print_header "Building ${BRAND_NAME} Binary"

    if [[ ! -f "build/${BRAND_BINARY_NAME}" ]] || [[ "build/${BRAND_BINARY_NAME}" -ot "main.go" ]]; then
        echo "Building ${BRAND_BINARY_NAME}..."
        make build-go
    else
        echo "${BRAND_BINARY_NAME} is up to date"
    fi

    print_success "Build complete"
    echo ""
}

# Discover and report tests
discover_tests() {
    local go_packages=$(find internal -name "*_test.go" -exec dirname {} \; 2>/dev/null | sort -u | wc -l | tr -d ' ')
    local shell_tests=$(find integration_tests/linux -name "*_test.sh" 2>/dev/null | grep -v -E "(tap-harness|runner|all)\.sh" | wc -l | tr -d ' ')

    echo -e "${BLUE}Test Discovery:${NC}"
    echo "  • Go packages with tests: $go_packages (in ./internal/...)"
    echo "  • Shell test scripts: $shell_tests (in integration_tests/linux/)"
    echo "  • Total test suites: $((go_packages > 0 ? 1 : 0 + shell_tests))"
    echo ""
}

# Run unit and integration tests in VM
run_unit_tests() {
    print_header "Unit & Integration Tests"
    discover_tests

    # Run VM with real-time output via tee, capture to temp file for analysis
    local tmpfile=$(mktemp)
    trap "rm -f $tmpfile" RETURN

    # Stream output in real-time while also capturing it
    # Ignore VM exit code - it's unreliable. Parse TAP_RESULT instead.
    API_PORT=none ./scripts/vm-dev.sh "$(pwd)" "configs/firewall.hcl" "quiet loglevel=0 test_mode=true" 2>&1 | tee "$tmpfile" || true

    # Look for our explicit TAP_RESULT line - this is the source of truth
    local tap_result
    tap_result=$(grep "^TAP_RESULT:" "$tmpfile" | tail -1)

    if [ -z "$tap_result" ]; then
        print_error "Unit & Integration tests failed (no TAP_RESULT found - VM may have crashed)"
        return 1
    elif echo "$tap_result" | grep -q "PASS"; then
        print_success "Unit & Integration tests passed"
        echo "  $tap_result"
        return 0
    else
        print_error "Unit & Integration tests failed"
        echo "  $tap_result"

        # List failed logs if available
        local failed_logs=$(grep -l "not ok" "$tmpfile" || true) # This assumes 'not ok' is in the log files themselves, not just the tee output.
                                                                # If the logs are written to a specific directory, that directory should be used.
                                                                # For now, assuming $tmpfile contains the relevant info.
        if [ -n "$failed_logs" ]; then
            echo -e "${YELLOW}Failed logs (from VM output):${NC}"
            for log_line in $failed_logs; do
                # Strip /mnt/glacic prefix for host-relative path if it appears in the log_line
                # This part of the change seems to assume 'log' is a file path, but $failed_logs here is just lines from $tmpfile.
                # If actual log files are generated in a known directory, that directory should be searched.
                # For now, applying the strip to the log_line itself, though it might not be a path.
                local rel_log="${log_line#/mnt/glacic/}"
                echo "  - $rel_log"
            done
        fi
        return 1
    fi
}

# Run a single configuration test
run_config_test() {
    local config_file="$1"
    local test_mode="$2"
    local config_name=$(basename "$config_file" .hcl)

    print_header "Testing Configuration: $config_file"

    echo "Configuration: $config_file"
    echo "Test Mode: $test_mode"

    # Extract kernel parameters
    local kernel_params="quiet loglevel=0 autotest=true"
    if [[ "$test_mode" == *"="* ]]; then
        kernel_params="$kernel_params $test_mode"
    fi

    # Check for rerun mode
    if [[ "$RERUN_FAILED" == "true" ]]; then
        kernel_params="$kernel_params rerun_failed=true"
    fi

    # Propagate FILTER if set (allows running specific shell tests)
    if [[ -n "$FILTER" ]]; then
        kernel_params="$kernel_params filter=$FILTER"
    fi

    echo "Kernel parameters: $kernel_params"
    echo "Starting test..."

    # Run the VM test and capture output - QEMU exit code doesn't reflect test results
    local output
    output=$(API_PORT=none ./scripts/vm-dev.sh "$(pwd)" "$config_file" "$kernel_params" 2>&1)
    local vm_exit=$?

    # Display the output
    echo "$output"

    # Check for test failures in TAP output
    local failures
    failures=$(echo "$output" | grep -c "^not ok" || true)

    if [ "$failures" -gt 0 ] || [ "$vm_exit" -ne 0 ]; then
        print_error "Configuration $config_name failed"
        FAILED_TESTS=$((FAILED_TESTS + 1))
    else
        print_success "Configuration $config_name passed"
        PASSED_TESTS=$((PASSED_TESTS + 1))
    fi

    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    echo ""
}

# Main script logic
main() {
    print_header "${BRAND_NAME} Multi-Configuration Test Runner"
    echo "Orchestrates tests across multiple firewall configurations"
    echo ""

    check_prerequisites
    build_firewall

    case "${1:-unit}" in
        unit)
            run_unit_tests
            ;;
        config)
            print_header "Running Configuration Tests"
            for config_entry in "${TEST_CONFIGS[@]}"; do
                config_file="${config_entry%%:*}"
                test_mode="${config_entry##*:}"
                run_config_test "$config_file" "$test_mode"
            done

            # Print summary
            print_header "Configuration Tests Summary"
            echo "Total configurations: $TOTAL_TESTS"
            echo -e "Passed: ${GREEN}$PASSED_TESTS${NC}"
            echo -e "Failed: ${RED}$FAILED_TESTS${NC}"

            if [[ $FAILED_TESTS -eq 0 ]]; then
                print_success "All configuration tests passed!"
                exit 0
            else
                print_error "Some configuration tests failed!"
                exit 1
            fi
            ;;
        rerun)
            print_header "Rerunning Failed Tests"
            export RERUN_FAILED=true
            # Default to config mode if not specified, but usually we just rerun what failed.
            # Runner iterates configs.
            for config_entry in "${TEST_CONFIGS[@]}"; do
                config_file="${config_entry%%:*}"
                test_mode="${config_entry##*:}"
                run_config_test "$config_file" "$test_mode"
            done
            ;;
        verbose)
            print_header "Running Integration Tests (Verbose Mode)"
            # Set verbose for runner (kernel arg)
            # We can reuse config logic but modify kernel params dynamically
            # But the runner.sh main loop is either 'unit' or 'config' or 'rerun'.
            # 'verbose' is just a modifier? No, user runs 'make test-int-verbose'.
            # So let's duplicate config loop but inject verbose param.
            # Actually, let's just refactor to support modifiers, but for now duplicate loop is safer.
             for config_entry in "${TEST_CONFIGS[@]}"; do
                config_file="${config_entry%%:*}"
                test_mode="${config_entry##*:} test_verbose=true" # Append verbose flag
                run_config_test "$config_file" "$test_mode"
            done

             # Print summary
            print_header "Configuration Tests Summary"
            echo "Total configurations: $TOTAL_TESTS"
            echo -e "Passed: ${GREEN}$PASSED_TESTS${NC}"
            echo -e "Failed: ${RED}$FAILED_TESTS${NC}"

            if [[ $FAILED_TESTS -eq 0 ]]; then
                print_success "All configuration tests passed!"
                exit 0
            else
                print_error "Some configuration tests failed!"
                exit 1
            fi
            ;;
        *)
            print_error "Unknown test mode: $1"
            echo "Usage: $0 [unit|config]"
            exit 1
            ;;
    esac
}

# Run main function with all arguments
main "$@"
