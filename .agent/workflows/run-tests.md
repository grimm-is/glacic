---
description: How to run and debug integration tests
---

# Running Integration Tests

Integration tests run in a QEMU VM with a real Linux kernel.

## Quick Start

```bash
# Run all integration tests
make test-int

# Run a specific test file
./scripts/run-int-test.sh t/10-api/api_auth_test.sh

# Run tests matching a pattern
./scripts/run-int-test.sh t/30-firewall/
```

## Test Organization

Tests are in `t/` directory, organized by category:

| Directory | Purpose |
|-----------|---------|
| `t/01-sanity/` | Basic connectivity, sanity checks |
| `t/10-api/` | API authentication, CRUD operations |
| `t/20-dhcp/` | DHCP server functionality |
| `t/25-dns/` | DNS server functionality |
| `t/30-firewall/` | Firewall rules, zones, policies |
| `t/40-network/` | VLANs, bonds, NAT, routing |
| `t/50-security/` | IPSets, learning engine, threat intel |
| `t/60-vpn/` | WireGuard, Tailscale |
| `t/70-system/` | Config, upgrade, lifecycle |
| `t/80-monitoring/` | Metrics, nflog |
| `t/90-cli/` | CLI commands |

## Writing a New Test

Tests are shell scripts using TAP-style output with helpers from `common.sh`:

```bash
#!/bin/sh

# Test description (used by orchestrator)
# TEST_TIMEOUT: Override default timeout if needed
TEST_TIMEOUT=60

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

log() { echo "[TEST] $1"; }

# --- Setup ---
CONFIG_FILE="/tmp/test.hcl"
cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"
# ... test config ...
EOF

# Start services
log "Starting Control Plane..."
$APP_BIN ctl "$CONFIG_FILE" > /tmp/ctl.log 2>&1 &
CTL_PID=$!
track_pid $CTL_PID

wait_for_file $CTL_SOCKET 5 || fail "Control plane socket not created"

# --- Test Cases ---
log "Testing feature X..."
result=$(some_command)
if [ "$result" = "expected" ]; then
    log "PASS: Feature X works"
else
    fail "FAIL: Feature X returned $result"
fi

# --- Cleanup (automatic via cleanup_on_exit) ---
rm -f "$CONFIG_FILE"
exit 0
```

## Common Test Helpers

From `t/common.sh`:

```bash
# Setup
require_root           # Fail if not root
require_binary         # Ensure glacic binary exists
cleanup_on_exit        # Auto-cleanup on exit

# Process management
track_pid $PID         # Track for cleanup
wait_for_file PATH SEC # Wait for file to exist
wait_for_port PORT SEC # Wait for port to open
wait_for_api           # Wait for API to be ready

# Assertions
fail "message"         # Log failure and exit 1
log "message"          # Log test progress

# API helpers
api_get "/path"                    # GET with auth
api_post "/path" '{"json":true}'   # POST with auth
```

## Debugging Failed Tests

### 1. Run with Verbose Output

```bash
VERBOSE=1 ./scripts/run-int-test.sh t/10-api/api_auth_test.sh
```

### 2. Keep VM Running

```bash
# Start VM manually
make vm-start

# SSH into VM
ssh -p 2222 root@localhost

# Run test manually inside VM
/mnt/glacic/t/10-api/api_auth_test.sh
```

### 3. Check Logs

Inside VM:
```bash
# Glacic logs
journalctl -u glacic

# Check firewall rules
nft list ruleset

# Check interfaces
ip addr
```

### 4. Test API Manually

```bash
# Get API key
cat /var/lib/glacic/auth.json

# Call API
curl -s http://localhost:8080/api/status \
    -H "Authorization: Bearer <api-key>"
```

## Test Environment Variables

| Variable | Purpose |
|----------|---------|
| `API_URL` | API base URL (default: http://localhost:8080) |
| `API_KEY` | Authentication key |
| `VERBOSE` | Enable verbose output |
| `KEEP_VM` | Don't stop VM after tests |

## Common Issues

### "API server unreachable"

```bash
# Check if ctl and api are running
ps aux | grep glacic

# Check socket
ls -la /run/glacic/

# Wait longer for startup
sleep 10
```

### "Permission denied"

```bash
# Tests must run as root in VM
sudo -i
```

### "Test timeout"

- Increase timeout in test
- Check if command is hanging (use `timeout` wrapper)
