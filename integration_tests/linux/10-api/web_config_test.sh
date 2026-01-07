#!/bin/bash
# Test new web {} block configuration

source "$(dirname "$0")/../common.sh"

test_description="Web Block Configuration Test"
test_count=3

# Create config with explicit web block
cat > glacic.hcl <<EOF
api {
    enabled = true
    require_auth = false
}

web {
    listen = ":9090"
    serve_ui = true
    
    # Allow from localhost explicitly
    allow {
        interfaces = ["lo"]
        sources = ["127.0.0.1"]
    }
    
    # Deny specific test IP
    deny {
        sources = ["1.2.3.4"]
    }
}

interface "lo" {
    # Legacy flag should be ignored in favor of web block?
    # Or additive. But here we test explicit web block.
    # ipv4 = ... (default lo has verified address usually)
}

interface "eth0" {
    ipv4 = ["127.0.0.2/8"] # Alias for testing
}
EOF

# Start control plane (enable API proxy spawning)
export GLACIC_SKIP_API=0
start_ctl "glacic.hcl"

# Wait for proxy to bind to custom port 9090
diag "Waiting for Proxy on port 9090..."
wait_for_port 9090
pass_count=$((pass_count+1))

# Test 1: Access allowed from allowed source (127.0.0.1) on allowed interface (lo)
diag "Test 1: Allowed access (127.0.0.1 -> 9090)"
if curl -s --connect-timeout 2 http://127.0.0.1:9090/api/status > /dev/null; then
    echo "ok $pass_count - Allowed access worked"
    pass_count=$((pass_count+1))
else
    echo "not ok $pass_count - Allowed access failed"
    fail_count=$((fail_count+1))
fi

# Test 2: Access denied from denied source (mocked via source IP if possible? Hard with curl locally)
# We can check if firewall rule exists using nft list.
diag "Test 2: Verify Deny Rule generation"
if $SUDO nft list table inet filter | grep -q "1.2.3.4"; then
    echo "ok $pass_count - Deny rule found in nftables"
    pass_count=$((pass_count+1))
else
    echo "not ok $pass_count - Deny rule missing"
    $SUDO nft list table inet filter
    echo "# CTL LOG OUTPUT:"
    cat "$CTL_LOG"
    fail_count=$((fail_count+1))
fi

stop_ctl
# cleanup_test # handled by trap
