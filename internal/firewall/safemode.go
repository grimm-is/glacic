package firewall

import (
	"fmt"
	"strings"
)

// SafeModeConfig holds the pre-rendered safe mode ruleset and configuration.
type SafeModeConfig struct {
	// Pre-rendered nftables script for instant application
	Script string

	// Interfaces allowed to access management (typically LAN interfaces)
	ManagementInterfaces []string
}

// BuildSafeModeScript generates a minimal safe mode ruleset.
// Safe Mode:
// - INPUT: Allow LAN access to Web UI/API, SSH, ICMP, DNS
// - FORWARD: DROP all (including established - kills existing connections)
// - OUTPUT: Allow VPN services (Tailscale/WireGuard) for remote management
//
// The interfaces parameter specifies which interfaces are considered "trusted"
// for management access. If empty, all non-WAN interfaces are assumed trusted.
func BuildSafeModeScript(tableName string, trustedInterfaces []string) string {
	sb := NewScriptBuilder(tableName, "inet")

	// Flush and recreate table
	sb.AddLine("flush ruleset")
	sb.AddTable()

	// INPUT chain - policy DROP
	sb.AddChain("input", "filter", "input", 0, "drop")
	// FORWARD chain - policy DROP (no forwarding in safe mode)
	// NOTE: We do NOT accept established here - we want to kill existing forwarded connections
	sb.AddChain("forward", "filter", "forward", 0, "drop")
	// OUTPUT chain - policy DROP
	sb.AddChain("output", "filter", "output", 0, "drop")

	// Input: Allow established/related for local services
	sb.AddRule("input", "ct state established,related accept")
	// Output: Allow established/related for local services
	sb.AddRule("output", "ct state established,related accept")
	// Forward: NO established accept - this kills all forwarded connections immediately

	// Loopback always allowed
	sb.AddRule("input", "iifname \"lo\" accept")
	sb.AddRule("output", "oifname \"lo\" accept")

	// ICMP for diagnostics
	sb.AddRule("input", "meta l4proto icmp accept")
	sb.AddRule("input", "meta l4proto icmpv6 accept")
	sb.AddRule("output", "meta l4proto icmp accept")
	sb.AddRule("output", "meta l4proto icmpv6 accept")

	// DNS for router functionality
	sb.AddRule("output", "udp dport 53 accept")
	sb.AddRule("output", "tcp dport 53 accept")

	// DHCP client (for WAN)
	sb.AddRule("input", "udp dport 67-68 accept")
	sb.AddRule("output", "udp dport 67-68 accept")

	// VPN Services - allow Tailscale/WireGuard for remote management
	// Tailscale uses UDP 41641, WireGuard typically 51820
	sb.AddRule("input", "udp dport { 41641, 51820 } accept")
	sb.AddRule("output", "udp dport { 41641, 51820 } accept")
	// Allow traffic on WireGuard interfaces
	sb.AddRule("input", "iifname \"wg*\" accept")
	sb.AddRule("input", "iifname \"tailscale*\" accept")
	sb.AddRule("output", "oifname \"wg*\" accept")
	sb.AddRule("output", "oifname \"tailscale*\" accept")

	// Management access from trusted interfaces
	if len(trustedInterfaces) > 0 {
		var quotedIfaces []string
		for _, iface := range trustedInterfaces {
			quotedIfaces = append(quotedIfaces, fmt.Sprintf("%q", iface))
		}
		ifaceSet := strings.Join(quotedIfaces, ", ")

		// Web UI / API ports
		sb.AddRule("input", fmt.Sprintf("iifname { %s } tcp dport { 22, 80, 443, 8080, 8443 } accept", ifaceSet))
		// DNS from LAN clients
		sb.AddRule("input", fmt.Sprintf("iifname { %s } udp dport 53 accept", ifaceSet))
		sb.AddRule("input", fmt.Sprintf("iifname { %s } tcp dport 53 accept", ifaceSet))
	} else {
		// Allow from any interface if none specified
		sb.AddRule("input", "tcp dport { 22, 80, 443, 8080, 8443 } accept")
		sb.AddRule("input", "udp dport 53 accept")
		sb.AddRule("input", "tcp dport 53 accept")
	}

	// Allow output to sandbox (for API responses)
	sb.AddRule("output", "ip daddr 169.254.255.2 tcp dport { 8080, 8443 } accept")

	// Log drops for debugging
	sb.AddRule("input", "limit rate 5/minute log prefix \"SAFEMODE_DROP_IN: \" group 0 counter drop")
	sb.AddRule("forward", "limit rate 5/minute log prefix \"SAFEMODE_DROP_FWD: \" group 0 counter drop")
	sb.AddRule("output", "limit rate 5/minute log prefix \"SAFEMODE_DROP_OUT: \" group 0 counter drop")

	return sb.Build()
}

// PreRenderSafeMode generates and caches the safe mode ruleset for instant application.
// Call this during startup after interfaces are known.
func PreRenderSafeMode(tableName string, trustedInterfaces []string) *SafeModeConfig {
	return &SafeModeConfig{
		Script:               BuildSafeModeScript(tableName, trustedInterfaces),
		ManagementInterfaces: trustedInterfaces,
	}
}
