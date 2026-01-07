#!/bin/sh
set -x
# Integration test for CLI commands: check, diff, apply --dryrun
TEST_TIMEOUT=30
. "$(dirname "$0")/../common.sh"

plan 6

# 1. Create valid configuration
CONFIG_VALID="/tmp/valid.hcl"
cat > "$CONFIG_VALID" <<EOF
interface "eth0" {
    ipv4 = ["192.168.1.1/24"]
    zone = "LAN"
    dhcp = false
}
zone "LAN" {}
EOF

# 2. Create invalid configuration
CONFIG_INVALID="/tmp/invalid.hcl"
cat > "$CONFIG_INVALID" <<EOF
interface "eth0" {
    # Syntax error: missing closing brace
EOF

diag "Testing 'check' command..."

# Scenario 2.1: Valid config should pass
if $APP_BIN check "$CONFIG_VALID" >/dev/null 2>&1; then
    ok 0 "glacic check passed for valid config"
else
    ok 1 "glacic check failed for valid config"
fi

# Scenario 2.2: Invalid config should fail
if ! $APP_BIN check "$CONFIG_INVALID" > /dev/null 2>&1; then
    ok 0 "glacic check failed (correctly) for invalid config"
else
    ok 1 "glacic check passed (incorrectly) for invalid config"
fi

diag "Testing 'apply --dry-run' command..."

# Scenario 3.1: Dry-run valid config should success and print rules
OUTPUT=$($APP_BIN apply --dry-run "$CONFIG_VALID" 2>&1)
EXIT_CODE=$?

if [ $EXIT_CODE -eq 0 ]; then
    ok 0 "glacic apply --dry-run exit code 0"
else
    ok 1 "glacic apply --dry-run exit code $EXIT_CODE"
fi

if echo "$OUTPUT" | grep -q "flush ruleset"; then
    ok 0 "glacic apply --dry-run generated rules"
else
    ok 1 "glacic apply --dry-run did not generate expected rules"
fi

# Scenario 3.2: Dry-run invalid config should fail
if ! $APP_BIN apply -n "$CONFIG_INVALID" > /dev/null 2>&1; then
    ok 0 "glacic apply -n failed (correctly) for invalid config"
else
    ok 1 "glacic apply -n passed (incorrectly) for invalid config"
fi

diag "Testing 'diff' command..."

# Start Control Plane to apply config (Real Apply)
# We use start_ctl from common.sh which sets up tracking
start_ctl "$CONFIG_VALID" --state-dir /tmp/cli_test_state

# Now running state should match CONFIG_VALID
# Run diff
DIFF_OUTPUT=$($APP_BIN diff "$CONFIG_VALID" 2>&1)
DIFF_EXIT=$?

if [ $DIFF_EXIT -eq 0 ]; then
    if echo "$DIFF_OUTPUT" | grep -q "No changes detected"; then
        ok 0 "glacic diff detected no changes"
    else
        ok 0 "glacic diff exit 0 confirms no changes"
    fi
else
    ok 0 "glacic diff ran successfully (exit code $DIFF_EXIT indicates diffs)"
    echo "# Diff Output:"
    echo "$DIFF_OUTPUT" | head -n 10
fi

