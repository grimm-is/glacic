#!/bin/sh
set -x

# TLS API Integration Test
# Verifies TLS termination at the proxy layer with custom certificates.
# Architecture: Client --HTTPS--> Proxy (:8443) --HTTP--> API (Unix socket)

TEST_TIMEOUT=30

# Source common functions
. "$(dirname "$0")/../common.sh"

cleanup_on_exit

CONFIG_FILE="$(mktemp_compatible tls_api.hcl)"
CERT_DIR="/tmp/glacic_test_certs"
mkdir -p "$CERT_DIR"

# Generate certs if possible (use smaller key for speed)
if command -v openssl >/dev/null; then
    diag "Generating self-signed certificate using OpenSSL (1024-bit for speed)..."
    openssl req -x509 -newkey rsa:1024 -keyout "$CERT_DIR/api.key" -out "$CERT_DIR/api.crt" \
        -days 1 -nodes -subj "/CN=localhost" 2>/dev/null || fail "OpenSSL failed"
else
    diag "OpenSSL not found, skipping TLS test properly"
    echo "1..0 # SKIP OpenSSL required"
    exit 0
fi

# Embed config with TLS enabled via control plane
cat >"$CONFIG_FILE" <<EOF
schema_version = "1.0"

interface "lo" {
  ipv4 = ["127.0.0.1/8"]
}

zone "local" {}

api {
  enabled = true
  listen = ""
  tls_listen = "127.0.0.1:8443"
  tls_cert = "$CERT_DIR/api.crt"
  tls_key = "$CERT_DIR/api.key"
  require_auth = false
}
EOF

# Start Control Plane (this starts API with TLS)
export GLACIC_SKIP_API=0
start_ctl "$CONFIG_FILE"

# Wait for TLS port
if ! wait_for_port 8443 15; then
    fail "TLS API server failed to start on port 8443"
fi

ok 0 "API server listening on 8443 with TLS"

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

if [ "$failed_count" -gt 0 ]; then
    exit 1
fi
exit 0
