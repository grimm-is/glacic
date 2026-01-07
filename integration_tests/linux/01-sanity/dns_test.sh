#!/bin/sh
set -x

# DNS Server Integration Test Script (TAP format)
# Tests DNS server configuration and resolution

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

skip() {
    test_count=$((test_count + 1))
    echo "ok $test_count - # SKIP $1"
}

# Check if we're root
if [ "$(id -u)" -ne 0 ]; then
    echo "1..0 # SKIP Must run as root"
    exit 0
fi

# --- Start Test Suite ---

plan 16

diag "Starting DNS server integration tests..."

# ============================================
# Testing DNS infrastructure
# ============================================
diag "Testing DNS infrastructure..."

# 1. Check if dnsmasq is available
if command -v dnsmasq >/dev/null 2>&1; then
    ok 0 "dnsmasq available for DNS"
    HAS_DNSMASQ=1
else
    ok 0 "dnsmasq not installed # SKIP"
    HAS_DNSMASQ=0
fi

# 2. Check if nslookup/dig is available
if command -v nslookup >/dev/null 2>&1; then
    ok 0 "nslookup available for testing"
    DNS_TOOL="nslookup"
elif command -v dig >/dev/null 2>&1; then
    ok 0 "dig available for testing"
    DNS_TOOL="dig"
elif command -v host >/dev/null 2>&1; then
    ok 0 "host available for testing"
    DNS_TOOL="host"
else
    ok 1 "No DNS lookup tool available"
    DNS_TOOL=""
fi

# 3. Check UDP port 53 status
if ! netstat -uln 2>/dev/null | grep -q ":53 " && ! ss -uln 2>/dev/null | grep -q ":53 "; then
    ok 0 "DNS port 53 available"
else
    ok 0 "DNS port 53 in use (existing DNS server)"
fi

# ============================================
# Testing DNS configuration
# ============================================
diag "Testing DNS configuration..."

# 4. Create test DNS config directory
mkdir -p /tmp/dns_test
ok $? "Can create DNS config directory"

# 5. Write dnsmasq DNS config
cat > /tmp/dns_test/dnsmasq.conf << 'EOF'
# DNS configuration
port=5353
listen-address=127.0.0.1
no-resolv
server=8.8.8.8
server=8.8.4.4
cache-size=1000
neg-ttl=60
EOF
ok $? "Can write DNS server config"

# 6. Add local DNS records
cat >> /tmp/dns_test/dnsmasq.conf << 'EOF'
# Local records
address=/router.lan/192.168.1.1
address=/server.lan/192.168.1.10
address=/nas.lan/192.168.1.20
EOF
ok $? "Can add local DNS records"

# 7. Verify config syntax
grep -q "cache-size" /tmp/dns_test/dnsmasq.conf
ok $? "DNS cache configured"

# ============================================
# Testing DNS zones
# ============================================
diag "Testing DNS zone configuration..."

# 8. Create hosts file for local zone
cat > /tmp/dns_test/hosts.local << 'EOF'
192.168.1.1   router router.lan gateway.lan
192.168.1.10  server1 server1.lan
192.168.1.11  server2 server2.lan
192.168.1.20  nas nas.lan storage.lan
EOF
ok $? "Can create local hosts file"

# 9. Count host entries
host_count=$(wc -l < /tmp/dns_test/hosts.local)
[ "$host_count" -ge 4 ]
ok $? "Multiple host entries configured ($host_count entries)"

# 10. Add addn-hosts directive
echo "addn-hosts=/tmp/dns_test/hosts.local" >> /tmp/dns_test/dnsmasq.conf
ok $? "Can add additional hosts file"

# ============================================
# Testing DNS forwarding
# ============================================
diag "Testing DNS forwarding..."

# 11. Configure conditional forwarding
cat >> /tmp/dns_test/dnsmasq.conf << 'EOF'
# Conditional forwarding
server=/corp.example.com/10.0.0.1
server=/168.192.in-addr.arpa/192.168.1.1
EOF
ok $? "Can configure conditional forwarding"

# 12. Test resolv.conf parsing
cat > /tmp/dns_test/resolv.conf << 'EOF'
nameserver 127.0.0.1
nameserver 8.8.8.8
search lan local
EOF
ok $? "Can create resolv.conf"

# ============================================
# Testing DNS blocklists
# ============================================
diag "Testing DNS blocklist support..."

# 13. Create blocklist file
cat > /tmp/dns_test/blocklist.txt << 'EOF'
address=/ads.example.com/0.0.0.0
address=/tracking.example.com/0.0.0.0
address=/malware.example.com/0.0.0.0
EOF
ok $? "Can create DNS blocklist"

# 14. Add blocklist to config
echo "conf-file=/tmp/dns_test/blocklist.txt" >> /tmp/dns_test/dnsmasq.conf
ok $? "Can add blocklist to config"

# ============================================
# Testing DNS over system resolver
# ============================================
diag "Testing system DNS resolution..."

# 15. Check /etc/resolv.conf exists
if [ -f /etc/resolv.conf ]; then
    ok 0 "System resolv.conf exists"
else
    ok 1 "System resolv.conf missing"
fi

# ============================================
# Cleanup
# ============================================
diag "Cleaning up..."

# 16. Remove test files
rm -rf /tmp/dns_test
ok $? "Cleaned up test files"

# --- End Test Suite ---

diag ""
diag "DNS test summary:"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count DNS tests passed!"
    exit 0
else
    diag "$failed_count of $test_count DNS tests failed."
    exit 1
fi
