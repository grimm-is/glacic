#!/bin/sh
set -x
# Feature Test: Management Granularity & Implicit Zones
# tests/features/management_granularity_test.sh

source "$(dirname "$0")/../common.sh"

# Configuration
TEST_NAME="mgmt_granularity"
GO_TEST_TAGS="integration"
TEST_TIMEOUT=30 # Custom timeout if needed

# 1. Define Firewall Config (HCL)
cat > /tmp/${TEST_NAME}.hcl <<EOF
schema_version = "1.0"
ip_forwarding = true

# Zone Definitions (Defaults)
zone "LAN" {
    description = "Default LAN"
    action = "drop"
    management {
        ssh = true       # Default: SSH allowed
        web = false      # Default: Web blocked
    }
}

zone "GUEST" {
    description = "Guest Network"
    action = "drop"
    management {
        ssh = false
        web = false
    }
}

# Interface Definitions (Overrides)
interface "veth_eth1" {
    description = "LAN Interface (Use Zone Default)"
    zone        = "LAN"
    ipv4        = ["10.1.0.1/24"]
    # Should inherit: SSH=true, Web=false
}

interface "veth_eth2" {
    description = "Admin Interface (Override Zone)"
    zone        = "LAN" # Still in LAN zone!
    ipv4        = ["10.2.0.1/24"]

    management {
        ssh = true
        web = false  # Web disabled
        api = true   # API enabled (Should allow HTTPS/443)
    }
}

interface "veth_eth3" {
    description = "Guest Interface (Implicit Zone Test)"
    # No Zone defined! Implicit zone "veth_eth3"
    ipv4        = ["10.3.0.1/24"]
}

# Policies
policy "LAN" "WAN" {
    name = "lan_to_wan"
    rule "allow_icmp" {
        proto  = "icmp"
        action = "accept"
    }
}

policy "veth_eth3" "WAN" {
    name = "eth3_to_wan"
    rule "allow_icmp_eth3" {
        proto  = "icmp"
        action = "accept"
    }
}

interface "veth_eth0" {
    description = "WAN"
    zone        = "WAN"
    ipv4        = ["10.0.0.1/24"]
}
EOF

# 2. Setup Test Environment (Namespaces)
setup_custom_topology() {
    # Create client namespaces
    ip netns add client_lan1    # Attached to eth1 (Default LAN)
    ip netns add client_admin   # Attached to eth2 (Admin LAN)
    ip netns add client_guest   # Attached to eth3 (Implicit)
    ip netns add client_wan     # Attached to eth0 (WAN)

    # Wire up connections (simulated with veth pairs)
    # eth1 <-> client_lan1
    ip link add veth_eth1 type veth peer name veth_c1
    ip link set veth_eth1 up
    ip link set veth_c1 netns client_lan1

    # eth2 <-> client_admin
    ip link add veth_eth2 type veth peer name veth_c2
    ip link set veth_eth2 up
    ip link set veth_c2 netns client_admin

    # eth3 <-> client_guest
    ip link add veth_eth3 type veth peer name veth_c3
    ip link set veth_eth3 up
    ip link set veth_c3 netns client_guest

    # eth0 <-> client_wan
    ip link add veth_eth0 type veth peer name veth_c0
    ip link set veth_eth0 up
    ip link set veth_c0 netns client_wan

    # Configure Glacic Host Interfaces
    ip addr add 10.1.0.1/24 dev veth_eth1
    ip addr add 10.2.0.1/24 dev veth_eth2
    ip addr add 10.3.0.1/24 dev veth_eth3
    ip addr add 10.0.0.1/24 dev veth_eth0

    # Configure Client Namespaces (just basic up)
    ip netns exec client_lan1 ip link set veth_c1 up
    ip netns exec client_admin ip link set veth_c2 up
    ip netns exec client_guest ip link set veth_c3 up
    ip netns exec client_wan ip link set veth_c0 up
}

cleanup_test() {
    ip netns del client_lan1 2>/dev/null
    ip netns del client_admin 2>/dev/null
    ip netns del client_guest 2>/dev/null
    ip link del veth_eth1 2>/dev/null
    ip link del veth_eth2 2>/dev/null
    ip link del veth_eth3 2>/dev/null
    ip link del veth_eth1 2>/dev/null
    ip link del veth_eth2 2>/dev/null
    ip link del veth_eth3 2>/dev/null

    # Restore config if needed (handled by runner usually)
}

# 3. Run Glacic and Tests
setup_custom_topology
# Use apply_firewall_rules from common.sh
apply_firewall_rules "/tmp/${TEST_NAME}.hcl" /tmp/${TEST_NAME}.log

# Verify NFTABLES Rules directly to be sure
# Since apply_firewall_rules is synchronous provided via common.sh
NFT_RULES=$(nft list ruleset)

# Helper to checking rules with debug
check_rule() {
    _regex="$1"
    _msg="$2"
    if echo "$NFT_RULES" | grep -E -q "$_regex"; then
        ok 0 "$_msg"
    else
        ok 1 "$_msg"
        diag "Expected regex: $_regex"
        # diag "Ruleset snippet:"
        # echo "$NFT_RULES" | grep "veth" | head -10
    fi
}

# Test 1: LAN1 (Default) - SSH Allowed, Web Blocked
diag "Testing LAN1 (Default Zone Config)..."
# Test 1a: SSH on eth1 (veth_eth1) -- Expecting map syntax: "veth_eth1" . 22
check_rule '"veth_eth1" \. 22' "Implicit Zone Default: SSH allowed on LAN1 (in map)"

# Test 1b: Web NOT on eth1 (80 or 443)
if echo "$NFT_RULES" | grep -E -q '"veth_eth1" \. (80|443)'; then
    ok 1 "Implicit Zone Default: Web blocked on LAN1 (Found allow rule!)"
else
    ok 0 "Implicit Zone Default: Web blocked on LAN1"
fi

# Test 2: Admin (Override) - SSH Allowed, API Allowed (Web False)
diag "Testing Admin (Interface Override)..."
# SSH
check_rule '"veth_eth2" \. 22' "Override: SSH allowed on Admin (in map)"

# Web blocked check? No, API=true allows 80/443.
check_rule '"veth_eth2" \. (80|443)' "Override: HTTP Port 80/443 allowed (concatenation set)"


# Test 3: Implicit Zone (veth_eth3)
diag "Testing Implicit Zone (veth_eth3)..."
# Check chain existence
echo "$NFT_RULES" | grep 'chain policy_veth_eth3_WAN' >/dev/null
ok $? "Implicit Zone chain 'policy_veth_eth3_WAN' created"

# Check jump rule (vmap syntax: "veth_eth3" . "veth_eth0" : jump policy_veth_eth3_WAN)
check_rule '"veth_eth3" .* : jump policy_veth_eth3_WAN' "Jump rule for Implicit Zone created (vmap)"

cleanup_test
exit 0
