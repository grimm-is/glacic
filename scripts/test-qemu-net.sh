#!/bin/bash
# Minimal QEMU networking test - no firewall, just HTTP server
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BUILD_DIR="$SCRIPT_DIR/build"
VM_DIR="$BUILD_DIR/vm"
ROOTFS="$VM_DIR/rootfs-alpine.qcow2"
KERNEL="$VM_DIR/kernel"

if [ ! -f "$ROOTFS" ] || [ ! -f "$KERNEL" ]; then
    echo "ERROR: VM files not found. Run 'make vm-setup' first"
    exit 1
fi

# Create minimal init that just starts HTTP server
INIT_SCRIPT=$(mktemp)
cat > "$INIT_SCRIPT" << 'INITEOF'
#!/bin/sh
mount -t proc none /proc
mount -t sysfs none /sys
mount -t devtmpfs none /dev

# Configure network
ip link set lo up
ip link set eth0 up
udhcpc -i eth0 -q

echo "=== QEMU NETWORKING TEST ==="
echo "IP: $(ip -4 addr show eth0 | grep inet | awk '{print $2}')"

# Create test file
mkdir -p /www
echo '{"qemu":"works"}' > /www/test.json

echo "Starting nc HTTP server on port 8080..."
while true; do
    echo -e "HTTP/1.0 200 OK\r\nContent-Type: application/json\r\nContent-Length: 17\r\nConnection: close\r\n\r\n{\"qemu\":\"works\"}" | nc -lp 8080 2>/dev/null
done
INITEOF

echo "=== Starting Minimal QEMU Test ==="
echo "Press Ctrl+A, X to exit"
echo "Test: curl http://localhost:8080/test.json"
echo ""

# Run QEMU with minimal config
qemu-system-aarch64 \
    -M virt \
    -cpu cortex-a57 \
    -m 256 \
    -nographic \
    -kernel "$KERNEL" \
    -drive file="$ROOTFS",format=qcow2,if=virtio,snapshot=on \
    -append "console=ttyAMA0 root=/dev/vda quiet init=/init.sh printk.time=1 console_msg_format=syslog rc_nocolor=YES" \
    -netdev user,id=net0,hostfwd=tcp::8080-:8080 \
    -device virtio-net-pci,netdev=net0 \
    -fsdev local,id=share,path=/tmp,security_model=mapped-xattr \
    -device virtio-9p-pci,fsdev=share,mount_tag=share
