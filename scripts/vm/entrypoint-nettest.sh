#!/bin/sh
# Minimal network test - no firewall
trap "poweroff" EXIT

mount -t proc none /proc 2>/dev/null
mount -t sysfs none /sys 2>/dev/null

# Configure network
ip link set lo up
ip link set eth0 up
udhcpc -i eth0 -q -s /sbin/udhcpc-script 2>/dev/null || udhcpc -i eth0 -q

echo ""
echo "=== QEMU NETWORKING TEST ==="
IP=$(ip -4 addr show eth0 | grep inet | awk '{print $2}' | cut -d/ -f1)
echo "VM IP: $IP"
echo ""

echo "Starting nc HTTP loop on port 8080..."
echo "(Host should run: curl http://localhost:8080/)"
echo ""

# Minimal HTTP server loop
while true; do
    echo -e "HTTP/1.0 200 OK\r\nContent-Type: application/json\r\nConnection: close\r\n\r\n{\"qemu_test\":\"success\"}" | nc -lp 8080 2>/dev/null || sleep 0.1
done
