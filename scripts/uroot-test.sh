#!/bin/bash
# u-root based test runner for Go and shell integration tests
# Creates a minimal Linux initramfs with Go tools and runs tests in QEMU
#
# Features:
# - Cross-compiles for arm64/amd64 from any host
# - Supports Go test packages and shell test scripts
# - Includes busybox-like Go commands (shell, networking tools)
# - Embeds test binaries/scripts and runs them in VM
#
# Usage:
#   ./scripts/uroot-test.sh [package] [test-flags]     # Go tests
#   ./scripts/uroot-test.sh --shell [test-script]      # Shell tests
#   ./scripts/uroot-test.sh --shell                    # All shell tests
#
# Examples:
#   ./scripts/uroot-test.sh ./internal/firewall
#   ./scripts/uroot-test.sh ./internal/config -run TestHCL
#   ./scripts/uroot-test.sh --shell config_cli_test.sh
#   ./scripts/uroot-test.sh --shell                    # Run all *_test.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BUILD_DIR="$PROJECT_ROOT/build/uroot"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Host architecture detection
HOST_ARCH=$(uname -m)
case "$HOST_ARCH" in
    x86_64)
        GOARCH="amd64"
        QEMU_BIN="qemu-system-x86_64"
        QEMU_MACHINE="-machine q35"
        QEMU_CONSOLE="ttyS0"
        ;;
    arm64|aarch64)
        GOARCH="arm64"
        QEMU_BIN="qemu-system-aarch64"
        QEMU_MACHINE="-machine virt"
        QEMU_CONSOLE="ttyAMA0"
        ;;
    *)
        echo -e "${RED}Unsupported architecture: $HOST_ARCH${NC}"
        exit 1
        ;;
esac

# Check for hardware acceleration
if [[ "$(uname -s)" == "Darwin" ]]; then
    QEMU_ACCEL="-accel hvf"
elif [[ -e /dev/kvm ]]; then
    QEMU_ACCEL="-accel kvm"
else
    QEMU_ACCEL=""
    echo -e "${YELLOW}Warning: No hardware acceleration available${NC}"
fi

# Ensure dependencies
check_deps() {
    local missing=()

    if ! command -v u-root &>/dev/null; then
        missing+=("u-root (go install github.com/u-root/u-root@latest)")
    fi

    if ! command -v $QEMU_BIN &>/dev/null; then
        missing+=("$QEMU_BIN")
    fi

    if [[ ${#missing[@]} -gt 0 ]]; then
        echo -e "${RED}Missing dependencies:${NC}"
        for dep in "${missing[@]}"; do
            echo "  - $dep"
        done
        exit 1
    fi
}

# Get or download a kernel
ensure_kernel() {
    local kernel_file="$BUILD_DIR/kernel-$GOARCH"

    if [[ -f "$kernel_file" ]]; then
        echo "$kernel_file"
        return
    fi

    echo -e "${BLUE}Fetching kernel for $GOARCH...${NC}" >&2
    mkdir -p "$BUILD_DIR"

    # Use vmtest tool to get a kernel
    if command -v runvmtest &>/dev/null; then
        GOARCH=$GOARCH runvmtest -- bash -c "cp \$VMTEST_KERNEL $kernel_file" 2>/dev/null || true
    fi

    # Fallback: use our existing Alpine kernel if available
    if [[ ! -f "$kernel_file" ]]; then
        if [[ -f "$PROJECT_ROOT/build/vmlinuz" ]]; then
            echo -e "${YELLOW}Using existing Alpine kernel${NC}" >&2
            cp "$PROJECT_ROOT/build/vmlinuz" "$kernel_file"
        else
            echo -e "${RED}No kernel available. Please run: perl build_alpine.pl${NC}" >&2
            exit 1
        fi
    fi

    echo "$kernel_file"
}

# Build test binary
build_test_binary() {
    local package="$1"
    local test_binary="$BUILD_DIR/test.test"

    echo -e "${BLUE}Building test binary for $package...${NC}" >&2

    mkdir -p "$BUILD_DIR"

    # Build static test binary
    CGO_ENABLED=0 GOOS=linux GOARCH=$GOARCH \
        go test -c \
        -ldflags='-extldflags=-static' \
        -trimpath \
        -tags 'osusergo netgo static_build linux integration' \
        -o "$test_binary" \
        "$package" 2>&1 >&2

    echo "$test_binary"
}

# Build initramfs with u-root
build_initramfs() {
    local test_binary="$1"
    local test_flags="$2"
    local initramfs="$BUILD_DIR/initramfs.cpio"

    echo -e "${BLUE}Building initramfs with u-root...${NC}" >&2

    # Create a simple init script that runs tests
    local init_script="$BUILD_DIR/init.sh"
    cat > "$init_script" << SHEOF
#!/bin/gosh
echo "=== Starting test runner ==="
/test.test $test_flags
exitcode=\$?
if [ \$exitcode -eq 0 ]; then
    echo ""
    echo "=== TESTS PASSED ==="
else
    echo ""
    echo "=== TESTS FAILED (exit \$exitcode) ==="
fi
sync
shutdown -h now
SHEOF
    chmod +x "$init_script"

    # Build minimal initramfs with just our test binary as init
    # The test binary will run and then the kernel will panic (which is fine for testing)
    GOOS=linux GOARCH=$GOARCH u-root \
        -o "$initramfs" \
        -nocmd \
        -defaultsh="" \
        -initcmd="/test.test" \
        -files "$test_binary:test.test" \
        >&2

    echo "$initramfs"
}

# Run tests in QEMU
run_qemu() {
    local kernel="$1"
    local initramfs="$2"
    local test_flags="$3"

    echo -e "${BLUE}Running tests...${NC}"
    echo -e "${YELLOW}─────────────────────────────────────────${NC}"

    # Run QEMU with timeout, capture output silently
    local output_file=$(mktemp)
    trap "rm -f $output_file" RETURN

    # Use script to force pseudo-tty, redirect to /dev/null for silent run
    script -q "$output_file" timeout 60 $QEMU_BIN \
        $QEMU_MACHINE \
        $QEMU_ACCEL \
        -cpu max \
        -m 2G \
        -nographic \
        -no-reboot \
        -netdev user,id=net0 -device virtio-net-device,netdev=net0 \
        -netdev user,id=net1 -device virtio-net-device,netdev=net1 \
        -netdev user,id=net2 -device virtio-net-device,netdev=net2 \
        -kernel "$kernel" \
        -initrd "$initramfs" \
        -append "console=$QEMU_CONSOLE panic=-1 -- $test_flags" >/dev/null 2>&1

    local qemu_exit=$?

    # Filter output to show only test-relevant lines:
    # - Go test output (=== RUN, --- PASS/FAIL, etc.)
    # - Skip kernel boot messages (lines starting with [)
    # - Skip kernel panic details
    grep -E "^(=== RUN|=== PAUSE|=== CONT|--- PASS|--- FAIL|--- SKIP|PASS|FAIL|ok |FAIL\t|panic:)" "$output_file" | \
        grep -v "Kernel panic" | \
        grep -v "exitcode=" || true

    echo -e "${YELLOW}─────────────────────────────────────────${NC}"

    # Check for test results in output
    # Kernel panic with exitcode=0x00000000 means success
    if grep -q "exitcode=0x00000000" "$output_file"; then
        # Check for PASS/FAIL in test output
        if grep -q "^FAIL" "$output_file"; then
            echo -e "${RED}Tests FAILED${NC}"
            exit 1
        else
            echo -e "${GREEN}Tests PASSED${NC}"
            exit 0
        fi
    elif [[ $qemu_exit -eq 124 ]]; then
        echo -e "${RED}Tests timed out${NC}"
        exit 1
    else
        echo -e "${RED}Tests failed (QEMU exit: $qemu_exit)${NC}"
        exit 1
    fi
}

# Download static busybox for shell tests
ensure_busybox() {
    local busybox="$BUILD_DIR/busybox-$GOARCH"

    if [[ -f "$busybox" && $(stat -f%z "$busybox" 2>/dev/null || stat -c%s "$busybox" 2>/dev/null) -gt 100000 ]]; then
        echo "$busybox"
        return
    fi

    echo -e "${BLUE}Downloading static busybox for $GOARCH...${NC}" >&2

    local url
    case "$GOARCH" in
        amd64)
            url="https://busybox.net/downloads/binaries/1.35.0-x86_64-linux-musl/busybox"
            ;;
        arm64)
            # Use raw GitHub URL for aarch64 binary
            url="https://raw.githubusercontent.com/shutingrz/busybox-static-binaries-fat/master/busybox-aarch64-linux-gnu"
            ;;
    esac

    curl -sL "$url" -o "$busybox" >&2
    chmod +x "$busybox"

    # Verify download
    local size=$(stat -f%z "$busybox" 2>/dev/null || stat -c%s "$busybox" 2>/dev/null)
    if [[ $size -lt 100000 ]]; then
        echo -e "${RED}Failed to download busybox (size: $size bytes)${NC}" >&2
        rm -f "$busybox"
        exit 1
    fi

    echo "$busybox"
}

# Build initramfs for shell tests (includes busybox, firewall binary, and test scripts)
build_shell_initramfs() {
    local test_scripts="$1"
    local initramfs="$BUILD_DIR/initramfs-shell.cpio"
    local firewall_binary="$BUILD_DIR/glacic"

    echo -e "${BLUE}Building firewall binary...${NC}" >&2

    # Build the firewall binary for Linux
    CGO_ENABLED=0 GOOS=linux GOARCH=$GOARCH \
        go build \
        -ldflags='-extldflags=-static' \
        -trimpath \
        -tags 'osusergo netgo static_build linux' \
        -o "$firewall_binary" \
        . 2>&1 >&2

    local busybox=$(ensure_busybox)

    echo -e "${BLUE}Building shell test initramfs...${NC}" >&2

    # Create init script that sets up busybox and runs tests
    local init_script="$BUILD_DIR/init"
    cat > "$init_script" << 'INITSCRIPT'
#!/bin/busybox sh
# Mount essential filesystems
/bin/busybox mount -t proc proc /proc
/bin/busybox mount -t sysfs sys /sys
/bin/busybox mount -t devtmpfs dev /dev
/bin/busybox mkdir -p /tmp
/bin/busybox mount -t tmpfs tmp /tmp

# Setup busybox symlinks
/bin/busybox --install -s /bin

# Setup environment
export PATH="/bin:/firewall"
export HOME="/tmp"
cd /tmp

echo "=== Shell Integration Tests ==="
echo ""

failed=0
passed=0

# Run each test script
for test in /tests/*_test.sh; do
    if [ -f "$test" ]; then
        name=$(/bin/busybox basename "$test")
        echo "--- Running: $name ---"
        if /bin/sh "$test"; then
            echo "--- PASS: $name ---"
            passed=$((passed + 1))
        else
            echo "--- FAIL: $name ---"
            failed=$((failed + 1))
        fi
        echo ""
    fi
done

echo "=== Results: $passed passed, $failed failed ==="

if [ $failed -eq 0 ]; then
    echo "PASS"
else
    echo "FAIL"
fi

# Sync and halt
/bin/busybox sync
exec /bin/busybox poweroff -f
INITSCRIPT
    chmod +x "$init_script"

    # Create initramfs structure
    local initramfs_dir="$BUILD_DIR/initramfs-root"
    rm -rf "$initramfs_dir"
    mkdir -p "$initramfs_dir"/{bin,dev,proc,sys,tmp,tests,firewall}

    # Copy files
    cp "$busybox" "$initramfs_dir/bin/busybox"
    cp "$init_script" "$initramfs_dir/init"
    cp "$firewall_binary" "$initramfs_dir/firewall/glacic"

    # Copy test scripts
    for script in $test_scripts; do
        cp "$script" "$initramfs_dir/tests/"
    done

    # Create cpio archive
    (cd "$initramfs_dir" && find . | cpio -o -H newc 2>/dev/null) > "$initramfs"

    rm -rf "$initramfs_dir"

    echo "$initramfs"
}

# Run shell tests in QEMU
run_shell_qemu() {
    local kernel="$1"
    local initramfs="$2"

    echo -e "${BLUE}Running shell tests...${NC}"
    echo -e "${YELLOW}─────────────────────────────────────────${NC}"

    local output_file=$(mktemp)
    trap "rm -f $output_file" RETURN

    # Use script to force pseudo-tty, redirect to /dev/null for silent run
    script -q "$output_file" timeout 120 $QEMU_BIN \
        $QEMU_MACHINE \
        $QEMU_ACCEL \
        -cpu max \
        -m 2G \
        -nographic \
        -no-reboot \
        -netdev user,id=net0 -device virtio-net-device,netdev=net0 \
        -netdev user,id=net1 -device virtio-net-device,netdev=net1 \
        -netdev user,id=net2 -device virtio-net-device,netdev=net2 \
        -kernel "$kernel" \
        -initrd "$initramfs" \
        -append "console=$QEMU_CONSOLE panic=-1" >/dev/null 2>&1

    local qemu_exit=$?

    # Filter output to show test-relevant lines
    grep -E "^(===|---|ok |not ok |1\.\.|PASS|FAIL|#)" "$output_file" | \
        grep -v "Kernel panic" | \
        grep -v "exitcode=" || true

    echo -e "${YELLOW}─────────────────────────────────────────${NC}"

    # Check for test results based on output content
    # Strip carriage returns and look for our test runner's final PASS/FAIL output
    local clean_output=$(tr -d '\r' < "$output_file")
    if echo "$clean_output" | grep -q "^FAIL$" || echo "$clean_output" | grep -q "not ok"; then
        echo -e "${RED}Shell Tests FAILED${NC}"
        exit 1
    elif echo "$clean_output" | grep -q "^PASS$"; then
        echo -e "${GREEN}Shell Tests PASSED${NC}"
        exit 0
    elif [[ $qemu_exit -eq 124 ]]; then
        echo -e "${RED}Shell tests timed out${NC}"
        exit 1
    else
        echo -e "${RED}Shell tests failed (no result found, QEMU exit: $qemu_exit)${NC}"
        exit 1
    fi
}

# Main for Go tests
main_go() {
    local package="${1:-./internal/firewall}"
    shift || true
    local test_flags="${@:--test.v}"

    cd "$PROJECT_ROOT"

    echo -e "${BLUE}╔════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║      u-root Integration Test Runner    ║${NC}"
    echo -e "${BLUE}╚════════════════════════════════════════╝${NC}"
    echo ""
    echo "Mode:    Go Tests"
    echo "Package: $package"
    echo "Arch:    $GOARCH"
    echo "Flags:   $test_flags"
    echo ""

    check_deps

    local kernel=$(ensure_kernel)
    echo -e "${GREEN}Kernel: $kernel${NC}"

    local test_binary=$(build_test_binary "$package")
    echo -e "${GREEN}Binary: $test_binary${NC}"

    local initramfs=$(build_initramfs "$test_binary" "$test_flags")
    echo -e "${GREEN}Initramfs: $initramfs${NC}"
    echo ""

    run_qemu "$kernel" "$initramfs" "$test_flags"
}

# Main for shell tests
main_shell() {
    local test_filter="$1"

    cd "$PROJECT_ROOT"

    # Find test scripts
    local test_scripts=""
    if [[ -n "$test_filter" && "$test_filter" != "--shell" ]]; then
        # Specific test script
        if [[ -f "scripts/test/$test_filter" ]]; then
            test_scripts="$PROJECT_ROOT/scripts/test/$test_filter"
        elif [[ -f "$test_filter" ]]; then
            test_scripts="$test_filter"
        else
            echo -e "${RED}Test script not found: $test_filter${NC}"
            exit 1
        fi
    else
        # All test scripts
        test_scripts=$(find "$PROJECT_ROOT/scripts/test" -name "*_test.sh" -type f | sort)
    fi

    local script_count=$(echo "$test_scripts" | wc -l | tr -d ' ')

    echo -e "${BLUE}╔════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║    u-root Shell Integration Tests      ║${NC}"
    echo -e "${BLUE}╚════════════════════════════════════════╝${NC}"
    echo ""
    echo "Mode:    Shell Tests"
    echo "Scripts: $script_count"
    echo "Arch:    $GOARCH"
    echo ""

    check_deps

    local kernel=$(ensure_kernel)
    echo -e "${GREEN}Kernel: $kernel${NC}"

    local initramfs=$(build_shell_initramfs "$test_scripts")
    echo -e "${GREEN}Initramfs: $initramfs${NC}"
    echo ""

    run_shell_qemu "$kernel" "$initramfs"
}

# Entry point
main() {
    if [[ "$1" == "--shell" ]]; then
        shift
        main_shell "$1"
    elif [[ "$1" == "--help" || "$1" == "-h" ]]; then
        echo "Usage:"
        echo "  $0 [package] [test-flags]     Run Go tests"
        echo "  $0 --shell [script]           Run shell tests"
        echo ""
        echo "Examples:"
        echo "  $0 ./internal/firewall        Run firewall Go tests"
        echo "  $0 --shell                    Run all shell tests"
        echo "  $0 --shell config_cli_test.sh Run specific shell test"
        exit 0
    else
        main_go "$@"
    fi
}

main "$@"
