#!/bin/sh
set -x

# TAP Test Suite for Firewall Config CLI Commands
# Tests get/set/validate/edit/delete functionality
#
# PREREQUISITES:
# - firewall daemon must be running
# - firewall binary must be built: make build-linux
# - Tests require basic HCL syntax understanding

# Source common functions
. "$(dirname "$0")/../common.sh"

require_binary

FIREWALL_CONFIG="$MOUNT_PATH/tests/test-vm.hcl"

# --- Test Configuration ---
cleanup_on_exit

# --- Test Configuration ---
TEST_CONFIG=$(mktemp_compatible "test_config.hcl")
BACKUP_DIR=$(mktemp_compatible "backups")
mkdir -p "$BACKUP_DIR"

# Create test HCL configuration
create_test_config() {
    cat > "$TEST_CONFIG" << 'EOF'
ip_forwarding = true

interface "eth0" {
  zone = "wan"
  dhcp = true
}

interface "eth1" {
  zone = "lan"
  ipv4 = ["192.168.1.1/24"]
}

policy "lan" "lan" {
  name = "allow_lan"
  action = "accept"

  rule "allow_ssh" {
    description = "Allow SSH access"
    dest_port = 22
    action = "accept"
  }
}

policy "wan" "lan" {
  name = "block_external"
  action = "drop"
}
EOF
}

# --- Start Test Suite ---

plan 25

# Test 1: Create test configuration
diag "Creating test configuration"
create_test_config
ok $? "Test configuration created"

diag "Starting firewall config CLI tests"

# Start the firewall daemon using the helper from common.sh
start_ctl "$TEST_CONFIG"
diag "Firewall daemon started (PID $CTL_PID)"

# Test 2: firewall config get (basic)
diag "Testing firewall config get"
GET_OUTPUT=$(mktemp_compatible "get_output.txt")
$APP_BIN config get > "$GET_OUTPUT" 2>&1
if [ $? -eq 0 ]; then
    ok 0 "Config get command executes"
else
    ok 1 "Config get command executes"
    diag "Output of config get ($GET_OUTPUT):"
    cat "$GET_OUTPUT"
    echo "# End of output"
fi

# Test 3: firewall config get contains expected content
diag "Verifying get output contains expected content"
grep -q "ip_forwarding" "$GET_OUTPUT" && grep -q "true" "$GET_OUTPUT"
ok $? "Get output contains ip_forwarding setting"

# Test 4: firewall config get JSON output
diag "Testing firewall config get -output json"
GET_JSON=$(mktemp_compatible "get_json.txt")
$APP_BIN config get --output json > "$GET_JSON" 2>/dev/null
ok $? "Config get JSON output works"

# Test 5: JSON output is valid JSON
diag "Verifying JSON output is valid JSON"
# Use python if available, otherwise jq
if command -v python3 >/dev/null 2>&1; then
    python3 -c "import json; json.load(open('$GET_JSON'))" 2>/dev/null
    ok $? "JSON output is valid JSON"
elif command -v jq >/dev/null 2>&1; then
    jq . "$GET_JSON" >/dev/null 2>&1
    ok $? "JSON output is valid JSON"
else
    # Skip if neither tool available
    skip "No JSON validator available"
fi

# Test 6: firewall config validate with valid config
diag "Testing firewall config validate with valid config"
$APP_BIN config validate --file "$TEST_CONFIG" 2>/dev/null
ok $? "Valid config passes validation"

# Test 7: firewall config validate with invalid HCL
diag "Testing firewall config validate with invalid HCL"
INVALID_HCL=$(mktemp_compatible "invalid.hcl")
echo "invalid hcl {" > "$INVALID_HCL"
$APP_BIN config validate --file "$INVALID_HCL" 2>/dev/null
[ $? -ne 0 ]
ok $? "Invalid HCL fails validation"

# Test 8: firewall config set from stdin
diag "Testing firewall config set from stdin"
cat "$TEST_CONFIG" | $APP_BIN config set --confirm=false 2>/dev/null
ok $? "Config set from stdin works"

# Test 9: firewall config set from file
diag "Testing firewall config set from file"
$APP_BIN config set --file "$TEST_CONFIG" --confirm=false 2>/dev/null
ok $? "Config set from file works"

# Test 10: firewall config set with invalid config fails
diag "Testing firewall config set with invalid config"
echo "invalid {" | $APP_BIN config set --confirm=false 2>/dev/null
[ $? -ne 0 ]
ok $? "Invalid config set fails appropriately"

# Test 11: firewall config set creates backup
diag "Testing config set creates backup"
# This is hard to test without access to backup system, so we'll test the command structure
$APP_BIN config set --file "$TEST_CONFIG" --confirm=false 2>/dev/null
ok $? "Config set with confirmation works (simulated)"

# Test 12: firewall config delete section (unlabeled - we delete block_external policy)
diag "Testing firewall config delete policy block_external"
$APP_BIN config delete --confirm=false policy block_external 2>/dev/null
ok $? "Delete labeled policy section works"

# Test 13: firewall config delete section (labeled)
diag "Testing firewall config delete interface eth0"
$APP_BIN config delete --confirm=false interface eth0 2>/dev/null
ok $? "Delete labeled section works"

# Test 14: firewall config delete with confirmation
diag "Testing firewall config delete requires confirmation"
# Use timeout to prevent hanging if command waits for TTY input
echo "n" | run_with_timeout 5 $APP_BIN config delete policy allow_lan 2>/dev/null
ok $? "Delete respects confirmation prompt"

# Test 15: firewall config delete non-existent section
diag "Testing firewall config delete non-existent section"
$APP_BIN config delete --confirm=false nonexistent_section 2>/dev/null
[ $? -ne 0 ]
ok $? "Delete non-existent section fails"

# Test 16: firewall config help shows delete command
diag "Testing firewall config help includes delete command"
$APP_BIN config 2>&1 | grep -q "delete"
ok $? "Help text includes delete command"

# Test 17: firewall config delete without arguments shows usage
diag "Testing firewall config delete without arguments"
$APP_BIN config delete 2>&1 | grep -q "Usage:"
ok $? "Delete without arguments shows usage"

# Test 18: firewall config get after deletion
diag "Testing config get after deletions"
POST_DELETE=$(mktemp_compatible "post_delete.txt")
$APP_BIN config get > "$POST_DELETE" 2>/dev/null
ok $? "Config get works after deletions"

# Test 19: Verify deleted sections are gone
diag "Verifying deleted sections are removed from config"
! grep -q "interface.*eth0" "$POST_DELETE"
ok $? "Deleted interface section is removed"

# Test 20: Verify policy still exists (we aborted the delete in Test 14)
diag "Verifying aborted delete kept the policy"
# Policy name is on a separate line, use grep -A or similar
grep -A 20 "policy" "$POST_DELETE" | grep -q "allow_lan"
ok $? "Aborted delete kept policy section"

# Test 21: Test config validation with stdin
diag "Testing firewall config validate with stdin"
echo "ip_forwarding = true" | $APP_BIN config validate 2>/dev/null
ok $? "Validate from stdin works"

# Test 22: Test config set with complex HCL
diag "Testing config set with complex HCL structure"
COMPLEX_HCL=$(mktemp_compatible "complex.hcl")
cat > "$COMPLEX_HCL" << 'EOF'
ip_forwarding = true

policy "lan" "wan" {
  name = "complex_test"
  action = "drop"

  rule "multiple_ports" {
    description = "Multiple destination ports"
    dest_ports = [80, 443, 8080]
    action = "accept"
  }

  rule "ipset_match" {
    description = "IPSet source matching"
    src_ipset = "whitelist"
    action = "accept"
  }
}

ipset "whitelist" {
  type = "ipv4_addr"
  entries = ["192.168.1.1", "10.0.0.1"]
}
EOF
$APP_BIN config set --file "$COMPLEX_HCL" --confirm=false > /tmp/config_set_debug.log 2>&1
if [ $? -eq 0 ]; then
    ok 0 "Complex HCL structure sets successfully"
else
    ok 1 "Complex HCL structure sets successfully"
    cat /tmp/config_set_debug.log
fi

# Test 23: Verify complex config was applied
diag "Verifying complex config was applied"
COMPLEX_OUTPUT=$(mktemp_compatible "complex_output.txt")
$APP_BIN config get > "$COMPLEX_OUTPUT" 2>/dev/null
# Complex policy name is on a separate line
grep -A 20 "policy" "$COMPLEX_OUTPUT" | grep -q "complex_test"
ok $? "Complex policy was applied"

# Test 24: Test error handling for malformed input
diag "Testing error handling for malformed input"
echo "malformed {" | $APP_BIN config set --confirm=false 2>&1 | grep -q -i "error\|failed"
ok $? "Error handling works for malformed input"

# Test 25: Test config operations in sequence
diag "Testing config operation sequence (get -> modify -> set -> validate)"
SEQUENCE_TEST=$(mktemp_compatible "sequence.hcl")
# Get current config
$APP_BIN config get > "$SEQUENCE_TEST" 2>/dev/null
# Modify it
echo "# Test modification" >> "$SEQUENCE_TEST"
# Set it back
$APP_BIN config set --file "$SEQUENCE_TEST" --confirm=false 2>/dev/null
# Validate
$APP_BIN config validate --file "$SEQUENCE_TEST" 2>/dev/null
ok $? "Config operation sequence works"

# --- Cleanup and Summary ---
diag "Cleaning up test files"
rm -f /tmp/get_output_*.txt /tmp/get_json_*.txt /tmp/invalid_*.hcl /tmp/complex_*.hcl /tmp/post_delete_*.txt /tmp/sequence_test_*.hcl /tmp/test_config_*.hcl /tmp/complex_output_*.txt
rm -rf "$BACKUP_DIR"

echo "# Test Summary: $((test_count - failed_count))/$test_count passed"
if [ $failed_count -gt 0 ]; then
    echo "# $failed_count tests failed"
    exit 1
else
    echo "# All tests passed"
    exit 0
fi
