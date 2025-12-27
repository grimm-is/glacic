#!/bin/sh
set -x

# DNS Blocklist URL Integration Test
# Verifies that DNS blocklists can be downloaded from a URL and enforced.

# Source common functions
. "$(dirname "$0")/../common.sh"

TEST_TIMEOUT=60
BLOCKLIST_PORT=8088
BLOCKLIST_URL="http://127.0.0.1:$BLOCKLIST_PORT/blocklist.txt"

CONFIG_FILE="/tmp/dns_blocklist_url.hcl"

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

dns {
  enabled = true
  listen_on = ["0.0.0.0"]
  
  # Forward to Google by default
  forwarders = ["8.8.8.8"]

  blocklist "test_url_list" {
    url = "$BLOCKLIST_URL"
    enabled = true
  }
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

diag "Starting DNS Blocklist URL Test..."

# 1. Start Mock HTTP Server
# We use a simple loop with netcat to serve the file
# The blocklist contains "blocked-domain.com"
mkdir -p /tmp/www
echo "0.0.0.0 blocked-domain.com" > /tmp/www/blocklist.txt

# Start simple http server
if command -v python3 >/dev/null 2>&1; then
    cd /tmp/www && python3 -m http.server $BLOCKLIST_PORT >/tmp/http_server.log 2>&1 &
    HTTP_PID=$!
    diag "Started Python HTTP server on port $BLOCKLIST_PORT (PID $HTTP_PID)"
else
    # Fallback to busybox httpd or netcat if python missing
    diag "Python3 not found, skipping test (or implement nc fallback)"
    exit 0
fi

trap "kill $HTTP_PID 2>/dev/null; pkill glacic_ctl; rm -f $CONFIG_FILE" EXIT

# 2. Start Glacic
$APP_BIN ctl "$CONFIG_FILE" >/tmp/glacic_dns.log 2>&1 &
CTL_PID=$!

diag "Waiting for startup and blocklist download..."
sleep 5

# Check if it tried to download
if grep -q "Loaded .* domains from blocklist" /tmp/glacic_dns.log; then
    ok 0 "Blocklist download logged"
else
    diag "Log dump:"
    cat /tmp/glacic_dns.log | sed 's/^/# /'
    ok 1 "Blocklist download NOT logged"
fi

# 3. Test DNS resolution
# We use 'dig' or 'nslookup'
if ! command -v dig >/dev/null 2>&1; then
    apk add bind-tools >/dev/null 2>&1 || true
fi

# Query blocked domain
# Should return NXDOMAIN (0.0.0.0 is mapped to NXDOMAIN in our implementation currently? 
# code says: msg.Rcode = dns.RcodeNameError // NXDOMAIN)
diag "Querying blocked-domain.com..."
result=$(dig +short @127.0.0.1 -p 53 blocked-domain.com)

if [ -z "$result" ]; then
    # dig +short returns nothing for NXDOMAIN usually, or we check status
    status=$(dig @127.0.0.1 -p 53 blocked-domain.com | grep "status:" | awk '{print $6}' | tr -d ',')
    if [ "$status" = "NXDOMAIN" ]; then
        ok 0 "blocked-domain.com returned NXDOMAIN"
    else
        ok 1 "blocked-domain.com status: $status (Expected NXDOMAIN)"
    fi
else
    ok 1 "blocked-domain.com resolved to: $result (Should be blocked)"
fi

# 4. Check cache file creation
if [ -d "/var/lib/glacic/blocklist_cache" ]; then
    ok 0 "Blocklist cache directory created"
else
    ok 1 "Blocklist cache directory missing"
fi

exit $failed_count
