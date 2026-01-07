#!/bin/sh
set -x

# Prometheus Metrics Integration Test Script (TAP format)
# Tests metrics collection, formatting, and export

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

# --- Start Test Suite ---

plan 18

diag "Starting Prometheus metrics integration tests..."

# ============================================
# Testing metrics format
# ============================================
diag "Testing Prometheus metrics format..."

# Create test metrics directory
mkdir -p /tmp/metrics_test

# 1. Create sample counter metric
cat > /tmp/metrics_test/counter.prom << 'EOF'
# HELP firewall_packets_total Total packets processed
# TYPE firewall_packets_total counter
firewall_packets_total{interface="eth0",direction="in"} 123456
firewall_packets_total{interface="eth0",direction="out"} 98765
firewall_packets_total{interface="eth1",direction="in"} 54321
firewall_packets_total{interface="eth1",direction="out"} 43210
EOF
ok $? "Can create counter metric file"

# 2. Verify HELP line format
grep -q "^# HELP" /tmp/metrics_test/counter.prom
ok $? "Counter has HELP line"

# 3. Verify TYPE line format
grep -q "^# TYPE.*counter$" /tmp/metrics_test/counter.prom
ok $? "Counter has TYPE line"

# 4. Verify metric with labels
grep -qE '^firewall_packets_total\{.*\} [0-9]+$' /tmp/metrics_test/counter.prom
ok $? "Counter metric format valid"

# ============================================
# Testing gauge metrics
# ============================================
diag "Testing gauge metrics..."

# 5. Create sample gauge metric
cat > /tmp/metrics_test/gauge.prom << 'EOF'
# HELP firewall_connections_active Current active connections
# TYPE firewall_connections_active gauge
firewall_connections_active{zone="lan"} 42
firewall_connections_active{zone="wan"} 128
firewall_connections_active{zone="dmz"} 15

# HELP firewall_memory_bytes Memory usage in bytes
# TYPE firewall_memory_bytes gauge
firewall_memory_bytes{type="heap"} 15728640
firewall_memory_bytes{type="stack"} 1048576
EOF
ok $? "Can create gauge metric file"

# 6. Verify gauge TYPE
grep -q "^# TYPE.*gauge$" /tmp/metrics_test/gauge.prom
ok $? "Gauge has correct TYPE"

# 7. Count metric lines (excluding comments)
metric_count=$(grep -v "^#" /tmp/metrics_test/gauge.prom | grep -c "firewall_")
[ "$metric_count" -ge 4 ]
ok $? "Multiple gauge metrics present ($metric_count metrics)"

# ============================================
# Testing histogram metrics
# ============================================
diag "Testing histogram metrics..."

# 8. Create sample histogram metric
cat > /tmp/metrics_test/histogram.prom << 'EOF'
# HELP firewall_request_duration_seconds Request duration histogram
# TYPE firewall_request_duration_seconds histogram
firewall_request_duration_seconds_bucket{le="0.005"} 24054
firewall_request_duration_seconds_bucket{le="0.01"} 33444
firewall_request_duration_seconds_bucket{le="0.025"} 100392
firewall_request_duration_seconds_bucket{le="0.05"} 129389
firewall_request_duration_seconds_bucket{le="0.1"} 133988
firewall_request_duration_seconds_bucket{le="+Inf"} 144320
firewall_request_duration_seconds_sum 53423
firewall_request_duration_seconds_count 144320
EOF
ok $? "Can create histogram metric file"

# 9. Verify histogram buckets
bucket_count=$(grep -c "_bucket" /tmp/metrics_test/histogram.prom)
[ "$bucket_count" -ge 5 ]
ok $? "Histogram has buckets ($bucket_count buckets)"

# 10. Verify histogram _sum and _count
grep -q "_sum" /tmp/metrics_test/histogram.prom && grep -q "_count" /tmp/metrics_test/histogram.prom
ok $? "Histogram has _sum and _count"

# ============================================
# Testing firewall-specific metrics
# ============================================
diag "Testing firewall-specific metrics..."

# 11. Create firewall metrics
cat > /tmp/metrics_test/firewall.prom << 'EOF'
# HELP firewall_rule_matches_total Number of times each rule matched
# TYPE firewall_rule_matches_total counter
firewall_rule_matches_total{chain="input",rule="allow_ssh"} 1542
firewall_rule_matches_total{chain="input",rule="drop_invalid"} 89
firewall_rule_matches_total{chain="forward",rule="lan_to_wan"} 45678

# HELP firewall_blocked_ips_total IPs blocked by ipset
# TYPE firewall_blocked_ips_total counter
firewall_blocked_ips_total{ipset="firehol_level1"} 234
firewall_blocked_ips_total{ipset="spamhaus_drop"} 56

# HELP firewall_ipset_size Number of entries in each ipset
# TYPE firewall_ipset_size gauge
firewall_ipset_size{name="firehol_level1"} 15234
firewall_ipset_size{name="spamhaus_drop"} 892
firewall_ipset_size{name="trusted_ips"} 12
EOF
ok $? "Can create firewall-specific metrics"

# 12. Verify rule match metrics
grep -q "firewall_rule_matches_total" /tmp/metrics_test/firewall.prom
ok $? "Rule match metrics present"

# 13. Verify IPSet metrics
grep -q "firewall_ipset_size" /tmp/metrics_test/firewall.prom
ok $? "IPSet size metrics present"

# ============================================
# Testing network metrics from /proc
# ============================================
diag "Testing system network metrics..."

# 14. Read network interface stats
if [ -f /proc/net/dev ]; then
    iface_count=$(cat /proc/net/dev | tail -n +3 | wc -l)
    [ "$iface_count" -ge 1 ]
    ok $? "Can read /proc/net/dev ($iface_count interfaces)"
else
    ok 0 "/proc/net/dev not available # SKIP"
fi

# 15. Read conntrack stats
if [ -f /proc/sys/net/netfilter/nf_conntrack_count ]; then
    ct_count=$(cat /proc/sys/net/netfilter/nf_conntrack_count)
    ok 0 "Can read conntrack count ($ct_count)"
else
    ok 0 "Conntrack stats not available # SKIP"
fi

# 16. Read nftables counters (if available)
if command -v nft >/dev/null 2>&1; then
    # Create test table with counter
    nft add table inet metrics_test 2>/dev/null
    nft add chain inet metrics_test test 2>/dev/null
    nft add rule inet metrics_test test counter 2>/dev/null
    nft list chain inet metrics_test test 2>/dev/null | grep -q "counter"
    ok $? "Can read nftables counters"
    nft delete table inet metrics_test 2>/dev/null
else
    ok 0 "nft not available # SKIP"
fi

# ============================================
# Testing metrics aggregation
# ============================================
diag "Testing metrics aggregation..."

# 17. Combine all metrics
cat /tmp/metrics_test/*.prom > /tmp/metrics_test/combined.prom 2>/dev/null
total_metrics=$(grep -v "^#" /tmp/metrics_test/combined.prom | grep -c "^[a-z]")
[ "$total_metrics" -ge 15 ]
ok $? "Can aggregate metrics ($total_metrics total)"

# ============================================
# Cleanup
# ============================================
diag "Cleaning up..."

# 18. Remove test files
rm -rf /tmp/metrics_test
ok $? "Cleaned up test files"

# --- End Test Suite ---

diag ""
diag "Metrics test summary:"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count metrics tests passed!"
    exit 0
else
    diag "$failed_count of $test_count metrics tests failed."
    exit 1
fi
