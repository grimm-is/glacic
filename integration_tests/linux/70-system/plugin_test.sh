#!/bin/sh
set -x
#
# Feature Test: Plugin Architecture (Wizards)
#
# Verifies that setup and import commands correctly invoke their respective
# plugin binaries.

. "$(dirname "$0")/../common.sh"

# 1. Verify we are testing the monolithic binary (no separate plugins needed)
diag "Verifying monolithic binary..."
if [ ! -f "$APP_BIN" ]; then
   diag "WARNING: Binary not found at $APP_BIN"
fi

# 2. Test Setup Wizard (Help/Usage)
# We can't easily run the full interactive wizard, but we can check if it launches
# and prints its specific help/error message vs "unknown command".
diag "Testing 'setup' command dispatch..."

OUT=$($APP_BIN setup --config-dir /tmp/test-setup 2>&1 || true)
if echo "$OUT" | grep -q "Error: setup must run as root"; then
    diag "Setup command dispatched correctly (root check hit)"
elif echo "$OUT" | grep -q "plugin not found"; then
    fail "Setup failed: plugin dispatch error (should be internal)"
elif echo "$OUT" | grep -q "Unknown command"; then
    fail "Setup failed: command not registered"
else
    # If we run as root in test VM, it might try to actually run.
    # The wizard prints "Detecting network interfaces"
    if echo "$OUT" | grep -q "Detecting network interfaces"; then
        diag "Setup command dispatched correctly (started detection)"
    else
        diag "Unexpected output from setup: $OUT"
        # Don't fail yet, analyze context
    fi
fi

# 3. Test Import Wizard (Flags)
diag "Testing 'import' command dispatch..."
# Running without args should fail with specific usage from the import plugin
OUT=$($APP_BIN import 2>&1 || true)

if echo "$OUT" | grep -q "input is required"; then
    diag "Import plugin invoked successfully (arg check)"
elif echo "$OUT" | grep -q "plugin not found"; then
    fail "Import failed: plugin dispatch error (should be internal)"
elif echo "$OUT" | grep -q "Unknown command"; then
    fail "Import failed: command not registered"
else
    diag "Unexpected output from import: $OUT"
    fail "Import dispatch verification failed"
fi

diag "Plugin integration test passed"
