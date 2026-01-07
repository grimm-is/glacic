TEST_DIR="/mnt/glacic/build/tests"
set -x

if [ ! -d "$TEST_DIR" ]; then
    echo "1..1"
    echo "not ok 1 - Test directory not found: $TEST_DIR"
    exit 1
fi

# Count tests
count=$(find "$TEST_DIR" -name "*.test" | wc -l)
if [ "$count" -eq 0 ]; then
    echo "1..0 # SKIP no test binaries found in $TEST_DIR"
    exit 0
fi


# Ensure loopback is up (critical for tests binding to 127.0.0.1)
ip link set lo up 2>/dev/null || true

echo "1..$count"

i=1
for test_bin in "$TEST_DIR"/*.test; do
    name=$(basename "$test_bin" .test)

    # Run test binary
    # -test.v: verbose output
    # -test.failfast: stop on failure
    output=$($test_bin -test.v 2>&1)
    exit_code=$?

    if [ $exit_code -eq 0 ]; then
        echo "ok $i - $name"
    else
        echo "not ok $i - $name"
        echo "# Exit code: $exit_code"
        echo "$output" | sed 's/^/# /'
    fi
    i=$((i+1))
done
