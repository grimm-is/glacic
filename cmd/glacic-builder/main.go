// glacic-builder - Alpine Linux Builder
// Builds Alpine Linux VM images and ISO installers
//
// Incorporates functionality from legacy build scripts

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"grimm.is/glacic/internal/brand"
)

// Configuration
const (
	AlpineRelease   = "v3.22"
	AlpineFullVer   = "3.22.2"
	DefaultHTTPPort = 8000
	BuildTimeout    = 10 * time.Minute
	DiskSizeMB      = 3072 // Increased for Go build cache
)

// VM mount path - derived from brand
var vmMountPath = "/mnt/" + brand.LowerName

// APK packages to install in the guest
// Minimal set based on integration test command analysis
var apkPackages = []string{
	// Core system (required for boot/init)
	"linux-virt", "openrc", "util-linux",

	// Networking stack
	"iproute2",    // ip command (427 uses in tests)
	"iproute2-ss", // ss command (netstat replacement)
	"iproute2-tc", // tc command (QoS)

	// Firewall & connection tracking
	"nftables",        // nft command (342 uses)
	"iptables",        // legacy compatibility
	"conntrack-tools", // conntrack for stateful firewall

	// Network utilities for tests
	"netcat-openbsd",       // nc (59 uses)
	"busybox-extras",       // udhcpc, ping, timeout
	"bind-tools",           // dig, nslookup (23 uses)
	"nmap", "nmap-scripts", // DHCP discovery (cannot be replaced by udhcpc)

	// General utilities
	"curl", // HTTP requests (56 uses)
	"jq",   // JSON parsing (39 uses)

	// Test infrastructure
	"perl", "perl-test-harness", // TAP harness (prove command)

	// Dev mode support (optional - can remove for production)
	"openssh", "openssh-server", "openssh-client",

	// Integration Test Dependencies
	"dnsmasq",         // for dns_test.sh
	"frr",             // for frr_test.sh (vtysh, zebra)
	"openssl",         // for tls_api.sh
	"sqlite",          // for dhcp_lease_lifecycle_test.sh
	"tcpdump",         // for ra.sh
	"wireguard-tools", // for vpn.sh
}

// Architecture configuration
type ArchConfig struct {
	Arch        string
	QEMUBin     string
	MachineArgs []string
	ConsoleTTY  string
}

func getArchConfig() (*ArchConfig, error) {
	hostArch := runtime.GOARCH
	hostOS := runtime.GOOS

	switch hostArch {
	case "arm64":
		cfg := &ArchConfig{
			Arch:       "aarch64",
			QEMUBin:    "qemu-system-aarch64",
			ConsoleTTY: "ttyAMA0",
		}
		if hostOS == "darwin" {
			cfg.MachineArgs = []string{"-machine", "virt", "-cpu", "cortex-a72", "-accel", "hvf"}
		} else {
			cfg.MachineArgs = []string{"-machine", "virt", "-cpu", "host", "-accel", "kvm"}
		}
		return cfg, nil

	case "amd64":
		return &ArchConfig{
			Arch:        "x86_64",
			QEMUBin:     "qemu-system-x86_64",
			MachineArgs: []string{"-machine", "q35", "-accel", "kvm"},
			ConsoleTTY:  "ttyS0",
		}, nil

	default:
		return nil, fmt.Errorf("unsupported architecture: %s", hostArch)
	}
}

// Builder manages the VM build process
type Builder struct {
	buildDir   string
	arch       *ArchConfig
	httpServer *http.Server
	serverDone chan struct{}
}

func NewBuilder(buildDir string) (*Builder, error) {
	arch, err := getArchConfig()
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create build directory: %w", err)
	}

	return &Builder{
		buildDir:   buildDir,
		arch:       arch,
		serverDone: make(chan struct{}),
	}, nil
}

// startHTTPServer starts the HTTP server for serving files during build
func (b *Builder) startHTTPServer(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", b.handleHTTPRequest)

	b.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		fmt.Printf("🌐 HTTP server starting on port %d...\n", port)
		if err := b.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			fmt.Printf("❌ HTTP server error: %v\n", err)
		}
		close(b.serverDone)
	}()

	// Wait for server to start
	time.Sleep(500 * time.Millisecond)
	return nil
}

func (b *Builder) stopHTTPServer() {
	if b.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		b.httpServer.Shutdown(ctx)
		<-b.serverDone
		fmt.Println("🌐 HTTP server stopped")
	}
}

func (b *Builder) handleHTTPRequest(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("🌐 %s %s from %s\n", r.Method, r.URL.Path, r.RemoteAddr)

	switch r.Method {
	case "GET":
		b.handleGet(w, r)
	case "PUT":
		b.handlePut(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		fmt.Fprintf(w, "Method %s not allowed\n", r.Method)
	}
}

func (b *Builder) handleGet(w http.ResponseWriter, r *http.Request) {
	filename := strings.TrimPrefix(r.URL.Path, "/")
	if filename == "" {
		files, _ := filepath.Glob(filepath.Join(b.buildDir, "*"))
		fmt.Fprintf(w, "Available files:\n")
		for _, file := range files {
			fmt.Fprintf(w, "- %s\n", filepath.Base(file))
		}
		return
	}

	filePath := filepath.Join(b.buildDir, filename)
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Printf("🌐 File not found: %s\n", filename)
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	stat, _ := file.Stat()
	fmt.Printf("🌐 Serving file: %s (%d bytes)\n", filename, stat.Size())

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	io.Copy(w, file)
	fmt.Printf("🌐 Sent %s successfully\n", filename)
}

func (b *Builder) handlePut(w http.ResponseWriter, r *http.Request) {
	filename := strings.TrimPrefix(r.URL.Path, "/")
	if filename == "" {
		http.Error(w, "Filename required", http.StatusBadRequest)
		return
	}

	fmt.Printf("🌐 Receiving file: %s\n", filename)

	filePath := filepath.Join(b.buildDir, filename)
	file, err := os.Create(filePath)
	if err != nil {
		fmt.Printf("🌐 Cannot create file: %s\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	bytesWritten, err := io.Copy(file, r.Body)
	if err != nil {
		fmt.Printf("🌐 Write error: %s\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Printf("🌐 Received %s (%d bytes)\n", filename, bytesWritten)
	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "File %s uploaded successfully\n", filename)
}

// downloadFile downloads a file if it doesn't exist
func (b *Builder) downloadFile(url, destName string) error {
	destPath := filepath.Join(b.buildDir, destName)

	if _, err := os.Stat(destPath); err == nil {
		fmt.Printf("✓ %s already exists\n", destName)
		return nil
	}

	fmt.Printf("⬇️  Downloading %s...\n", destName)

	cmd := exec.Command("curl", "-L", "-f", "-o", destPath, url)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to download %s: %w", url, err)
	}

	return nil
}

// generateProvisionScript generates the VM provisioning script
func (b *Builder) generateProvisionScript() string {
	packages := strings.Join(apkPackages, " ")

	script := `#!/bin/sh
set -e
echo "🔧 [Guest] Starting Network Install..."

# 1. Prepare Disks
echo "🔧 [Guest] Formatting disk..."
apk add --quiet --no-cache e2fsprogs
modprobe ext4
mkfs.ext4 -F -q /dev/vda
mkdir -p /mnt/target
mount -t ext4 /dev/vda /mnt/target

# 2. Install Alpine
echo "🔧 [Guest] Downloading RootFS..."
cd /mnt/target
wget -q -O rootfs.tar.gz http://dl-cdn.alpinelinux.org/alpine/` + AlpineRelease + `/releases/` + b.arch.Arch + `/alpine-minirootfs-` + AlpineFullVer + `-` + b.arch.Arch + `.tar.gz
tar -xzf rootfs.tar.gz
rm rootfs.tar.gz

# 3. Configure System
echo "🔧 [Guest] Configuring System..."
echo "nameserver 8.8.8.8" > etc/resolv.conf
echo "http://dl-cdn.alpinelinux.org/alpine/` + AlpineRelease + `/main" > etc/apk/repositories
echo "http://dl-cdn.alpinelinux.org/alpine/` + AlpineRelease + `/community" >> etc/apk/repositories

# Remove default Alpine MOTD
rm -f etc/motd

# Set hostname based on brand
echo "` + brand.LowerName + `" > etc/hostname
echo "127.0.0.1 ` + brand.LowerName + ` localhost" > etc/hosts

# Create directories that minirootfs doesn't have but packages expect
mkdir -p etc/security usr/lib/pam.d usr/lib/security/pam_filter

# Install Kernel & Tools via chroot (more reliable than --root)
chroot /mnt/target apk add --no-cache ` + packages + `
`
	// --- INITTAB CONFIGURATION ---
	// Replace default inittab with custom configuration
	// We run the dispatcher script directly on the console instead of a login shell
	inittab := fmt.Sprintf(`::sysinit:/sbin/openrc sysinit
::sysinit:/sbin/openrc boot
::wait:/sbin/openrc default

# Run dispatcher script on console
%s:2345:once:/root/entrypoint_dispatcher.sh

::shutdown:/sbin/openrc shutdown
::ctrlaltdel:/sbin/reboot
`, b.arch.ConsoleTTY)

	// We'll write the inittab in the provision script
	script += fmt.Sprintf("\ncat > etc/inittab <<'INITTAB'\n%sINITTAB\n", inittab)

	// Add console to securetty
	script += fmt.Sprintf("echo \"%s\" >> etc/securetty\n", b.arch.ConsoleTTY)

	// Network Configuration
	script += `cat > etc/network/interfaces <<'END'
auto lo
iface lo inet loopback
END
`
	// Runlevel setup
	script += `ln -s /etc/init.d/urandom etc/runlevels/boot/
ln -s /etc/init.d/localmount etc/runlevels/boot/
ln -s /etc/init.d/hostname etc/runlevels/boot/

echo "root:root" | chroot . chpasswd

# Shared Folder Setup
mkdir -p mnt/` + brand.LowerName + `
echo "host_share ` + vmMountPath + ` 9p trans=virtio,version=9p2000.L,rw 0 0" >> etc/fstab
echo "9p" >> etc/modules
echo "9pnet" >> etc/modules
echo "9pnet_virtio" >> etc/modules
`

	// --- ENTRYPOINT DISPATCHER ---
	dispatcher := `#!/bin/sh
# VM Entrypoint Dispatcher
# Determines which test/script to run based on kernel command line

# Helper function to exit VM with proper exit code propagation
qemu_exit() {
    local exit_code="${1:-0}"
    local arch=$(uname -m)

    if [ "$arch" = "aarch64" ] && [ -x ` + vmMountPath + `/build/qemu-exit-arm64 ]; then
        ` + vmMountPath + `/build/qemu-exit-arm64 "$exit_code"
        return
    elif [ "$arch" = "x86_64" ] && [ -x ` + vmMountPath + `/build/qemu-exit-amd64 ]; then
        ` + vmMountPath + `/build/qemu-exit-amd64 "$exit_code"
        return
    fi

    case "$arch" in
        x86_64|i686|i386)
            if [ "$exit_code" = "0" ]; then value=0; else value=1; fi
            if [ -c /dev/port ]; then
                printf "\\x$(printf '%02x' $value)" | dd of=/dev/port bs=1 seek=244 count=1 2>/dev/null
            else
                poweroff -f 2>/dev/null
            fi
            ;;
        *)
            poweroff -f 2>/dev/null
            ;;
    esac
}

# Mount shared folder if not already mounted (redundant check)
if ! mountpoint -q ` + vmMountPath + ` 2>/dev/null; then
    mount -t 9p -o trans=virtio,version=9p2000.L host_share ` + vmMountPath + ` 2>/dev/null || true
fi

# Check boot mode from kernel cmdline
if cat /proc/cmdline | grep -q "test_mode=true"; then
    if [ -f ` + vmMountPath + `/scripts/vm/entrypoint-test.sh ]; then
        sh ` + vmMountPath + `/scripts/vm/entrypoint-test.sh
        test_exit=$?
        qemu_exit $test_exit
    fi
elif cat /proc/cmdline | grep -q "agent_mode=true"; then
    echo "⚡ Starting in AGENT-ONLY mode..."
    # Run the agent binary directly (no ctl/api)
    if [ -x ` + vmMountPath + `/build/orca-agent ]; then
        exec ` + vmMountPath + `/build/orca-agent agent
    elif [ -x ` + vmMountPath + `/build/toolbox-linux ]; then
        exec ` + vmMountPath + `/build/toolbox-linux agent
    else
        echo "❌ Agent binary not found"
        qemu_exit 1
    fi
elif cat /proc/cmdline | grep -q "dev_mode=true"; then
    if [ -f ` + vmMountPath + `/scripts/vm/entrypoint-dev.sh ]; then
        echo "⚡ Starting in development mode..."
        sh ` + vmMountPath + `/scripts/vm/entrypoint-dev.sh
    fi
elif cat /proc/cmdline | grep -q "client_vm="; then
    if [ -f ` + vmMountPath + `/scripts/vm/entrypoint-client.sh ]; then
        echo "⚡ Found entrypoint-client.sh. Executing..."
        sh ` + vmMountPath + `/scripts/vm/entrypoint-client.sh
    fi
elif cat /proc/cmdline | grep -q "single_vm_test=true"; then
    if [ -f ` + vmMountPath + `/tests/single_vm_zone_test.sh ]; then
        echo "⚡ Running single VM zone test..."
        sh ` + vmMountPath + `/tests/single_vm_zone_test.sh
        test_exit=$?
        qemu_exit $test_exit
    fi
elif cat /proc/cmdline | grep -q "config="; then
    if [ -f ` + vmMountPath + `/scripts/vm/entrypoint-firewall.sh ]; then
        echo "⚡ Found entrypoint-firewall.sh. Executing..."
        sh ` + vmMountPath + `/scripts/vm/entrypoint-firewall.sh
    fi
else
    if [ -f ` + vmMountPath + `/scripts/vm/entrypoint.sh ]; then
        echo "⚡ Found entrypoint.sh. Executing..."
        sh ` + vmMountPath + `/scripts/vm/entrypoint.sh
    fi
fi
`
	script += fmt.Sprintf("\ncat > root/entrypoint_dispatcher.sh <<'DISPATCHER'\n%sDISPATCHER\n", dispatcher)
	script += "chmod +x root/entrypoint_dispatcher.sh\n"

	script += `
# 4. Upload Artifacts to Host
echo "📤 [Guest] Uploading new kernel to host..."
apk add --quiet --no-cache curl
curl -s -T /mnt/target/boot/vmlinuz-virt http://10.0.2.2:8000/vmlinuz
curl -s -T /mnt/target/boot/initramfs-virt http://10.0.2.2:8000/initramfs

# Cleanup and Clean Shutdown
cd /
sync
umount /mnt/target
sync
echo "✅ BUILD_COMPLETE"
poweroff
`
	return script
}

// createDiskImage creates a blank disk image
func (b *Builder) createDiskImage(path string, sizeMB int) error {
	if _, err := os.Stat(path); err == nil {
		return nil // Already exists
	}

	fmt.Printf("💿 Creating sparse disk image (%d MB)...\n", sizeMB)

	// Use qemu-img for sparse QCOW2 creation
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2", path, fmt.Sprintf("%dM", sizeMB))
	return cmd.Run()
}

// Build builds the Alpine VM image
func (b *Builder) Build() error {
	fmt.Println("╔════════════════════════════════════════╗")
	fmt.Println("║      " + brand.Name + " Builder (Go)               ║")
	fmt.Println("╚════════════════════════════════════════╝")
	fmt.Printf("Architecture: %s\n", b.arch.Arch)
	fmt.Printf("Build dir:    %s\n\n", b.buildDir)

	// Generate and write provision script
	script := b.generateProvisionScript()
	scriptPath := filepath.Join(b.buildDir, "setup.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return fmt.Errorf("failed to write setup.sh: %w", err)
	}
	fmt.Printf("✓ Generated setup.sh (%d bytes)\n", len(script))

	// Start HTTP server
	if err := b.startHTTPServer(DefaultHTTPPort); err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}
	defer b.stopHTTPServer()

	// Download Alpine netboot files
	baseURL := fmt.Sprintf("https://dl-cdn.alpinelinux.org/alpine/%s/releases/%s/netboot",
		AlpineRelease, b.arch.Arch)

	downloads := []struct{ url, name string }{
		{baseURL + "/vmlinuz-virt", "vmlinuz-virt-" + AlpineFullVer},
		{baseURL + "/initramfs-virt", "initramfs-virt-" + AlpineFullVer},
		{baseURL + "/modloop-virt", "modloop-virt-" + AlpineFullVer},
	}

	for _, dl := range downloads {
		if err := b.downloadFile(dl.url, dl.name); err != nil {
			return err
		}
	}

	// Create disk image
	diskPath := filepath.Join(b.buildDir, "rootfs.qcow2")
	if err := b.createDiskImage(diskPath, DiskSizeMB); err != nil {
		return fmt.Errorf("failed to create disk image: %w", err)
	}

	// Build QEMU command
	fmt.Println("\n🚀 Launching Builder VM...")

	kernelAppend := fmt.Sprintf("console=%s ip=dhcp modloop=http://10.0.2.2:%d/modloop-virt-%s alpine_repo=http://dl-cdn.alpinelinux.org/alpine/%s/main",
		b.arch.ConsoleTTY, DefaultHTTPPort, AlpineFullVer, AlpineRelease)

	args := append(b.arch.MachineArgs,
		"-m", "512",
		"-smp", "2",
		"-nographic",
		"-kernel", filepath.Join(b.buildDir, "vmlinuz-virt-"+AlpineFullVer),
		"-initrd", filepath.Join(b.buildDir, "initramfs-virt-"+AlpineFullVer),
		"-append", kernelAppend,
		"-drive", fmt.Sprintf("file=%s,format=qcow2,if=virtio", diskPath),
		"-netdev", "user,id=net0",
		"-device", "virtio-net-pci,netdev=net0",
	)

	cmd := exec.Command(b.arch.QEMUBin, args...)

	// Create pipes for VM interaction
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start QEMU: %w", err)
	}

	fmt.Printf("🔍 VM started with PID %d\n", cmd.Process.Pid)

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// VM interaction state machine
	var (
		loggedIn         = false
		provisionStarted = false
		buildComplete    = false
		buffer           strings.Builder
	)

	reader := bufio.NewReader(stdout)
	timeout := time.After(BuildTimeout)

	done := make(chan error, 1)
	go func() {
		for {
			char, err := reader.ReadByte()
			if err != nil {
				if err != io.EOF {
					done <- err
				}
				done <- nil
				return
			}

			fmt.Print(string(char))
			buffer.WriteByte(char)
			content := buffer.String()

			// State machine for VM interaction
			if !loggedIn && strings.Contains(content, "login:") {
				fmt.Println("\n🔍 Login prompt detected, sending 'root'")
				stdin.Write([]byte("root\n"))
				buffer.Reset()
				loggedIn = true
			}

			// Look for shell prompt: "hostname:path# " or just ":~#" pattern
			if loggedIn && !provisionStarted && (strings.Contains(content, ":~#") || strings.Contains(content, "# \n") || strings.HasSuffix(strings.TrimSpace(content), "~#")) {
				fmt.Println("\n🔍 Shell prompt detected, running provision script")
				time.Sleep(500 * time.Millisecond) // Let terminal settle
				stdin.Write([]byte(fmt.Sprintf("wget -O - http://10.0.2.2:%d/setup.sh | sh\n", DefaultHTTPPort)))
				provisionStarted = true
				buffer.Reset()
			}

			if strings.Contains(content, "BUILD_COMPLETE") {
				fmt.Println("\n🔍 Build completed successfully!")
				buildComplete = true
				done <- nil
				return
			}
		}
	}()

	// Wait for completion, timeout, or signal
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("VM interaction error: %w", err)
		}
	case <-timeout:
		cmd.Process.Kill()
		return fmt.Errorf("build timed out after %v", BuildTimeout)
	case sig := <-sigChan:
		cmd.Process.Kill()
		return fmt.Errorf("interrupted by signal: %v", sig)
	}

	stdin.Close()
	cmd.Wait()

	// Verify build artifacts
	vmlinuzPath := filepath.Join(b.buildDir, "vmlinuz")
	if _, err := os.Stat(vmlinuzPath); err != nil {
		return fmt.Errorf("build failed: vmlinuz not found in %s", b.buildDir)
	}

	if !buildComplete {
		return fmt.Errorf("build did not complete successfully")
	}

	fmt.Println("\n🎉 Build completed successfully!")
	fmt.Printf("   Kernel:    %s/vmlinuz\n", b.buildDir)
	fmt.Printf("   Initramfs: %s/initramfs\n", b.buildDir)
	fmt.Printf("   Disk:      %s/rootfs.qcow2\n", b.buildDir)

	return nil
}

// ServeOnly runs just the HTTP server (for debugging)
func (b *Builder) ServeOnly(port int) error {
	fmt.Printf("🌐 Starting HTTP server on port %d (serving %s)\n", port, b.buildDir)
	fmt.Println("Press Ctrl+C to stop")

	if err := b.startHTTPServer(port); err != nil {
		return err
	}

	// Wait for signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	b.stopHTTPServer()
	return nil
}

// generateISOBuilderScript generates the script that builds the ISO inside the VM
func (b *Builder) generateISOBuilderScript() string {
	return `#!/bin/sh
set -e
echo "🔧 [Guest] Starting ISO Build..."

# 1. Install Tools
echo "🔧 [Guest] Installing build tools..."
apk add --quiet --no-cache xorriso syslinux isolinux curl

# 2. Workspace
mkdir -p /tmp/iso/work
cd /tmp/iso

# 3. Download Alpine ISO
echo "🔧 [Guest] Downloading Alpine Standard ISO..."
ISO_URL="http://dl-cdn.alpinelinux.org/alpine/` + AlpineRelease + `/releases/` + b.arch.Arch + `/alpine-standard-` + AlpineFullVer + `-` + b.arch.Arch + `.iso"
curl -L -o alpine.iso "$ISO_URL"

# 4. Extract ISO
echo "🔧 [Guest] Extracting ISO..."
xorriso -osirrox on -indev alpine.iso -extract / /tmp/iso/work

# 5. Inject Payload
echo "🔧 [Guest] Injecting Firewall Payload..."
FW_DIR="/tmp/iso/work/firewall"
mkdir -p "$FW_DIR"

SRC="` + vmMountPath + `"

if [ ! -f "$SRC/build/` + brand.BinaryName + `-linux" ]; then
    echo "❌ Error: ` + brand.BinaryName + `-linux binary missing in $SRC/build"
    exit 1
fi

cp "$SRC/build/` + brand.BinaryName + `-linux" "$FW_DIR/"

if [ -d "$SRC/ui/dist" ]; then
    cp -r "$SRC/ui/dist" "$FW_DIR/ui"
else
    echo "⚠️ UI dist not found, skipping"
fi

if [ -f "$SRC/configs/basic.hcl" ]; then
    cp "$SRC/configs/basic.hcl" "$FW_DIR/config.hcl"
elif [ -f "$SRC/glacic.hcl" ]; then
    cp "$SRC/glacic.hcl" "$FW_DIR/config.hcl"
fi

if [ -f "$SRC/scripts/installer/install.sh" ]; then
    cp "$SRC/scripts/installer/install.sh" "$FW_DIR/"
    chmod +x "$FW_DIR/install.sh"
fi
if [ -f "$SRC/scripts/installer/firewall-ctl.init" ]; then
    cp "$SRC/scripts/installer/firewall-ctl.init" "$FW_DIR/"
fi
if [ -f "$SRC/scripts/installer/firewall-api.init" ]; then
    cp "$SRC/scripts/installer/firewall-api.init" "$FW_DIR/"
fi

# 6. Repack ISO
echo "🔧 [Guest] Repacking ISO..."
OUTPUT_NAME="` + brand.LowerName + `-installer-` + AlpineFullVer + `-` + b.arch.Arch + `.iso"
OUTPUT_PATH="` + vmMountPath + `/build/$OUTPUT_NAME"

cd /tmp/iso/work

EFI_IMG="boot/grub/efi.img"
if [ ! -f "$EFI_IMG" ]; then
    EFI_IMG=$(find . -name efi.img | head -n 1)
fi

echo "Using EFI image: $EFI_IMG"

xorriso -as mkisofs \
    -o "$OUTPUT_PATH" \
    -isohybrid-mbr /usr/share/syslinux/isohdpfx.bin \
    -c boot/syslinux/boot.cat \
    -b boot/syslinux/isolinux.bin \
    -no-emul-boot -boot-load-size 4 -boot-info-table \
    -eltorito-alt-boot \
    -e "$EFI_IMG" \
    -no-emul-boot -isohybrid-gpt-basdat \
    -volid "` + strings.ToUpper(brand.LowerName) + `_INSTALL" \
    .

echo "✅ ISO_BUILD_COMPLETE"
poweroff
`
}

// BuildISO builds a bootable installer ISO
func (b *Builder) BuildISO(projectRoot string) error {
	fmt.Println("╔════════════════════════════════════════╗")
	fmt.Println("║      " + brand.Name + " Installer ISO Builder      ║")
	fmt.Println("╚════════════════════════════════════════╝")
	fmt.Printf("Architecture: %s\n", b.arch.Arch)
	fmt.Printf("Project root: %s\n\n", projectRoot)

	// Check for firewall binary
	firewallBin := filepath.Join(projectRoot, "build", brand.BinaryName+"-linux")
	if _, err := os.Stat(firewallBin); err != nil {
		return fmt.Errorf("%s-linux binary not found at %s\nRun 'make build-linux' first", brand.BinaryName, firewallBin)
	}

	// Generate and write ISO builder script
	script := b.generateISOBuilderScript()
	scriptPath := filepath.Join(b.buildDir, "setup-iso.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return fmt.Errorf("failed to write setup-iso.sh: %w", err)
	}
	fmt.Printf("✓ Generated setup-iso.sh (%d bytes)\n", len(script))

	// Start HTTP server on port 8001 (different from vm build)
	isoHTTPPort := 8001
	if err := b.startHTTPServer(isoHTTPPort); err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}
	defer b.stopHTTPServer()

	// Download Alpine netboot files
	baseURL := fmt.Sprintf("https://dl-cdn.alpinelinux.org/alpine/%s/releases/%s/netboot",
		AlpineRelease, b.arch.Arch)

	downloads := []struct{ url, name string }{
		{baseURL + "/vmlinuz-virt", "vmlinuz-virt-" + AlpineFullVer},
		{baseURL + "/initramfs-virt", "initramfs-virt-" + AlpineFullVer},
		{baseURL + "/modloop-virt", "modloop-virt-" + AlpineFullVer},
	}

	for _, dl := range downloads {
		if err := b.downloadFile(dl.url, dl.name); err != nil {
			return err
		}
	}

	fmt.Println("\n🚀 Launching ISO Builder VM...")

	// Build QEMU command - RAM mode with 9p share
	kernelAppend := fmt.Sprintf("console=%s ip=dhcp modloop=http://10.0.2.2:%d/modloop-virt-%s alpine_repo=http://dl-cdn.alpinelinux.org/alpine/%s/main",
		b.arch.ConsoleTTY, isoHTTPPort, AlpineFullVer, AlpineRelease)

	args := append(b.arch.MachineArgs,
		"-m", "1024",
		"-smp", "2",
		"-nographic",
		"-kernel", filepath.Join(b.buildDir, "vmlinuz-virt-"+AlpineFullVer),
		"-initrd", filepath.Join(b.buildDir, "initramfs-virt-"+AlpineFullVer),
		"-append", kernelAppend,
		"-netdev", "user,id=net0",
		"-device", "virtio-net-pci,netdev=net0",
		// 9p share for project root
		"-fsdev", fmt.Sprintf("local,security_model=none,id=fsdev0,path=%s", projectRoot),
		"-device", "virtio-9p-pci,id=fs0,fsdev=fsdev0,mount_tag=host_share",
	)

	cmd := exec.Command(b.arch.QEMUBin, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start QEMU: %w", err)
	}

	fmt.Printf("🔍 VM started with PID %d\n", cmd.Process.Pid)

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	var (
		loggedIn         = false
		provisionStarted = false
		buildComplete    = false
		buffer           strings.Builder
	)

	reader := bufio.NewReader(stdout)
	timeout := time.After(10 * time.Minute) // ISO build takes longer

	done := make(chan error, 1)
	go func() {
		for {
			char, err := reader.ReadByte()
			if err != nil {
				if err != io.EOF {
					done <- err
				}
				done <- nil
				return
			}

			fmt.Print(string(char))
			buffer.WriteByte(char)
			content := buffer.String()

			if !loggedIn && strings.Contains(content, "login:") {
				fmt.Println("\n🔍 Login prompt detected, sending 'root'")
				stdin.Write([]byte("root\n"))
				buffer.Reset()
				loggedIn = true
			}

			if loggedIn && !provisionStarted && strings.Contains(content, "#") {
				fmt.Println("\n🔍 Shell ready, configuring ISO builder...")
				time.Sleep(time.Second)

				// Commands to run in guest
				cmds := []string{
					"echo 'nameserver 8.8.8.8' > /etc/resolv.conf",
					"mkdir -p " + vmMountPath,
					"modprobe 9p",
					"modprobe 9pnet",
					"modprobe 9pnet_virtio",
					"mount -t 9p -o trans=virtio,version=9p2000.L,rw host_share " + vmMountPath,
					fmt.Sprintf("wget -O /tmp/setup-iso.sh http://10.0.2.2:%d/setup-iso.sh", isoHTTPPort),
					"sh /tmp/setup-iso.sh",
				}

				for _, c := range cmds {
					stdin.Write([]byte(c + "\n"))
					time.Sleep(500 * time.Millisecond)
				}

				provisionStarted = true
				buffer.Reset()
			}

			if strings.Contains(content, "ISO_BUILD_COMPLETE") {
				fmt.Println("\n🔍 ISO build completed successfully!")
				buildComplete = true
				done <- nil
				return
			}
		}
	}()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("VM interaction error: %w", err)
		}
	case <-timeout:
		cmd.Process.Kill()
		return fmt.Errorf("ISO build timed out")
	case sig := <-sigChan:
		cmd.Process.Kill()
		return fmt.Errorf("interrupted by signal: %v", sig)
	}

	stdin.Close()
	cmd.Wait()

	if !buildComplete {
		return fmt.Errorf("ISO build did not complete successfully")
	}

	isoPath := filepath.Join(b.buildDir, fmt.Sprintf("%s-installer-%s-%s.iso", brand.LowerName, AlpineFullVer, b.arch.Arch))
	if _, err := os.Stat(isoPath); err != nil {
		return fmt.Errorf("ISO file not found at %s", isoPath)
	}

	stat, _ := os.Stat(isoPath)
	fmt.Printf("\n🎉 ISO build completed successfully!\n")
	fmt.Printf("   Output: %s\n", isoPath)
	fmt.Printf("   Size:   %.1f MB\n", float64(stat.Size())/(1024*1024))

	return nil
}

func printUsage() {
	fmt.Println("glacic-builder - Alpine Linux VM & ISO Builder")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  glacic-builder build              Build Alpine VM image for development")
	fmt.Println("  glacic-builder iso                Build bootable installer ISO")
	fmt.Println("  glacic-builder serve [port]       Run HTTP server only (default: 8000)")
	fmt.Println("  glacic-builder help               Show this help")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  build  - Creates VM image with kernel, initramfs, and rootfs")
	fmt.Println("  iso    - Creates bootable ISO with firewall pre-installed")
	fmt.Println("           Requires: make build-linux first")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Get project root (assume we're run from project root or cmd/glacic-builder)
	cwd, _ := os.Getwd()
	projectRoot := cwd
	buildDir := filepath.Join(cwd, "build")

	// If we're in cmd/glacic-builder, go up two levels
	if strings.HasSuffix(cwd, "cmd/glacic-builder") {
		projectRoot = filepath.Dir(filepath.Dir(cwd))
		buildDir = filepath.Join(projectRoot, "build")
	} else if strings.HasSuffix(cwd, "cmd/vmbuilder") { // Backwards compat just in case
		projectRoot = filepath.Dir(filepath.Dir(cwd))
		buildDir = filepath.Join(projectRoot, "build")
	}

	builder, err := NewBuilder(buildDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "build":
		if err := builder.Build(); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Build failed: %v\n", err)
			os.Exit(1)
		}

	case "iso":
		if err := builder.BuildISO(projectRoot); err != nil {
			fmt.Fprintf(os.Stderr, "❌ ISO build failed: %v\n", err)
			os.Exit(1)
		}

	case "serve":
		port := DefaultHTTPPort
		if len(os.Args) > 2 {
			fmt.Sscanf(os.Args[2], "%d", &port)
		}
		if err := builder.ServeOnly(port); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Server error: %v\n", err)
			os.Exit(1)
		}

	case "help", "-h", "--help":
		printUsage()

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}
