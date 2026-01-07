#!/bin/sh
set -x

# Protection Features Integration Test Script (TAP format)
# Tests anti-spoofing, bogon filtering, rate limiting, etc.

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

# Check if nft is available
if ! command -v nft >/dev/null 2>&1; then
    echo "1..0 # SKIP nft command not found"
    exit 0
fi

# --- Start Test Suite ---

plan 22

diag "Starting protection features integration tests..."

# Clean up any existing test table
nft delete table inet test_protection 2>/dev/null

# --- Setup ---

nft add table inet test_protection
nft add chain inet test_protection prerouting '{ type filter hook prerouting priority -300; policy accept; }'
nft add chain inet test_protection input '{ type filter hook input priority 0; policy accept; }'

# --- Invalid Packet Filtering ---

# 1. Drop invalid conntrack state packets
diag "Testing invalid packet filtering..."
nft add rule inet test_protection prerouting ct state invalid drop
ok $? "Can add invalid packet drop rule"

# 2. Verify rule exists
nft list chain inet test_protection prerouting | grep -q "ct state invalid drop"
ok $? "Invalid packet rule visible in chain"

# --- Anti-Spoofing Tests ---

# 3. Block RFC1918 from WAN (simulated with iif check)
diag "Testing anti-spoofing rules..."
nft add rule inet test_protection prerouting ip saddr 10.0.0.0/8 iifname "eth0" drop
ok $? "Can add anti-spoof rule for 10.0.0.0/8"

# 4. Block 172.16.0.0/12 spoofing
nft add rule inet test_protection prerouting ip saddr 172.16.0.0/12 iifname "eth0" drop
ok $? "Can add anti-spoof rule for 172.16.0.0/12"

# 5. Block 192.168.0.0/16 spoofing
nft add rule inet test_protection prerouting ip saddr 192.168.0.0/16 iifname "eth0" drop
ok $? "Can add anti-spoof rule for 192.168.0.0/16"

# --- Bogon Filtering Tests ---

# 6. Block 0.0.0.0/8 (current network)
diag "Testing bogon filtering..."
nft add rule inet test_protection prerouting ip saddr 0.0.0.0/8 drop
ok $? "Can add bogon filter for 0.0.0.0/8"

# 7. Block 127.0.0.0/8 (loopback) from outside
nft add rule inet test_protection prerouting ip saddr 127.0.0.0/8 iifname "eth0" drop
ok $? "Can add bogon filter for 127.0.0.0/8"

# 8. Block 169.254.0.0/16 (link-local)
nft add rule inet test_protection prerouting ip saddr 169.254.0.0/16 iifname "eth0" drop
ok $? "Can add bogon filter for 169.254.0.0/16"

# 9. Block 224.0.0.0/4 (multicast as source)
nft add rule inet test_protection prerouting ip saddr 224.0.0.0/4 drop
ok $? "Can add bogon filter for 224.0.0.0/4 (multicast)"

# 10. Block 240.0.0.0/4 (reserved)
nft add rule inet test_protection prerouting ip saddr 240.0.0.0/4 drop
ok $? "Can add bogon filter for 240.0.0.0/4 (reserved)"

# 11. Block 255.255.255.255/32 (broadcast as source)
nft add rule inet test_protection prerouting ip saddr 255.255.255.255 drop
ok $? "Can add bogon filter for broadcast address"

# --- SYN Flood Protection Tests ---

# 12. Create SYN flood protection with rate limit
diag "Testing SYN flood protection..."
nft add rule inet test_protection input tcp flags syn limit rate 25/second burst 50 packets accept
ok $? "Can add SYN rate limit rule (25/sec burst 50)"

# 13. Drop excess SYN packets
nft add rule inet test_protection input tcp flags syn drop
ok $? "Can add SYN drop rule for excess packets"

# --- ICMP Rate Limiting Tests ---

# 14. Rate limit ICMP echo requests
diag "Testing ICMP rate limiting..."
nft add rule inet test_protection input ip protocol icmp icmp type echo-request limit rate 10/second accept
ok $? "Can add ICMP echo rate limit (10/sec)"

# 15. Drop excess ICMP
nft add rule inet test_protection input ip protocol icmp icmp type echo-request drop
ok $? "Can add ICMP drop rule for excess"

# --- Port Scan Detection ---

# 16. Detect and log port scans (many new connections)
diag "Testing port scan detection..."
nft add rule inet test_protection input ct state new limit rate over 60/minute log prefix '"PORTSCAN: "' drop
ok $? "Can add port scan detection rule"

# --- Bad Flag Combinations ---

# 17. Drop NULL packets (no flags)
diag "Testing bad TCP flag filtering..."
nft add rule inet test_protection prerouting tcp flags '& (fin|syn|rst|psh|ack|urg) == 0x0' drop
ok $? "Can add NULL packet drop rule"

# 18. Drop XMAS packets (all flags)
nft add rule inet test_protection prerouting tcp flags 'fin,psh,urg / fin,syn,rst,psh,ack,urg' drop
ok $? "Can add XMAS packet drop rule"

# --- Conntrack Limits ---

# 19. Check conntrack sysctl exists
diag "Testing conntrack configuration..."
if [ -f /proc/sys/net/netfilter/nf_conntrack_max ]; then
    cat /proc/sys/net/netfilter/nf_conntrack_max >/dev/null 2>&1
    ok $? "Can read nf_conntrack_max"
else
    skip "nf_conntrack_max not available"
fi

# 20. Check conntrack count
if [ -f /proc/sys/net/netfilter/nf_conntrack_count ]; then
    cat /proc/sys/net/netfilter/nf_conntrack_count >/dev/null 2>&1
    ok $? "Can read nf_conntrack_count"
else
    skip "nf_conntrack_count not available"
fi

# --- Verification ---

# 21. Count protection rules
diag "Verifying protection rules..."
RULE_COUNT=$(nft list table inet test_protection 2>/dev/null | grep -c "drop\|accept\|log")
[ "$RULE_COUNT" -ge 15 ]
ok $? "Protection table has expected rules ($RULE_COUNT rules)"

# --- Cleanup ---

# 22. Delete test table
nft delete table inet test_protection
ok $? "Can delete protection test table"

# --- End Test Suite ---

diag ""
diag "Protection features test summary:"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count protection tests passed!"
    exit 0
else
    diag "$failed_count of $test_count protection tests failed."
    exit 1
fi
