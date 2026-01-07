#!/bin/sh
set -x

# UID Routing Test
# Verifies: UID routing generates correct nftables mark rules

TEST_TIMEOUT=30
. "$(dirname "$0")/../common.sh"

plan 4

require_root
require_binary

cleanup_on_exit

diag "Test: UID Routing Configuration"

# Create test config with UID routing
TEST_CONFIG=$(mktemp_compatible uid_route.hcl)
cat > "$TEST_CONFIG" <<EOF
schema_version = "1.0"

interface "lo" {
  zone = "lan"
  ipv4 = ["127.0.0.1/8"]
}

interface "eth0" {
  zone = "wan"
  dhcp = true
  table = 10
}

zone "lan" {
  interfaces = ["lo"]
}

zone "wan" {
  interfaces = ["eth0"]
}

# UID routing: route user 65534 (nobody) via eth0 uplink
uid_routing "test_user" {
  uid = 65534
  uplink = "eth0"
  enabled = true
}
EOF

# Start control plane
diag "Starting control plane..."
start_ctl "$TEST_CONFIG"
ok 0 "Control plane started with UID routing config"

# Wait for rules to be applied
dilated_sleep 2

# Check if UID routing rules appear in nftables
# Should see: meta skuid 65534 meta mark set ...
NFT_OUTPUT=$(nft list table inet glacic 2>/dev/null || echo "")
if echo "$NFT_OUTPUT" | grep -q "skuid.*65534"; then
    ok 0 "UID routing rule found in nftables (skuid 65534)"
else
    ok 1 "UID routing rule not found in nftables"
    diag "Expected: meta skuid 65534 meta mark set ..."
    diag "NFT Output snippet:"
    echo "$NFT_OUTPUT" | grep -E "skuid|uid" || echo "(no uid-related rules found)"
fi

# Verify output chain exists for connmark restore
if echo "$NFT_OUTPUT" | grep -q "output"; then
    ok 0 "Output chain present for mark restore"
else
    ok 1 "Output chain missing"
fi

# Verify firewall table created
if nft list table inet glacic >/dev/null 2>&1; then
    ok 0 "Firewall table created"
else
    ok 1 "Firewall table missing"
fi

rm -f "$TEST_CONFIG"
exit 0

