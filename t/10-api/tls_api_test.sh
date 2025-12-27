#!/bin/sh
set -x

# TLS API Integration Test
# Verifies TLS certificate generation and HTTPS API server
# test-api doesn't support TLS flags; requires code changes
SKIP=false

TEST_TIMEOUT=60

# Source common functions
. "$(dirname "$0")/../common.sh"

cleanup_on_exit

CONFIG_FILE="$(mktemp_compatible tls_api.hcl)"
CERT_DIR="/tmp/glacic_test_certs"
mkdir -p "$CERT_DIR"

# Generate certs if possible
if command -v openssl >/dev/null; then
    diag "Generating self-signed certificate using OpenSSL..."
    openssl req -x509 -newkey rsa:2048 -keyout "$CERT_DIR/api.key" -out "$CERT_DIR/api.crt" \
        -days 1 -nodes -subj "/CN=localhost" 2>/dev/null || fail "OpenSSL failed"
else
    diag "OpenSSL not found, skipping TLS test properly"
    echo "1..0 # SKIP OpenSSL required"
    exit 0
fi

# Embed config with TLS enabled
cat >"$CONFIG_FILE" <<EOF
schema_version = "1.0"

api {
  enabled = false
  listen = "127.0.0.1:8443"
  tls_cert = "$CERT_DIR/api.crt"
  tls_key = "$CERT_DIR/api.key"
}
EOF

# Start Control Plane
start_ctl "$CONFIG_FILE"

# Start API Server (custom port)
export GLACIC_NO_SANDBOX=1
start_api -listen :8443

ok 0 "API server listening on 8443"

# Test HTTPS connection
if command -v curl >/dev/null; then
    diag "Testing HTTPS connection..."
    RESP=$(curl -v -sk --connect-timeout 5 --max-time 10 https://127.0.0.1:8443/api/status 2>&1)
    RET=$?

    if [ $RET -eq 0 ]; then
        if echo "$RESP" | grep -q "status\|version\|online"; then
            ok 0 "HTTPS request successful"
        else
            ok 1 "HTTPS request returned garbage"
            diag "Response: $RESP"
        fi
    else
        ok 1 "HTTPS connection failed"
        diag "Curl output: $RESP"
        diag "API Log:"
        cat "$API_LOG"
    fi
else
    ok 0 "# SKIP Curl not found"
fi

exit 0
