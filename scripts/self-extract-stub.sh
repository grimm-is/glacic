#!/bin/bash
set -e

# Self-extracting installer for Glacic
# Wraps the tarball payload attached at the end of this script

echo "Glacic Self-Extracting Installer"
echo "================================"

# Create temp directory
TMP_DIR=$(mktemp -d)
cleanup() {
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

# Find the start line of the binary payload
PAYLOAD_START=$(awk '/^__ARCHIVE_BELOW__/ {print NR + 1; exit 0; }' "$0")

if [ -z "$PAYLOAD_START" ]; then
    echo "Error: Payload marker not found."
    exit 1
fi

echo "Extracting payload to $TMP_DIR..."
tail -n +$PAYLOAD_START "$0" | tar xz -C "$TMP_DIR"

if [ ! -f "$TMP_DIR/install.sh" ]; then
    echo "Error: install.sh not found in payload."
    exit 1
fi

echo "Running installer..."
echo ""
cd "$TMP_DIR"
./install.sh "$@"

exit 0

__ARCHIVE_BELOW__
