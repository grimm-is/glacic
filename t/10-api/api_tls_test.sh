#!/bin/sh

# API TLS Integration Test
# Verifies that API server generates self-signed certs and serves HTTPS.

# Source common functions
. "$(dirname "$0")/../common.sh"

TEST_TIMEOUT=120
CONFIG_FILE="/tmp/api_tls.hcl"
CERT_FILE="/var/lib/glacic/certs/test_api.crt"
KEY_FILE="/var/lib/glacic/certs/test_api.key"
LOG_FILE="/var/log/glacic/glacic.log"

# Clean up previous run
rm -f "$CERT_FILE" "$KEY_FILE" "$LOG_FILE"
mkdir -p /var/log/glacic
mkdir -p /var/lib/glacic/certs

# Embed config
cat >"$CONFIG_FILE" <<EOF
schema_version = "1.0"

interface "lo" {
  zone = "lan"
  ipv4 = ["127.0.0.1/8"]
}

zone "lan" {
  interfaces = ["lo"]
}

api {
  enabled = true
  tls_cert = "$CERT_FILE"
  tls_key = "$KEY_FILE"
}
EOF

test_count=0
failed_count=0

plan() {
	echo "1..$1"
}

ok() {
	test_count=$((test_count + 1))
	if [ "$1" -eq 0 ]; then
		echo "ok $test_count - $2"
	else
		echo "not ok $test_count - $2"
		failed_count=$((failed_count + 1))
	fi
}

diag() {
	echo "# $1"
}

# --- Start Test Suite ---

plan 4

debug_logs() {
    diag "--- GLACIC LOG ---"
    if [ -f "$LOG_FILE" ]; then
        cat "$LOG_FILE" | sed 's/^/# /'
    else
        diag "Log file NOT found at $LOG_FILE"
    fi
    diag "------------------"
}

diag "Starting API TLS Test..."

# 1. Start Control Plane (which starts API)
$APP_BIN ctl "$CONFIG_FILE" >/dev/null 2>&1 &
CTL_PID=$!
diag "Started CTL (PID $CTL_PID)"

trap "kill $CTL_PID 2>/dev/null; rm -f $CONFIG_FILE $CERT_FILE $KEY_FILE $LOG_FILE" EXIT

diag "Waiting for API startup..."

# Wait blindly for valid startup logs to appear (timeouts debugging)
diag "Sleeping 20s to allow startup..."
sleep 20

diag "Checking logs now..."
debug_logs

# Check for API start line in the log file we just dumped (or check file content directly)
if [ -f "$LOG_FILE" ] && grep -q "Starting API server on .* (HTTPS)" "$LOG_FILE"; then
    diag "API started (detected in log)"
    ok 0 "API started with TLS"
else
    diag "API start message not found in log"
    ok 1 "API start message not found"
fi

# 2. Check if cert files were created
if [ -f "$CERT_FILE" ] && [ -f "$KEY_FILE" ]; then
    ok 0 "Certificate files generated"
else
    ok 1 "Certificate files NOT generated"
    ls -l $(dirname "$CERT_FILE") 2>/dev/null
    debug_logs
fi

# 3. Test HTTPS connection
# Use curl with -k (insecure) because self-signed
if ! command -v curl >/dev/null 2>&1; then
    apk add curl >/dev/null 2>&1 || true
fi

diag "Connecting to HTTPS..."
# Force HTTP/1.1 or check protocol support? Just -k is enough.
response=$(curl -k -s -o /dev/null -w "%{http_code}" https://127.0.0.1:8443/api/status)

if [ "$response" = "200" ] || [ "$response" = "401" ]; then
     # 200 is OK, 401 is Unauthorized (which means server is running and auth is enabled/disabled)
     # If auth is disabled, it should be 200.
    ok 0 "HTTPS request to /api/status returned $response"
else
    ok 1 "HTTPS request failed with code: $response"
    diag "Curl output:"
    curl -k -v https://127.0.0.1:8443/api/status 2>&1 | sed 's/^/# /'
fi

# 4. Verify log message
if grep -q "Starting API server on .* (HTTPS)" "$LOG_FILE"; then
    ok 0 "Log confirms TLS startup"
else
    ok 1 "Log missing TLS startup message"
fi

# 5. Check IPv6 behavior (Expect failure/unreachable as IPv6 is not yet configured in netns)
diag "Checking IPv6 connectivity..."
if curl -k --connect-timeout 2 -s -o /dev/null https://[::1]:8443/api/status; then
    diag "IPv6 connection SUCCEEDED (Unexpected but good?)"
else
    diag "IPv6 connection failed as expected (Not configured)"
fi

exit $failed_count
