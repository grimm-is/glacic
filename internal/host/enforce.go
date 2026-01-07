package host

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"strings"
	"time"

	"github.com/vishvananda/netlink"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
)

// CheckPortConflicts scans for processes holding ports that Glacic needs.
// It returns a list of warning messages.
func CheckPortConflicts(cfg *config.Config) ([]string, error) {
	requiredPorts := getRequiredPorts(cfg)
	var warnings []string

	// On Linux, we can scan /proc to find conflicts
	if runtimeOS() != "linux" {
		return nil, nil // Skip on non-Linux (e.g. macOS dev)
	}

	// Maps protocol:port -> owner process info
	portOwners, err := scanOpenPorts()
	if err != nil {
		return nil, fmt.Errorf("failed to scan open ports: %v", err)
	}

	for _, req := range requiredPorts {
		key := fmt.Sprintf("%s:%d", req.Proto, req.Port)
		if owner, exists := portOwners[key]; exists {
			// Ignore if it's us (though checking our own PID is tricky if we just started,
			// assuming we haven't bound yet. If we are asking this check, we assume we haven't started services).
			// But maybe we are restarting?
			if owner.PID == os.Getpid() {
				continue
			}

			msg := fmt.Sprintf("Port %d/%s is in use by '%s' (PID %d). Service '%s' may fail to start.",
				req.Port, req.Proto, owner.CmdLine, owner.PID, req.Service)
			warnings = append(warnings, msg)
		}
	}

	return warnings, nil
}

// EnforceLoopback ensures the loopback interface is UP and has standard assignments.
func EnforceLoopback(cfg *config.Config) error {
	// 1. Ensure 'lo' exists
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("loopback interface 'lo' not found: %v", err)
	}

	// 2. Ensure UP
	if lo.Attrs().Flags&net.FlagUp == 0 {
		logging.Warn("[HOST] Loopback interface is DOWN. Bringing it UP.")
		if err := netlink.LinkSetUp(lo); err != nil {
			return fmt.Errorf("failed to bring loopback up: %v", err)
		}
	}

	// 3. Ensure Standard IPs (127.0.0.1/8, ::1/128)
	// We check config if they defined custom loopback, otherwise default.
	// Actually, system sanity requires 127.0.0.1 regardless of config usually.
	// But let's respect config if "lo" is defined?
	// The standard behavior for Glacic is to manage what's in HCL.
	// However, the user explicitly asked to "Ensure 127.0.0.1/8 and ::1/128 are assigned".

	// Let's blindly verify 127.0.0.1/8 first as a minimal requirement.
	if err := ensureAddr(lo, "127.0.0.1/8"); err != nil {
		return err
	}
	if err := ensureAddr(lo, "::1/128"); err != nil {
		return err
	}

	return nil
}

// --- Helpers ---

type portRequirement struct {
	Port    int
	Proto   string // "tcp" or "udp"
	Service string
}

type processInfo struct {
	PID     int
	CmdLine string
}

func getRequiredPorts(cfg *config.Config) []portRequirement {
	reqs := []portRequirement{}

	// API
	if cfg.API == nil || cfg.API.Enabled {
		// Default 8080 or parsed from Listen
		port := 8080
		if cfg.API != nil && cfg.API.Listen != "" {
			port = parsePort(cfg.API.Listen)
		}
		reqs = append(reqs, portRequirement{Port: port, Proto: "tcp", Service: "API"})
	}

	// DNS (53 TCP/UDP)
	// Checked via Service Defaults logic usually, but here checking generic
	reqs = append(reqs, portRequirement{Port: 53, Proto: "udp", Service: "DNS"})
	reqs = append(reqs, portRequirement{Port: 53, Proto: "tcp", Service: "DNS"})

	// DHCP (67 UDP)
	reqs = append(reqs, portRequirement{Port: 67, Proto: "udp", Service: "DHCP"})

	// mDNS (5353 UDP)
	reqs = append(reqs, portRequirement{Port: 5353, Proto: "udp", Service: "mDNS"})

	return reqs
}

func parsePort(addr string) int {
	_, portStr, err := net.SplitHostPort(addr)
	if err == nil {
		p, _ := strconv.Atoi(portStr)
		return p
	}
	// Try parsing as just port
	if !strings.Contains(addr, ":") {
		p, err := strconv.Atoi(addr)
		if err == nil {
			return p
		}
	}
	if strings.HasPrefix(addr, ":") {
		p, err := strconv.Atoi(addr[1:])
		if err == nil {
			return p
		}
	}
	return 0
}

func runtimeOS() string {
	// Simple wrapper for easy mocking if needed, or just os.Getenv
	return "linux"
}

func ensureAddr(link netlink.Link, cidr string) error {
	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return err
	}

	// Check if exists
	addrs, err := netlink.AddrList(link, 0)
	if err != nil {
		return err
	}

	found := false
	for _, a := range addrs {
		if a.Equal(*addr) {
			found = true
			break
		}
	}

	if !found {
		logging.Warn(fmt.Sprintf("[HOST] Loopback missing %s. Fixing...", cidr))
		if err := netlink.AddrAdd(link, addr); err != nil {
			// Ignore EEXIST just in case race
			if !strings.Contains(err.Error(), "file exists") {
				return fmt.Errorf("failed to add address %s: %v", cidr, err)
			}
		}
	}
	return nil
}

// scanOpenPorts reads /proc/net/{tcp,udp} and maps to PIDs via /proc This is expensive!
func scanOpenPorts() (map[string]processInfo, error) {
	owners := make(map[string]processInfo)
	inodeOwner := make(map[string]processInfo)

	// Short delay to allow recent process starts to reflect in /proc (stabilization)
	time.Sleep(100 * time.Millisecond)

	// 1. Build map of inode -> PID/Cmd by scanning /proc/[pid]/fd
	// This approach is what netstat/ss does but requires iterating all procs.
	// Optimization: Only scan if we suspect usage? No, we need this map to construct the lookup.

	// Helper to get cmdline
	getCmd := func(pid int) string {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			return fmt.Sprintf("PID %d", pid)
		}
		// Cmdline elements are null separated, we just list the binary or first args
		parts := strings.Split(string(data), "\x00")
		if len(parts) > 0 {
			return filepath.Base(parts[0]) // just return binary name
		}
		return fmt.Sprintf("PID %d", pid)
	}

	// Scan PIDs
	dentries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}

	for _, d := range dentries {
		if !d.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(d.Name())
		if err != nil {
			continue
		}

		// Scan FDs
		fdPath := fmt.Sprintf("/proc/%d/fd", pid)
		fds, err := os.ReadDir(fdPath)
		if err != nil {
			continue
		}

		cmd := "" // Lazy load

		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdPath, fd.Name()))
			if err != nil {
				continue
			}
			// Look for socket:[inode]
			if strings.HasPrefix(link, "socket:[") && strings.HasSuffix(link, "]") {
				inode := link[8 : len(link)-1]
				if cmd == "" {
					cmd = getCmd(pid)
				}
				inodeOwner[inode] = processInfo{PID: pid, CmdLine: cmd}
			}
		}
	}

	// 2. Scan /proc/net/{tcp,udp,tcp6,udp6}
	files := []struct {
		Path  string
		Proto string
	}{
		{"/proc/net/tcp", "tcp"},
		{"/proc/net/udp", "udp"},
		{"/proc/net/tcp6", "tcp"},
		{"/proc/net/udp6", "udp"},
	}

	for _, f := range files {
		fh, err := os.Open(f.Path)
		if err != nil {
			continue
		}
		defer fh.Close()

		idx := 0
		scanner := bufio.NewScanner(fh)
		for scanner.Scan() {
			if idx == 0 {
				idx++
				continue // skip header
			}

			// Format: sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
			//          0: 00000000:0035 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 123456 ...
			fields := strings.Fields(scanner.Text())
			if len(fields) < 10 {
				continue
			}

			state := fields[3]
			// TCP Listen state is 0A. UDP is 07 usually but stateless.
			// Ideally we only care about listening sockets for conflicts.
			// TCP_LISTEN = 0A
			if f.Proto == "tcp" && state != "0A" {
				continue
			}

			localAddrHex := fields[1]
			inode := fields[9]

			// Parse Port
			parts := strings.Split(localAddrHex, ":")
			if len(parts) != 2 {
				continue
			}

			portVal, err := strconv.ParseInt(parts[1], 16, 64)
			if err != nil {
				continue
			}

			// Lookup owner
			if owner, ok := inodeOwner[inode]; ok {
				key := fmt.Sprintf("%s:%d", f.Proto, portVal)
				owners[key] = owner
			}
		}
	}

	return owners, nil
}
