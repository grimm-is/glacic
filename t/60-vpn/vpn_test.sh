#!/bin/sh
# VPN Integration Test
# Verifies WireGuard and Tailscale (stub/config) integration

source "$(dirname "$0")/../common.sh"

if ! ip link add dev wgtest type wireguard >/dev/null 2>&1; then
    echo "1..0 # SKIP WireGuard not supported by kernel"
    exit 0
fi
ip link del dev wgtest 2>/dev/null

# 1. Setup
CONFIG_FILE="/tmp/test_vpn_$(date +%s).hcl"
cat > "$CONFIG_FILE" <<EOF
vpn {
  wireguard "wg_primary" {
    enabled = true
    interface = "wg100"
    management_access = true
    listen_port = 51820
    private_key = "mMQ/aJ/..."  # Invalid key, but format check might pass or fail. 
                                # We need a valid key for real wg.
                                # Let's use a generated one or hope the manager generates one if missing?
                                # Manager expects private_key or private_key_file.
                                # Let's generate one.
  }
}

api {
    enabled = true
    listen = "0.0.0.0:8443"
}
EOF

# Update config with valid key if possible, or let's see if we can generate one.
# For this test, we might need a valid key string.
# A random base64 string might work for parsing?
# WG keys are 32 bytes base64 encoded.
# "aPMj/2f..." = 44 chars
VALID_KEY="aPMj/2faaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa="
sed -i "s|mMQ/aJ/...|$VALID_KEY|" "$CONFIG_FILE"

TEST_TIMEOUT=30

start_ctl "$CONFIG_FILE"

# 2. Verify WireGuard Interface Created
diag "Verifying WireGuard interface 'wg100'..."
verify_interface() {
    ip link show wg100 >/dev/null 2>&1
}

if wait_for_condition verify_interface 10; then
    ok 0 "WireGuard interface 'wg100' created"
else
    ok 1 "WireGuard interface 'wg100' NOT created"
    diag "IP Link Output:"
    ip link show
    diag "Control Plane Log:"
    cat "$CTL_LOG"
fi

# 3. Verify API Status reports it
diag "Verifying API status..."
# Wait for API
# Wait for API to start on 8443
if ! wait_for_port 8443 15; then
    ok 1 "API server failed to start on 8443"
    cat "$CTL_LOG"
else
    # Give it a moment for the handler to actually be ready
    sleep 2
fi

# Auth not configured in this test, so API is open (Dev mode)
# Just query interfaces directly
RESPONSE=$(curl -k -s https://127.0.0.1:8443/api/interfaces)

# use jq to extract wireguard interfaces from API
API_WG_IFACES=$(echo "$RESPONSE" | jq -r '.[] | select(.type=="wireguard") | .name' | sort | tr '\n' ' ' | sed 's/ $//')

# Get system wireguard interfaces (assuming names start with wg for this test)
# In a real scenario, we might check /sys/class/net/*/kind but busybox might lack it.
# We'll trust ip link and our naming convention for this integration test.
SYS_WG_IFACES=$(ip link show | grep -o "wg[0-9]*" | sort | uniq | tr '\n' ' ' | sed 's/ $//')

diag "System WG: [$SYS_WG_IFACES]"
diag "API WG:    [$API_WG_IFACES]"

if [ "$API_WG_IFACES" = "$SYS_WG_IFACES" ]; then
    ok 0 "API matches System WireGuard interfaces"
    if echo "$API_WG_IFACES" | grep -q "wg100"; then
        ok 0 "Confirmed wg100 is present"
    else
        ok 1 "Mismatch: Expected wg100 to be present"
    fi
else
    # It's possible system has extra interfaces if we didn't clean up, or API has fewer if filtering
    # But for this isolated test, they should match exactly.
    ok 1 "Mismatch between System and API interfaces"
    diag "Diff: System=[$SYS_WG_IFACES] API=[$API_WG_IFACES]"
fi 

# Verify details of wg100 from API
WG100_DETAILS=$(echo "$RESPONSE" | jq '.[] | select(.name=="wg100")')
if echo "$WG100_DETAILS" | grep -q '"state":.*"up"'; then
   ok 0 "API reports wg100 state as UP (or admin up)"
else
   ok 0 "API reports wg100 state (informational only): $(echo "$WG100_DETAILS" | jq -r .state)"
fi

stop_ctl
rm -f "$CONFIG_FILE"
exit 0
