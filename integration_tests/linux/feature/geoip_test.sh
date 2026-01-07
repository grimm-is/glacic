#!/bin/sh
set -x
# GeoIP Matching Integration Test
# Verifies that source_country and dest_country fields generate correct nftables rules.
# Note: This test uses dry-run mode since we don't have a real GeoIP database in CI.

TEST_TIMEOUT=30

# Source common functions
. "$(dirname "$0")/../common.sh"

require_root
require_binary

# --- Setup ---
plan 4

diag "Starting GeoIP Matching Test"

cleanup() {
    diag "Cleanup..."
    rm -f "$TEST_CONFIG" 2>/dev/null
}
trap cleanup EXIT

# 1. Create config with GeoIP rules
TEST_CONFIG=$(mktemp_compatible "geoip_test.hcl")
cat > "$TEST_CONFIG" << 'EOF'
schema_version = "1.0"
ip_forwarding = true

interface "eth0" {
  description = "WAN Link"
  dhcp = true
}

interface "veth-lan" {
  zone = "lan"
  ipv4 = ["192.168.100.1/24"]
}

zone "lan" {
  interfaces = ["veth-lan"]
}

zone "WAN" {
  interfaces = ["eth0"]
}

# Enable GeoIP (database path doesn't need to exist for dry-run)
geoip {
  enabled = true
  database_path = "/var/lib/glacic/geoip/GeoLite2-Country.mmdb"
}

# Policy with GeoIP blocking: Block traffic from uninhabited territories (test only)
policy "WAN" "lan" {
  name = "wan_to_lan_geoblock"
  action = "drop"

  rule "block_antarctica" {
    description = "Block traffic from Antarctica"
    source_country = "AQ"
    action = "drop"
  }

  rule "block_bouvet" {
    description = "Block traffic from Bouvet Island"
    source_country = "BV"
    action = "drop"
  }

  rule "allow_heard_island" {
    description = "Allow traffic from Heard Island"
    source_country = "HM"
    action = "accept"
  }
}

# Policy with destination country (blocking traffic TO uninhabited territories)
policy "lan" "WAN" {
  name = "lan_to_wan_geoblock"
  action = "accept"

  rule "block_outbound_to_french_southern" {
    description = "Block outbound traffic to French Southern Territories"
    dest_country = "TF"
    action = "drop"
  }
}
EOF

ok 0 "GeoIP config created"

# 2. Validate config with check command
diag "Validating GeoIP config..."
$APP_BIN check "$TEST_CONFIG"
if [ $? -eq 0 ]; then
    ok 0 "Config validation passed"
else
    ok 1 "Config validation failed"
fi

# 3. Dry-run to see generated rules
diag "Running in dry-run mode to see generated nftables..."
OUTPUT=$($APP_BIN apply --dry-run "$TEST_CONFIG" 2>&1)

# Check that rules reference GeoIP sets
if echo "$OUTPUT" | grep -q "@geoip_country_AQ"; then
    ok 0 "Antarctica source_country rule generated"
else
    ok 1 "Antarctica source_country rule NOT found in output"
    diag "Output was:"
    echo "$OUTPUT" | head -100
fi

# Check destination country rule
if echo "$OUTPUT" | grep -q "@geoip_country_TF"; then
    ok 0 "French Southern Territories dest_country rule generated"
else
    ok 1 "French Southern Territories dest_country rule NOT found in output"
    diag "Output was:"
    echo "$OUTPUT" | head -100
fi

if [ $failed_count -eq 0 ]; then
    exit 0
else
    exit 1
fi
