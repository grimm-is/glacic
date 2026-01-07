#!/bin/sh
set -e

# Glacic Firewall Installer
# Usage: ./install.sh [target_disk]

echo "🔥 Glacic Firewall Installer"
echo "============================="

# Detect Source
SOURCE_DIR=$(dirname $(realpath $0))
# If run from root, check typical mount points
if [ "$SOURCE_DIR" = "." ] || [ "$SOURCE_DIR" = "/" ]; then
    if [ -d "/media/cdrom/firewall" ]; then
        SOURCE_DIR="/media/cdrom/firewall"
    elif [ -d "/media/usb/firewall" ]; then
        SOURCE_DIR="/media/usb/firewall"
    fi
fi

if [ ! -f "$SOURCE_DIR/glacic" ]; then
    echo "❌ Error: Glacic binaries not found in $SOURCE_DIR"
    exit 1
fi

# Detect Target Disk
DISK=$1
if [ -z "$DISK" ]; then
    echo "Available disks:"
    lsblk -d -n -o NAME,SIZE,MODEL,TYPE | grep -E 'disk|nvme'
    echo
    read -p "Enter target disk (e.g. sda, nvme0n1): " DISK
fi

DEV="/dev/$DISK"
if [ ! -b "$DEV" ]; then
    echo "❌ Error: Device $DEV not found."
    exit 1
fi

echo "WARNING: All data on $DEV will be erased!"
read -p "Continue? (y/N): " CONFIRM
if [ "$CONFIRM" != "y" ]; then
    echo "Aborted."
    exit 1
fi

echo "🔧 Setup environment..."
# Ensure required tools
apk add --quiet --no-cache e2fsprogs parted alpine-conf

echo "🔧 Installing Alpine Linux to $DEV..."
# Use setup-disk in sys mode
# BOOT_SIZE=100 (default), SWAP_SIZE=0 (we disable swap for firewall usually, or keep small)
export MBOOT_INSTALL_PARTITION_SIZE=512
export SWAP_SIZE=0

# Run setup-disk
# -m sys: System mode (bootable)
# -s 0: No swap
# -k lts: Use LTS kernel (broadest hardware support)
setup-disk -m sys -s 0 -k lts "$DEV"

echo "🔧 Mounting new system..."
MOUNTPOINT="/mnt/target"
mkdir -p "$MOUNTPOINT"

# Find root partition (usually p3 for EFI, or p2/p3 for BIOS)
# setup-disk uses UUIDs usually, but we need the device path.
# Let's try to find the ext4 partition on the device.
ROOT_PART=$(blkid | grep "$DEV" | grep 'TYPE="ext4"' | cut -d: -f1 | head -n1)

if [ -z "$ROOT_PART" ]; then
    # Fallback guessing
    if [ -b "${DEV}p3" ]; then ROOT_PART="${DEV}p3"; fi
    if [ -b "${DEV}3" ]; then ROOT_PART="${DEV}3"; fi
    if [ -b "${DEV}p2" ]; then ROOT_PART="${DEV}p2"; fi
    if [ -b "${DEV}2" ]; then ROOT_PART="${DEV}2"; fi
fi

if [ -z "$ROOT_PART" ]; then
    echo "❌ Error: Could not detect root partition on $DEV"
    exit 1
fi

mount "$ROOT_PART" "$MOUNTPOINT"

echo "🔧 Installing Glacic binaries..."
mkdir -p "$MOUNTPOINT/usr/local/bin"
cp "$SOURCE_DIR/glacic" "$MOUNTPOINT/usr/local/bin/glacic"
chmod +x "$MOUNTPOINT/usr/local/bin/glacic"

echo "🔧 Installing UI..."
mkdir -p "$MOUNTPOINT/var/lib/glacic/ui"
if [ -d "$SOURCE_DIR/ui" ]; then
    cp -r "$SOURCE_DIR/ui"/* "$MOUNTPOINT/var/lib/glacic/ui/"
else
    echo "⚠️ UI directory not found in source, skipping."
fi

echo "🔧 Installing Configuration..."
mkdir -p "$MOUNTPOINT/etc/glacic"
if [ -f "$SOURCE_DIR/config.hcl" ]; then
    cp "$SOURCE_DIR/config.hcl" "$MOUNTPOINT/etc/glacic/glacic.hcl"
else
    # Create default
    echo 'ip_forwarding = true' > "$MOUNTPOINT/etc/glacic/glacic.hcl"
fi

echo "🔧 Installing Services..."
mkdir -p "$MOUNTPOINT/etc/init.d"
cp "$SOURCE_DIR/glacic-ctl.init" "$MOUNTPOINT/etc/init.d/glacic-ctl"
cp "$SOURCE_DIR/glacic-api.init" "$MOUNTPOINT/etc/init.d/glacic-api"
chmod +x "$MOUNTPOINT/etc/init.d/glacic-ctl"
chmod +x "$MOUNTPOINT/etc/init.d/glacic-api"

echo "🔧 Configuring System..."
# Add firewall services to default runlevel
# We must chroot
chroot "$MOUNTPOINT" rc-update add glacic-ctl default
chroot "$MOUNTPOINT" rc-update add glacic-api default
chroot "$MOUNTPOINT" rc-update add networking default # Ensure basic net is up (lo)

# Enable IP forwarding persistently
echo "net.ipv4.ip_forward=1" >> "$MOUNTPOINT/etc/sysctl.conf"
echo "net.ipv6.conf.all.forwarding=1" >> "$MOUNTPOINT/etc/sysctl.conf"

# Cleanup
umount -R "$MOUNTPOINT"

echo "✅ Installation Complete!"
echo "Please reboot your system."
