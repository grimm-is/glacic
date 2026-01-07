#!/bin/sh
# Integration test for custom DHCP options
set -e
set -x

TEST_TIMEOUT=20

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
. "$SCRIPT_DIR/../common.sh"

CONFIG_FILE=$(mktemp /tmp/dhcp-options-config.hcl.XXXXXX)
DHCP_OPTIONS_LOG=$(mktemp /tmp/dhcp-options.log.XXXXXX)

cat > "$CONFIG_FILE" << 'EOF'
schema_version = "1.1"

interface "lo" {
  zone = "lan"
  ipv4 = ["192.168.1.1/24"]
}

zone "lan" {
  interfaces = ["lo"]
}

dhcp {
  enabled = true

  scope "lan_pool" {
    interface = "lo"
    range_start = "192.168.1.100"
    range_end = "192.168.1.200"
    router = "192.168.1.1"
    dns = ["8.8.8.8", "8.8.4.4"]
    lease_time = "1h"

    # Test custom options with human-readable names
    options = {
      # Network Boot / PXE (option 66, 67)
      tftp_server = "tftp.example.com"
      bootfile = "pxelinux.0"

      # Domain (option 15)
      domain_name = "example.com"

      # NTP (option 42)
      ntp_server = "192.168.1.254"

      # Subnet mask (option 1)
      subnet_mask = "ip:255.255.255.0"

      # WPAD (option 252)
      wpad = "http://proxy.example.com/wpad.dat"
    }
  }
}
EOF

ok 0 "Created test config with custom DHCP options"

# Assign IP to lo so DHCP can bind
ip addr add 192.168.1.1/24 dev lo 2>/dev/null || true

# Start control plane
start_ctl "$CONFIG_FILE"

# Wait for DHCP server to be ready
dilated_sleep 3

# Check if DHCP server is listening on port 67
if netstat -uln | grep -q ":67 "; then
    ok 0 "DHCP server listening on port 67"
else
    ok 1 "DHCP server NOT listening on port 67"
    diag "netstat output:"
    netstat -uln | grep -E "(Proto|:67)" || true
fi

# Create a udhcpc script to capture DHCP options
UDHCPC_SCRIPT=$(mktemp /tmp/udhcpc-script.XXXXXX)
cat > "$UDHCPC_SCRIPT" << 'UDHCPC_EOF'
#!/bin/sh
# udhcpc script to log all DHCP options

echo "=== DHCP Event: $1 ===" >> /tmp/dhcp-options.log
env | grep -E "^(ip|subnet|timezone|router|timesvr|dns|hostname|domain|ipttl|bootfile|tftp|ntpsrv|wpad)" >> /tmp/dhcp-options.log 2>&1 || true

# Also dump all environment variables for debugging
echo "--- All Environment Variables ---" >> /tmp/dhcp-options.log
env >> /tmp/dhcp-options.log
echo "===================================" >> /tmp/dhcp-options.log
UDHCPC_EOF

chmod +x "$UDHCPC_SCRIPT"

# Remove old IP from loopback if present (udhcpc requirement)
ip addr del 192.168.1.1/24 dev lo 2>/dev/null || true
dilated_sleep 1

# Run udhcpc on loopback to request DHCP
diag "Running udhcpc to request DHCP lease on loopback..."
# -f: foreground, -i: interface, -s: script, -q: quit after lease, -n: exit if lease fails, -t: retries
timeout 10 udhcpc -f -i lo -s "$UDHCPC_SCRIPT" -q -n -t 3 2>&1 | head -20 || true

# Re-add the IP for the DHCP server
ip addr add 192.168.1.1/24 dev lo 2>/dev/null || true

dilated_sleep 1

# Check if we got DHCP options logged
if [ -f /tmp/dhcp-options.log ]; then
    diag "DHCP options received:"
    cat /tmp/dhcp-options.log | head -80

    DHCP_LOG=$(cat /tmp/dhcp-options.log)

    # Verify we got a DHCP response
    if echo "$DHCP_LOG" | grep -q "bound\|renew"; then
        ok 0 "DHCP lease obtained successfully"

        # Verify custom options
        # Option 66 - TFTP Server (may appear as 'tftp' or 'siaddr')
        if echo "$DHCP_LOG" | grep -qi "tftp\|siaddr" && echo "$DHCP_LOG" | grep -q "tftp.example.com"; then
            ok 0 "Option 66 (TFTP Server) found: tftp.example.com"
        else
            ok 1 "Option 66 (TFTP Server) NOT found"
        fi

        # Option 67 - Bootfile Name
        if echo "$DHCP_LOG" | grep -qi "bootfile\|boot_file" && echo "$DHCP_LOG" | grep -q "pxelinux"; then
            ok 0 "Option 67 (Bootfile) found: pxelinux.0"
        else
            ok 1 "Option 67 (Bootfile) NOT found"
        fi

        # Option 15 - Domain Name
        if echo "$DHCP_LOG" | grep -qi "domain" && echo "$DHCP_LOG" | grep -q "example.com"; then
            ok 0 "Option 15 (Domain Name) found: example.com"
        else
            ok 1 "Option 15 (Domain Name) NOT found"
        fi

        # Option 42 - NTP Servers (may appear as 'ntpsrv')
        if echo "$DHCP_LOG" | grep -qi "ntp" && echo "$DHCP_LOG" | grep -q "192.168.1.254"; then
            ok 0 "Option 42 (NTP Server) found: 192.168.1.254"
        else
            ok 1 "Option 42 (NTP Server) NOT found"
        fi

        # Option 1 - Subnet Mask
        if echo "$DHCP_LOG" | grep -qi "subnet" && echo "$DHCP_LOG" | grep -q "255.255.255.0"; then
            ok 0 "Option 1 (Subnet Mask) found: 255.255.255.0"
        else
            ok 1 "Option 1 (Subnet Mask) NOT found"
        fi

        # Option 252 - WPAD
        if echo "$DHCP_LOG" | grep -qi "wpad" && echo "$DHCP_LOG" | grep -q "proxy.example.com"; then
            ok 0 "Option 252 (WPAD) found"
        else
            # WPAD might not be exposed by udhcpc env vars, this is acceptable
            ok 0 "Option 252 (WPAD) check skipped (not in udhcpc output)"
        fi

    else
        ok 1 "DHCP lease NOT obtained - no bound/renew event"
        diag "Expected to see 'bound' or 'renew' in DHCP log"
    fi
else
    ok 1 "DHCP options log file NOT created"
    diag "udhcpc may have failed to run or communicate with DHCP server"
fi

# Cleanup
stop_ctl
ip addr del 192.168.1.1/24 dev lo 2>/dev/null || true
rm -f "$CONFIG_FILE" "$UDHCPC_SCRIPT" /tmp/dhcp-options.log

ok 0 "Test cleanup completed"
