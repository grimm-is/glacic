#!/bin/sh
set -x

# Fail2Ban-Style Blocking Integration Test
# Verifies automatic IP blocking after repeated auth failures
# TEST_TIMEOUT: Extra time for repeated requests
TEST_TIMEOUT=30

. "$(dirname "$0")/../common.sh"

require_root
require_binary
cleanup_on_exit

log() { echo "[TEST] $1"; }

CONFIG_FILE="/tmp/fail2ban.hcl"

# Configuration using dev auth (default) but checking blocking logic
cat > "$CONFIG_FILE" <<EOF
schema_version = "1.0"

api {
  enabled = true
  listen  = "127.0.0.1:8084"
  require_auth = true
}

# Create blocked_ips IPSet for fail2ban
ipset "blocked_ips" {
  type = "ipv4_addr"
  entries = []
}
EOF

# Start Control Plane
log "Starting Control Plane..."
start_ctl "$CONFIG_FILE"

# Start API Server
log "Starting API Server..."
start_api -listen :8084

TEST_IP="10.99.99.1"
log "Using Test IP: $TEST_IP"

if command -v curl > /dev/null 2>&1; then
    # Test 1: API server handles authentication and blocks IP
    log "Test 1: Fail2Ban blocking verification"

    # Verify IP not blocked initially
    if nft list set inet glacic blocked_ips | grep -q "$TEST_IP"; then
        fail "IP $TEST_IP already blocked!"
    fi

    log "Sending failed attempts from $TEST_IP (via X-Forwarded-For)..."
    for i in $(seq 1 6); do
        curl -s -X POST http://127.0.0.1:8084/api/auth/login \
            -H "X-Forwarded-For: $TEST_IP" \
            -d '{"username":"testuser","password":"wrongpassword"}' \
            -H "Content-Type: application/json" > /dev/null 2>&1
        dilated_sleep 0.2
    done

    # Wait for async RPC to complete (increased time)
    sleep 3

    # Verify IP is in blocked_ips set
    log "Verifying IP $TEST_IP is blocked in nftables..."
    if nft list set inet glacic blocked_ips | grep -q "$TEST_IP"; then
        pass "IP $TEST_IP successfully blocked in nftables"
    else
        log "FAILURE: IP $TEST_IP NOT found in blocked_ips set"
        log "--- IPSet Content ---"
        nft list set inet glacic blocked_ips || true
        log "--- API Logs (Content) ---"
        if [ -n "$API_LOG" ] && [ -f "$API_LOG" ]; then
            cat "$API_LOG"
        else
            log "No API log found (API_LOG=$API_LOG)"
        fi
        fail "IP $TEST_IP NOT found in blocked_ips set"
    fi

    log "Fail2ban integration test completed successfully"
else
    fail "curl required for this test"
fi

# Cleanup
rm -f "$CONFIG_FILE"
exit 0
