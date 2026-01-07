package vmm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type VM struct {
	Config     Config
	CID        uint32 // vsock Context ID (Linux only)
	SocketPath string // Unix socket path (macOS)
	cmd        *exec.Cmd
}

func NewVM(cfg Config, id int) (*VM, error) {
	// Verify artifacts
	if _, err := os.Stat(cfg.KernelPath); err != nil {
		return nil, fmt.Errorf("kernel not found at %s", cfg.KernelPath)
	}

	vm := &VM{Config: cfg}

	if runtime.GOOS == "linux" {
		// On Linux with KVM, use vsock with CID >= 3
		vm.CID = uint32(id + 2)
	} else {
		// On macOS, use Unix sockets
		vm.SocketPath = fmt.Sprintf("/tmp/glacic-vm%d.sock", id)
	}

	return vm, nil
}

func (v *VM) Start(ctx context.Context) error {
	// Architecture detection
	var qemuBin, machine, cpu, consoleTTY string
	switch runtime.GOARCH {
	case "arm64":
		qemuBin = "qemu-system-aarch64"
		machine = "virt,accel=hvf" // macOS ARM64
		cpu = "cortex-a72"
		consoleTTY = "ttyAMA0"
	default: // amd64
		qemuBin = "qemu-system-x86_64"
		if runtime.GOOS == "darwin" {
			machine = "q35,accel=hvf"
		} else {
			machine = "q35,accel=kvm"
		}
		cpu = "host"
		consoleTTY = "ttyS0"
	}

	// Unique overlay per VM
	var overlayID string
	if v.CID != 0 {
		overlayID = fmt.Sprintf("cid%d", v.CID)
	} else {
		overlayID = filepath.Base(v.SocketPath)
	}
	overlayPath := filepath.Join(os.TempDir(), fmt.Sprintf("glacic-overlay-%d-%s.qcow2", os.Getpid(), overlayID))

	imgBin := findBinary("qemu-img")
	createCmd := exec.Command(imgBin, "create", "-f", "qcow2", "-b", v.Config.RootfsPath, "-F", "qcow2", overlayPath)
	if out, err := createCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create overlay (bin: %s): %v (%s)", imgBin, err, out)
	}
	defer os.Remove(overlayPath)

	kernelArgs := fmt.Sprintf(
		"root=/dev/vda rw console=%s earlyprintk=serial rootwait modules=ext4 "+
			"printk.time=1 console_msg_format=syslog rc_nocolor=YES "+
			"agent_mode=true quiet loglevel=0",
		consoleTTY,
	)
	if v.Config.RunSkipped {
		kernelArgs += " glacic.run_skipped=1"
	}

	args := []string{
		"-machine", machine,
		"-cpu", cpu,
		"-smp", "1",
		"-m", "512M",
		"-nographic",
		"-no-reboot",

		"-kernel", v.Config.KernelPath,
		"-initrd", v.Config.InitrdPath,
		"-append", kernelArgs,

		"-drive", fmt.Sprintf("file=%s,format=qcow2,if=virtio,id=system_disk", overlayPath),

		// Host Share - Read-only for project root
		"-virtfs", fmt.Sprintf("local,path=%s,mount_tag=host_share,security_model=none,readonly=on,id=host_share", v.Config.ProjectRoot),
		// Build directory - Writable for test output
		"-virtfs", fmt.Sprintf("local,path=%s/build,mount_tag=build_share,security_model=none,id=build_share", v.Config.ProjectRoot),
	}

	// Add transport-specific devices
	if runtime.GOOS == "linux" && v.CID != 0 {
		// Linux: use vsock
		args = append(args,
			"-device", fmt.Sprintf("vhost-vsock-pci,guest-cid=%d", v.CID),
		)
	} else {
		// macOS: use virtio-serial with Unix socket
		_ = os.Remove(v.SocketPath)
		args = append(args,
			"-device", "virtio-serial-pci",
			"-chardev", fmt.Sprintf("socket,path=%s,server=on,wait=off,id=channel0", v.SocketPath),
			"-device", "virtserialport,chardev=channel0,name=glacic.agent",
		)
	}

	// Networking
	args = append(args,
		"-netdev", "user,id=wan",
		"-device", "virtio-net-pci,netdev=wan,mac=52:54:00:11:00:01",
		"-netdev", "user,id=lan1",
		"-device", "virtio-net-pci,netdev=lan1,mac=52:54:00:22:00:01",
		"-netdev", "user,id=lan2",
		"-device", "virtio-net-pci,netdev=lan2,mac=52:54:00:22:00:02",
		"-netdev", "user,id=lan3",
		"-device", "virtio-net-pci,netdev=lan3,mac=52:54:00:22:00:03",
	)

	qemuFullPath := findBinary(qemuBin)
	v.cmd = exec.CommandContext(ctx, qemuFullPath, args...)

	if v.Config.Debug {
		v.cmd.Stdout = os.Stdout
		v.cmd.Stderr = os.Stderr
	}

	return v.cmd.Run()
}

func (v *VM) Stop() error {
	if v.cmd != nil && v.cmd.Process != nil {
		return v.cmd.Process.Kill()
	}
	return nil
}

func findBinary(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}

	// Common locations on macOS/Linux if not in PATH
	extraPaths := []string{
		"/usr/local/bin/" + name,
		"/opt/homebrew/bin/" + name,
		"/usr/bin/" + name,
		"/bin/" + name,
	}

	for _, p := range extraPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return name // Fallback to original, which will eventually fail
}
