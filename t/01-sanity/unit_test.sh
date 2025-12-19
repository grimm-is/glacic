#!/bin/sh

# Go Test to TAP converter
# Runs go test and converts output to TAP format

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_DIR"

# Set up Go environment
export GOPATH="${GOPATH:-/tmp/go}"
export GOCACHE="${GOCACHE:-/tmp/go-cache}"
export HOME="${HOME:-/root}"
mkdir -p "$GOPATH" "$GOCACHE" 2>/dev/null

# Check if go is available
if ! command -v go >/dev/null 2>&1; then
    echo "1..0 # SKIP go command not found"
    exit 0
fi

# Check if go.mod exists
if [ ! -f "$PROJECT_DIR/go.mod" ]; then
    echo "1..0 # SKIP no go.mod found"
    exit 0
fi

# Check Go version compatibility
required_version=$(grep "^go " "$PROJECT_DIR/go.mod" | awk '{print $2}')
current_version=$(go version | grep -oE 'go[0-9]+\.[0-9]+' | sed 's/go//')
echo "# Required Go: $required_version, Available: $current_version" >&2

# Simple version check (compare major.minor)
req_major=$(echo "$required_version" | cut -d. -f1)
req_minor=$(echo "$required_version" | cut -d. -f2)
cur_major=$(echo "$current_version" | cut -d. -f1)
cur_minor=$(echo "$current_version" | cut -d. -f2)

if [ "$cur_major" -lt "$req_major" ] || { [ "$cur_major" -eq "$req_major" ] && [ "$cur_minor" -lt "$req_minor" ]; }; then
    echo "1..0 # SKIP Go $current_version < required $required_version"
    exit 0
fi

# Use vendor if available
if [ -d "$PROJECT_DIR/vendor" ]; then
    export GOFLAGS="-mod=vendor"
fi

# Set GOPROXY to off to prevent any attempts to reach network
export GOPROXY=off

# Run go test and capture output
output=$(go test ./... -v 2>&1)
exit_code=$?

# Parse output and convert to TAP
# Count tests first
pass_count=$(echo "$output" | grep -c "--- PASS:" || true)
fail_count=$(echo "$output" | grep -c "--- FAIL:" || true)
skip_count=$(echo "$output" | grep -c "--- SKIP:" || true)
total=$((pass_count + fail_count + skip_count))

if [ "$total" -eq 0 ]; then
    # No tests found or build error
    if [ "$exit_code" -ne 0 ]; then
        echo "1..1"
        echo "not ok 1 - Go build/test failed"
        echo "# Exit code: $exit_code"
        echo "# Build output (last 30 lines):"
        echo "$output" | tail -30 | sed 's/^/# /'
        exit 1
    else
        echo "1..0 # SKIP no Go tests found"
        exit 0
    fi
fi

echo "1..$total"

# Convert each test result to TAP
test_num=0
echo "$output" | while IFS= read -r line; do
    case "$line" in
        *"--- PASS:"*)
            test_num=$((test_num + 1))
            test_name=$(echo "$line" | sed 's/.*--- PASS: \([^ ]*\).*/\1/')
            duration=$(echo "$line" | grep -oE '\([0-9.]+s\)' || echo "")
            echo "ok $test_num - $test_name $duration"
            ;;
        *"--- FAIL:"*)
            test_num=$((test_num + 1))
            test_name=$(echo "$line" | sed 's/.*--- FAIL: \([^ ]*\).*/\1/')
            echo "not ok $test_num - $test_name"
            ;;
        *"--- SKIP:"*)
            test_num=$((test_num + 1))
            test_name=$(echo "$line" | sed 's/.*--- SKIP: \([^ ]*\).*/\1/')
            echo "ok $test_num - $test_name # SKIP"
            ;;
    esac
done

exit $exit_code
