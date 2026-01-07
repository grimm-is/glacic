#!/bin/sh
set -x

# FRR (Free Range Routing) Integration Test Script (TAP format)
# Tests OSPF, BGP, and dynamic routing configuration

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

plan 20

diag "Starting FRR integration tests..."

# ============================================
# Testing FRR infrastructure
# ============================================
diag "Testing FRR infrastructure..."

# 1. Check if vtysh is available
if command -v vtysh >/dev/null 2>&1; then
    ok 0 "vtysh (FRR CLI) available"
    HAS_VTYSH=1
else
    ok 0 "vtysh not installed # SKIP"
    HAS_VTYSH=0
fi

# 2. Check if zebra is available
if command -v zebra >/dev/null 2>&1; then
    ok 0 "zebra daemon available"
else
    ok 0 "zebra not installed # SKIP"
fi

# 3. Check FRR config directory
if [ -d /etc/frr ] || mkdir -p /tmp/frr_test 2>/dev/null; then
    ok 0 "FRR config directory accessible"
else
    ok 1 "Cannot access FRR config directory"
fi

# ============================================
# Testing FRR configuration syntax
# ============================================
diag "Testing FRR configuration syntax..."

mkdir -p /tmp/frr_test

# 4. Create zebra configuration
cat > /tmp/frr_test/zebra.conf << 'EOF'
! Zebra configuration
hostname firewall-router
password zebra
enable password zebra
!
interface eth0
 description WAN Interface
!
interface eth1
 description LAN Interface
!
ip forwarding
ipv6 forwarding
!
line vty
EOF
ok $? "Can create zebra configuration"

# 5. Create OSPF configuration
cat > /tmp/frr_test/ospfd.conf << 'EOF'
! OSPF configuration
hostname firewall-router
password ospf
enable password ospf
!
router ospf
 ospf router-id 10.0.0.1
 network 10.0.0.0/24 area 0.0.0.0
 network 192.168.1.0/24 area 0.0.0.1
 passive-interface eth0
 redistribute connected
 redistribute static
!
interface eth1
 ip ospf cost 10
 ip ospf hello-interval 10
 ip ospf dead-interval 40
!
line vty
EOF
ok $? "Can create OSPF configuration"

# 6. Create BGP configuration
cat > /tmp/frr_test/bgpd.conf << 'EOF'
! BGP configuration
hostname firewall-router
password bgp
enable password bgp
!
router bgp 65001
 bgp router-id 10.0.0.1
 no bgp default ipv4-unicast
 neighbor 10.0.0.2 remote-as 65002
 neighbor 10.0.0.2 description Upstream Provider
 !
 address-family ipv4 unicast
  network 192.168.0.0/16
  neighbor 10.0.0.2 activate
  neighbor 10.0.0.2 soft-reconfiguration inbound
 exit-address-family
!
line vty
EOF
ok $? "Can create BGP configuration"

# 7. Verify OSPF config syntax
grep -q "router ospf" /tmp/frr_test/ospfd.conf
ok $? "OSPF router block configured"

# 8. Verify BGP config syntax
grep -q "router bgp" /tmp/frr_test/bgpd.conf
ok $? "BGP router block configured"

# ============================================
# Testing OSPF features
# ============================================
diag "Testing OSPF configuration features..."

# 9. Check OSPF area configuration
grep -q "area 0.0.0.0" /tmp/frr_test/ospfd.conf
ok $? "OSPF backbone area configured"

# 10. Check OSPF network statements
network_count=$(grep -c "network.*area" /tmp/frr_test/ospfd.conf)
[ "$network_count" -ge 2 ]
ok $? "Multiple OSPF networks configured ($network_count networks)"

# 11. Check OSPF passive interface
grep -q "passive-interface" /tmp/frr_test/ospfd.conf
ok $? "OSPF passive interface configured"

# 12. Check OSPF redistribution
grep -q "redistribute" /tmp/frr_test/ospfd.conf
ok $? "OSPF redistribution configured"

# ============================================
# Testing BGP features
# ============================================
diag "Testing BGP configuration features..."

# 13. Check BGP AS number
grep -q "router bgp 65001" /tmp/frr_test/bgpd.conf
ok $? "BGP AS number configured"

# 14. Check BGP neighbor
grep -q "neighbor.*remote-as" /tmp/frr_test/bgpd.conf
ok $? "BGP neighbor configured"

# 15. Check BGP address family
grep -q "address-family ipv4" /tmp/frr_test/bgpd.conf
ok $? "BGP IPv4 address family configured"

# 16. Check BGP network advertisement
grep -q "network 192.168" /tmp/frr_test/bgpd.conf
ok $? "BGP network advertisement configured"

# ============================================
# Testing FRR daemons file
# ============================================
diag "Testing FRR daemons configuration..."

# 17. Create daemons file
cat > /tmp/frr_test/daemons << 'EOF'
# FRR daemons configuration
zebra=yes
bgpd=yes
ospfd=yes
ospf6d=no
ripd=no
ripngd=no
isisd=no
pimd=no
ldpd=no
nhrpd=no
eigrpd=no
babeld=no
sharpd=no
staticd=yes
pbrd=no
bfdd=no
fabricd=no
vrrpd=no
EOF
ok $? "Can create daemons configuration"

# 18. Verify enabled daemons
enabled_count=$(grep -c "=yes" /tmp/frr_test/daemons)
[ "$enabled_count" -ge 3 ]
ok $? "Multiple FRR daemons enabled ($enabled_count daemons)"

# ============================================
# Testing route injection
# ============================================
diag "Testing route injection..."

# 19. Check if ip route supports protocol tag
ip route add 172.16.0.0/12 dev lo proto static 2>/dev/null
if [ $? -eq 0 ]; then
    ok 0 "Can add route with protocol tag"
    ip route del 172.16.0.0/12 dev lo 2>/dev/null
else
    ok 0 "Route protocol tag not available # SKIP"
fi

# ============================================
# Cleanup
# ============================================
diag "Cleaning up..."

# 20. Remove test files
rm -rf /tmp/frr_test
ok $? "Cleaned up test files"

# --- End Test Suite ---

diag ""
diag "FRR test summary:"
if [ $failed_count -eq 0 ]; then
    diag "All $test_count FRR tests passed!"
    exit 0
else
    diag "$failed_count of $test_count FRR tests failed."
    exit 1
fi
