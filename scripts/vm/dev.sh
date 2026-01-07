#!/bin/bash
set -e
echo "Script Starting..."

# ==============================================================================
# Configuration
# ==============================================================================
# Load brand configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
eval "$("$SCRIPT_DIR/../build/brand.sh")"

APP_NAME="$BRAND_LOWER_NAME"
BUILD_DIR="$(pwd)/build"

# Artifacts from build_alpine.pl
ROOTFS="$BUILD_DIR/rootfs.qcow2"
KERNEL="$BUILD_DIR/vmlinuz"
INITRAMFS="$BUILD_DIR/initramfs"

# Network Configuration (Mac Addresses)
# 52:54:00 is QEMU default OUI.
MAC_WAN="52:54:00:11:00:01"
MAC_LAN1="52:54:00:22:00:01"
MAC_LAN2="52:54:00:22:00:02"
MAC_LAN3="52:54:00:22:00:03"

# API Port (can be overridden via environment variable)
# Set API_PORT=none to disable port forwarding (for test_mode runs)
API_PORT=${API_PORT:-8080}

# Socket base port for inter-VM networking (can be overridden)
# Each VM instance needs unique socket ports to avoid conflicts
SOCKET_BASE=${SOCKET_BASE:-1234}

# Find an available port
find_free_port() {
    local port=$1
    while lsof -n -i :$port >/dev/null 2>&1; do
        port=$((port + 1))
    done
    echo $port
}

# Find available socket port if default is in use
if lsof -n -i :${SOCKET_BASE} >/dev/null 2>&1; then
    SOCKET_BASE=$(find_free_port ${SOCKET_BASE})
fi

# Build WAN netdev options based on whether port forwarding is needed
if [ "$SEPARATE_MGMT_IFACE" = "1" ]; then
    # Split mode: WAN gets no hostfwd, MGMT gets hostfwd
    WAN_NETDEV="-netdev user,id=wan"

    # Random MAC for Mgmt if not set
    MAC_MGMT="52:54:00:22:00:99"
    MGMT_NETDEV_CMD="-netdev user,id=mgmt,net=10.0.4.0/24,dhcpstart=10.0.4.15,hostfwd=tcp::${API_PORT}-:8080,hostfwd=tcp::2222-:22 -device virtio-net-pci,netdev=mgmt,mac=$MAC_MGMT"

    echo "   * eth4 (MGMT) : $MAC_MGMT (HostFwd 8080->${API_PORT})"
elif [ "$API_PORT" = "none" ]; then
    WAN_NETDEV="-netdev user,id=wan"
    MGMT_NETDEV_CMD=""
else
    WAN_NETDEV="-netdev user,id=wan,hostfwd=tcp::${API_PORT}-:8080,hostfwd=tcp::2222-:22"
    MGMT_NETDEV_CMD=""
fi

# Use a temporary qcow2 overlay if the original rootfs is locked (allows parallel runs)
# This uses copy-on-write so it's fast and space-efficient
ROOTFS_OVERLAY=""

cleanup() {
    if [ -n "$QEMU_PID" ]; then
        kill $QEMU_PID 2>/dev/null || true
    fi
    if [ -n "$ROOTFS_OVERLAY" ] && [ -f "$ROOTFS_OVERLAY" ]; then
        rm -f "$ROOTFS_OVERLAY"
    fi
}
trap cleanup EXIT SIGINT SIGTERM

# ==============================================================================
# 1. Host Architecture Detection
# ==============================================================================
HOST_OS=$(uname -s)
HOST_ARCH=$(uname -m)

if [ "$HOST_OS" = "Darwin" ]; then
    # macOS
    ACCEL="hvf"
    MACHINE_TYPE="virt"

    if [ "$HOST_ARCH" = "arm64" ]; then
        # Apple Silicon
        QEMU_BIN="qemu-system-aarch64"
        CPU_TYPE="cortex-a72"
        CONSOLE_TTY="ttyAMA0"
    else
        # Intel Mac
        QEMU_BIN="qemu-system-x86_64"
        CPU_TYPE="host"
        CONSOLE_TTY="ttyS0"
        MACHINE_TYPE="q35"
    fi
elif [ "$HOST_OS" = "Linux" ]; then
    # Linux
    ACCEL="kvm"
    CPU_TYPE="host"

    if [ "$HOST_ARCH" = "aarch64" ]; then
        QEMU_BIN="qemu-system-aarch64"
        MACHINE_TYPE="virt"
        CONSOLE_TTY="ttyAMA0"
    else
        QEMU_BIN="qemu-system-x86_64"
        MACHINE_TYPE="q35"
        CONSOLE_TTY="ttyS0"
    fi
else
    echo "❌ Unsupported OS: $HOST_OS"
    exit 1
fi

# ==============================================================================
# 2. Asset Verification
# ==============================================================================
if [ ! -f "$ROOTFS" ] || [ ! -f "$KERNEL" ] || [ ! -f "$INITRAMFS" ]; then
    echo "❌ Missing build artifacts in ./build/"
    echo "   Please run: make vm-setup"
    exit 1
fi

# ==============================================================================
# 3. Launch VM
# ==============================================================================

# Check if quiet mode (suppress banner for test runs)
QUIET_MODE=0
TEST_MODE=0
if [[ "$3" == *"quiet"* ]] || [[ "$3" == *"test_mode"* ]]; then
    QUIET_MODE=1
    TEST_MODE=1
fi

# Build exit device options for test mode (allows guest to propagate exit code)
# Note: On ARM64, we rely on TAP output parsing since semihosting is unreliable
# On x86_64, isa-debug-exit allows direct exit code propagation
EXIT_DEVICE=""
KERNEL_ARGS="$3"

# Default to dev_mode=true if no kernel args provided (ensures glacic starts on make vm-start)
if [ -z "$KERNEL_ARGS" ]; then
    KERNEL_ARGS="dev_mode=true"
fi

if [ "$TEST_MODE" = "1" ]; then
    if [ "$HOST_ARCH" = "x86_64" ]; then
        # x86_64: use isa-debug-exit device
        # Exit code formula: (value_written << 1) | 1
        # So writing 0 gives exit 1, writing 1 gives exit 3, etc.
        EXIT_DEVICE="-device isa-debug-exit,iobase=0xf4,iosize=0x04"
    fi
    # ARM64: no special device, rely on TAP output parsing
fi

# Always use a qcow2 overlay - keeps base rootfs read-only and allows parallel runs
ROOTFS_OVERLAY="/tmp/rootfs-overlay-$$.qcow2"
if [[ $QUIET_MODE -eq 0 ]]; then
    echo "📋 Creating qcow2 overlay..."
fi
qemu-img create -f qcow2 -b "$ROOTFS" -F qcow2 "$ROOTFS_OVERLAY" >/dev/null 2>&1

if [[ $QUIET_MODE -eq 0 ]]; then
    echo "🚀 Launching $APP_NAME VM..."
    echo "------------------------------------------------------------"
    echo "   - Host:    $HOST_OS ($HOST_ARCH)"
    echo "   - Console: $CONSOLE_TTY (Mapped to Terminal)"
    echo "   - Kernel:  Direct Boot (Matches disk modules)"
    echo "   - Shared:  $(pwd) mounted at /mnt/$APP_NAME"
    echo "   - Network:"
    echo "     * eth0 (WAN)  : $MAC_WAN"
    echo "     * eth1 (LAN1) : $MAC_LAN1"
    echo "     * eth2 (LAN2) : $MAC_LAN2"
    echo "     * eth3 (LAN3) : $MAC_LAN3"
    echo "------------------------------------------------------------"
    echo "💡 Press Ctrl+A, then X to exit."
    echo "------------------------------------------------------------"
fi

$QEMU_BIN \
    -machine $MACHINE_TYPE,accel=$ACCEL \
    -cpu $CPU_TYPE \
    -smp 2 \
    -m 1G \
    -nographic \
    -no-reboot \
    $EXIT_DEVICE \
    \
    -kernel "$KERNEL" \
    -initrd "$INITRAMFS" \
    -append "root=/dev/vda rw console=$CONSOLE_TTY earlyprintk=serial rootwait modules=ext4 printk.time=1 console_msg_format=syslog rc_nocolor=YES $KERNEL_ARGS" \
    \
    -drive file="$ROOTFS_OVERLAY",format=qcow2,if=virtio,id=system_disk \
    \
    -virtfs local,path="${1:-$(pwd)}",mount_tag=host_share,security_model=none,id=host_share \
    \
    -device virtio-serial-pci \
    -chardev socket,path=${ORCHESTRATION_SOCKET:-/tmp/glacic-vm${VM_ID:-1}.sock},server=on,wait=off,id=channel0 \
    -device virtserialport,chardev=channel0,name=glacic.agent \
    $WAN_NETDEV -device virtio-net-pci,netdev=wan,mac=$MAC_WAN \
    -netdev socket,listen=:${SOCKET_BASE},id=lan1 -device virtio-net-pci,netdev=lan1,mac=$MAC_LAN1 \
    -netdev socket,connect=:${SOCKET_BASE},id=lan2 -device virtio-net-pci,netdev=lan2,mac=$MAC_LAN2 \
    -netdev socket,connect=:${SOCKET_BASE},id=lan3 -device virtio-net-pci,netdev=lan3,mac=$MAC_LAN3 \
    $MGMT_NETDEV_CMD &
QEMU_PID=$!
wait $QEMU_PID

# Capture QEMU exit code
QEMU_EXIT=$?

# For isa-debug-exit on x86_64, decode the exit code
# Exit code formula: (value_written << 1) | 1
# Guest writes 0 for success -> QEMU exits with 1
# Guest writes 1 for failure -> QEMU exits with 3
if [ "$TEST_MODE" = "1" ] && [ "$HOST_ARCH" != "arm64" ] && [ "$HOST_ARCH" != "aarch64" ]; then
    if [ "$QEMU_EXIT" = "1" ]; then
        # Success (guest wrote 0)
        exit 0
    elif [ "$QEMU_EXIT" = "3" ]; then
        # Failure (guest wrote 1)
        exit 1
    fi
fi

exit $QEMU_EXIT
