#!/bin/bash
set -e

# ==============================================================================
# Zone-based VM Launcher for Firewall Testing
# ==============================================================================
APP_NAME="firewall-zones"
BUILD_DIR="$(pwd)/build"

# Artifacts from build_alpine.pl
ROOTFS="$BUILD_DIR/rootfs.ext4"
KERNEL="$BUILD_DIR/vmlinuz"
INITRAMFS="$BUILD_DIR/initramfs"

# Zone MAC Addresses
MAC_WAN="52:54:00:11:00:01"
MAC_GREEN="52:54:00:22:00:01"
MAC_ORANGE="52:54:00:33:00:01"
MAC_RED="52:54:00:44:00:01"

# ==============================================================================
# 1. Host Architecture Detection
# ==============================================================================
HOST_OS=$(uname -s)
HOST_ARCH=$(uname -m)

if [ "$HOST_OS" = "Darwin" ]; then
    ACCEL="hvf"
    MACHINE_TYPE="virt"

    if [ "$HOST_ARCH" = "arm64" ]; then
        QEMU_BIN="qemu-system-aarch64"
        CPU_TYPE="cortex-a72"
        CONSOLE_TTY="ttyAMA0"
    else
        QEMU_BIN="qemu-system-x86_64"
        CPU_TYPE="host"
        CONSOLE_TTY="ttyS0"
        MACHINE_TYPE="q35"
    fi
elif [ "$HOST_OS" = "Linux" ]; then
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
    echo "   Please run: perl build_alpine.pl"
    exit 1
fi

# ==============================================================================
# 3. VM Type Configuration
# ==============================================================================
VM_TYPE=${1:-"firewall"}
ZONE_NAME=${2:-"green"}
INTERFACE=${3:-"eth1"}

launch_firewall_vm() {
    local config_file=${2:-"./zones.hcl"}

    echo "🚀 Launching Firewall VM..."
    echo "------------------------------------------------------------"
    echo "   - Host:    $HOST_OS ($HOST_ARCH)"
    echo "   - Console: $CONSOLE_TTY"
    echo "   - Config:  $config_file"
    echo "   - Zones:   Green (eth1), Orange (eth2), Red (eth3)"
    echo "   - Network: Socket-based zone isolation"
    echo "------------------------------------------------------------"

    $QEMU_BIN \
        -machine $MACHINE_TYPE,accel=$ACCEL \
        -cpu $CPU_TYPE \
        -smp 2 \
        -m 1G \
        -nographic \
        \
        -kernel "$KERNEL" \
        -initrd "$INITRAMFS" \
        -append "root=/dev/vda rw console=$CONSOLE_TTY earlyprintk=serial rootwait modules=ext4 config=$config_file" \
        \
        -drive file="$ROOTFS",format=raw,if=virtio,id=system_disk \
        \
        -virtfs local,path="$(pwd)",mount_tag=host_share,security_model=none,id=host_share \
        \
        -netdev user,id=wan,hostfwd=tcp::8080-:8080 -device virtio-net-pci,netdev=wan,mac=$MAC_WAN \
        -netdev socket,listen=:1234,id=green -device virtio-net-pci,netdev=green,mac=$MAC_GREEN \
        -netdev socket,listen=:1235,id=orange -device virtio-net-pci,netdev=orange,mac=$MAC_ORANGE \
        -netdev socket,listen=:1236,id=red -device virtio-net-pci,netdev=red,mac=$MAC_RED
}

launch_client_vm() {
    local zone_name=$1
    local interface=$2
    local socket_port=$3
    local client_mac=$4

    echo "🚀 Launching $zone_name Client VM..."
    echo "------------------------------------------------------------"
    echo "   - Zone:     $zone_name"
    echo "   - Interface: $interface"
    echo "   - Socket:   localhost:$socket_port"
    echo "------------------------------------------------------------"

    $QEMU_BIN \
        -machine $MACHINE_TYPE,accel=$ACCEL \
        -cpu $CPU_TYPE \
        -smp 1 \
        -m 512M \
        -nographic \
        \
        -kernel "$KERNEL" \
        -initrd "$INITRAMFS" \
        -append "root=/dev/vda rw console=$CONSOLE_TTY earlyprintk=serial rootwait modules=ext4 client_vm=$zone_name" \
        \
        -drive file="$ROOTFS",format=raw,if=virtio,id=system_disk \
        \
        -virtfs local,path="$(pwd)",mount_tag=host_share,security_model=none,id=host_share \
        \
        -netdev socket,connect=:$socket_port,id=zone -device virtio-net-pci,netdev=zone,mac=$client_mac
}

# ==============================================================================
# 4. Launch Logic
# ==============================================================================
case $VM_TYPE in
    "firewall")
        launch_firewall_vm
        ;;
    "client")
        case $ZONE_NAME in
            "Green"|"green")
                launch_client_vm "Green" "eth1" "1234" "52:54:00:22:00:02"
                ;;
            "Orange"|"orange")
                launch_client_vm "Orange" "eth2" "1235" "52:54:00:33:00:02"
                ;;
            "Red"|"red")
                launch_client_vm "Red" "eth3" "1236" "52:54:00:44:00:02"
                ;;
            *)
                echo "❌ Unknown zone: $ZONE_NAME"
                echo "   Valid zones: Green, Orange, Red"
                exit 1
                ;;
        esac
        ;;
    *)
        echo "❌ Unknown VM type: $VM_TYPE"
        echo "   Usage: $0 <firewall|client> [zone_name] [interface]"
        exit 1
        ;;
esac
