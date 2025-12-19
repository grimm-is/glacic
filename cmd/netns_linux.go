//go:build linux

package cmd

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/logging"
)

var (
	nsName   = brand.LowerName + "-api"
	vethHost = "veth-api-host"
	vethNS   = "veth-api-ns"
	ipHost   = "169.254.255.1/30"
	ipNS     = "169.254.255.2/30"
)

// setupNetworkNamespace sets up the isolation network namespace and interfaces
func setupNetworkNamespace() error {
	// 1. Create/Get Namespace
	// Lock OS thread to ensure we don't switch namespaces on other goroutines
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Initial namespace
	origns, err := netns.Get()
	if err != nil {
		return fmt.Errorf("failed to get original netns: %w", err)
	}
	defer origns.Close()

	// Create named namespace if it doesn't exist
	newns, err := netns.GetFromName(nsName)
	if err != nil {
		// Doesn't exist, create it
		newns, err = netns.NewNamed(nsName)
		if err != nil {
			return fmt.Errorf("failed to create netns %s: %w", nsName, err)
		}
	}
	defer newns.Close()

	// Switch back to original NS to setup veth
	if err := netns.Set(origns); err != nil {
		return fmt.Errorf("failed to switch back to original ns: %w", err)
	}

	// 2. Create Veth Pair
	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name: vethHost,
		},
		PeerName: vethNS,
	}

	// Check if already exists (host side)
	if l, err := netlink.LinkByName(vethHost); err == nil {
		if err := netlink.LinkDel(l); err != nil {
			return fmt.Errorf("failed to delete existing veth %s: %w", vethHost, err)
		}
	}
	// Check if peer exists in host ns (from failed setup)
	if l, err := netlink.LinkByName(vethNS); err == nil {
		if err := netlink.LinkDel(l); err != nil {
			return fmt.Errorf("failed to delete existing veth peer %s: %w", vethNS, err)
		}
	}

	if err := netlink.LinkAdd(veth); err != nil {
		return fmt.Errorf("failed to create veth pair: %w", err)
	}

	// 3. Move Peer to Namespace
	peer, err := netlink.LinkByName(vethNS)
	if err != nil {
		return fmt.Errorf("failed to get veth peer: %w", err)
	}

	if err := netlink.LinkSetNsFd(peer, int(newns)); err != nil {
		return fmt.Errorf("failed to move veth peer to ns: %w", err)
	}

	// 4. Configure Host Interface
	hostLink, err := netlink.LinkByName(vethHost)
	if err != nil {
		return err
	}

	addr, _ := netlink.ParseAddr(ipHost)
	if err := netlink.AddrAdd(hostLink, addr); err != nil {
		// Ignore EEXIST
	}

	if err := netlink.LinkSetUp(hostLink); err != nil {
		return fmt.Errorf("failed to bring up host veth: %w", err)
	}
	disableTxOffload(vethHost)

	// 5. Configure NS Interface (Switch to NS)
	if err := netns.Set(newns); err != nil {
		return fmt.Errorf("failed to enter netns: %w", err)
	}

	// We are now in the new namespace

	// Create loopback
	lo, _ := netlink.LinkByName("lo")
	if err := netlink.LinkSetUp(lo); err != nil {
		return fmt.Errorf("failed to setup loopback in ns: %w", err)
	}

	// Setup veth peer
	nsLink, err := netlink.LinkByName(vethNS)
	if err != nil {
		return fmt.Errorf("failed to get veth peer in ns: %w", err)
	}

	addrNS, _ := netlink.ParseAddr(ipNS)
	if err := netlink.AddrAdd(nsLink, addrNS); err != nil {
		// Ignore EEXIST
	}

	if err := netlink.LinkSetUp(nsLink); err != nil {
		return fmt.Errorf("failed to up veth peer: %w", err)
	}
	disableTxOffload(vethNS)

	// Default Gateway (to host)
	route := &netlink.Route{
		Scope: netlink.SCOPE_UNIVERSE,
		Gw:    addr.IP,
	}
	if err := netlink.RouteAdd(route); err != nil {
		// Ignore EEXIST
	}

	// Switch back
	if err := netns.Set(origns); err != nil {
		return fmt.Errorf("failed to return to original ns: %w", err)
	}

	return nil
}

// configureHostFirewall sets up anti-lockout nftables rules for the API interface.
// This ensures that even if firewall rules are misconfigured, localhost and
// configured management networks can still access the API.
// Rules are added to a dedicated "glacic-anti-lockout" chain with high priority.
func configureHostFirewall(interfaces []string) error {
	if len(interfaces) == 0 {
		logging.Warn("Anti-lockout protection DISABLED - no networks or interfaces configured")
		logging.Warn("You may lose access to the management interface if firewall rules are misconfigured!")
		return nil
	}

	lockoutTable := brand.LowerName + "-lockout"
	lockoutTableLocal := brand.LowerName + "-lockout-local"
	nsIP := strings.Split(ipNS, "/")[0]
	// hostIP := strings.Split(ipHost, "/")[0] // Unused in script but good for logs

	// Prepare sets
	// Quote interfaces just in case
	var qIfaces []string
	for _, iface := range interfaces {
		qIfaces = append(qIfaces, fmt.Sprintf("%q", iface))
	}
	ifaceSet := strings.Join(qIfaces, ", ")
	if len(interfaces) > 1 {
		ifaceSet = "{ " + ifaceSet + " }"
	}

	// Build the script
	var sb strings.Builder

	// Delete existing tables (ignore errors if strictly needed, but in script 'table' command is idempotent/additive,
	// so to force clean state we usually want flush or delete.
	// However, simply redefining the chains with 'flush chain' inside is cleaner if we want to preserve atomicity issues?
	// Actually, just deleting the table at the start is easiest to ensure no stale rules remain.)
	// BUT: 'delete table' fails if it doesn't exist.
	// We can use a small hack: 'add table ...; delete table ...; add table ...' ?
	// Or just 'flush ruleset' is too broad.
	// Let's rely on standard nft behavior: redefining a chain NOT flushing it appends.
	// So we should FLUSH them.

	sb.WriteString(fmt.Sprintf("table inet %s {\n", lockoutTable))
	sb.WriteString("  chain prerouting {\n")
	sb.WriteString("    type nat hook prerouting priority dstnat; policy accept;\n")
	sb.WriteString("    flush ruleset\n") // Flush this chain? No, 'flush chain' syntax is 'flush chain inet table chain'
	// Actually inside table block:
	sb.WriteString("  }\n")
	sb.WriteString("}\n")

	// Better approach: Generate robust script that flushes specific chains.
	// Or just use the existing runNft("delete", "table"...) pattern briefly before applying new ones?
	// The user asked for "condense rules".
	// Let's generate a script that FLUSHES the table if it exists.
	// 'table inet x { }' creates it.
	// 'flush table inet x' clears it.

	sb.Reset()
	sb.WriteString(fmt.Sprintf(`
add table inet %s
flush table inet %s
table inet %s {
	chain prerouting {
		type nat hook prerouting priority -110; policy accept;
		iifname %s tcp dport { 8080, 8443 } dnat ip to %s
	}
	chain forward {
		type filter hook forward priority -200; policy accept;
		iifname %s ip daddr %s tcp dport { 8080, 8443 } accept
	}
	chain input {
		type filter hook input priority -200; policy accept;
		iifname %s tcp dport { 22, 8080, 8443 } accept
	}
}

add table ip %s
flush table ip %s
table ip %s {
	chain output {
		type nat hook output priority -110; policy accept;
		tcp dport { 8080, 8443 } dnat to %s
	}
	chain postrouting {
		type nat hook postrouting priority 100; policy accept;
		ip daddr %s masquerade
	}
}
`, lockoutTable, lockoutTable, lockoutTable,
		ifaceSet, nsIP,
		ifaceSet, nsIP,
		ifaceSet,
		lockoutTableLocal, lockoutTableLocal, lockoutTableLocal,
		nsIP,
		nsIP,
	))

	// Apply script
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(sb.String())
	if output, err := cmd.CombinedOutput(); err != nil {
		logging.Warn(fmt.Sprintf("Failed to apply anti-lockout rules: %v\nOutput: %s", err, string(output)))
		return fmt.Errorf("failed to apply anti-lockout rules: %w", err)
	}

	logging.Info(fmt.Sprintf("Anti-lockout rules configured for %s", ifaceSet))
	return nil
}

// runNft executes an nft command, logging errors if they occur
func runNft(args ...string) {
	cmd := exec.Command("nft", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		// Log error physically so we see it
		logging.Warn(fmt.Sprintf("Failed to run nft command: %v, Output: %s, Args: %v", err, string(output), args))
	}
}

// disableTxOffload disables TX checksum offload on the specified interface.
// This is critical for veth pairs where hardware offload is simulated but not present,
// leading to bad checksums and dropped packets (hanging connections).
func disableTxOffload(iface string) {
	// Enable ARP on the host side interface
	// Also enable route_localnet to allow routing 127.0.0.0/8 traffic (essential for localhost forwarding)
	// And disable rp_filter because we are preserving source IPs (No SNAT), which causes asymmetric routing paths
	// from the kernel's perspective (Response comes from veth, but route to client is on eth).
	for _, key := range []string{"route_localnet=1", "rp_filter=0"} {
		if err := exec.Command("sysctl", "-w", fmt.Sprintf("net.ipv4.conf.%s.%s", iface, key)).Run(); err != nil {
			logging.Warn(fmt.Sprintf("Failed to set %s on %s: %v", key, iface, err))
		}
	}

	// Try ethtool first (most reliable)
	// We specifically need to disable tx-checksumming (tx-checksum-ip-generic or similar)
	cmd := exec.Command("ethtool", "-K", iface, "tx", "off")
	if output, err := cmd.CombinedOutput(); err != nil {
		logging.Warn(fmt.Sprintf("Ethtool failed on %s: %v (Output: %s)", iface, err, string(output)))
		// Try disabling other offloads that might cause issues if generic 'tx' failed
		exec.Command("ethtool", "-K", iface, "tso", "off", "gso", "off", "gro", "off").Run()
	}
}

// isIPv6 checks if a CIDR is IPv6
func isIPv6(cidr string) bool {
	return strings.Contains(cidr, ":")
}

// isIsolated checks if we are running inside the isolated namespace
func isIsolated() bool {
	// Compare current netns with expected
	// Heuristic: Check if our veth-api-ns has the expected IP
	// This is a simple check.
	ns, err := netns.Get()
	if err != nil {
		return false
	}
	defer ns.Close()

	// A perfect check is hard without known handles.
	// But if we have interface "veth-api-ns", we are likely inside.
	_, err = netlink.LinkByName(vethNS)
	return err == nil
}
