package firewall

import "grimm.is/glacic/internal/config"

// Config represents the specific configuration required by the firewall.
// It is a subset of the global configuration.
type Config struct {
	Zones             []config.Zone
	Interfaces        []config.Interface
	IPSets            []config.IPSet
	Policies          []config.Policy
	NAT               []config.NATRule
	API               *config.APIConfig // For DisableSandbox check in NAT rules
	Web               *config.WebConfig // For web access rules
	VPN               *config.VPNConfig
	DNS               *config.DNS // For dns.inspect rules
	MDNS              *config.MDNSConfig
	Features          *config.Features
	Protections       []config.InterfaceProtection
	EnableFlowOffload bool
	MSSClamping       bool
	MarkRules         []config.MarkRule
	UIDRouting        []config.UIDRouting
	RuleLearning      *config.RuleLearningConfig // For inline mode nfqueue support
	NTP               *config.NTPConfig
	UPnP              *config.UPnPConfig
}

// FromGlobalConfig extracts the firewall configuration from the global config.
// Wildcard policies (using * or glob patterns) are automatically expanded.
func FromGlobalConfig(g *config.Config) *Config {
	if g == nil {
		return &Config{}
	}

	// Expand wildcard policies to concrete zone pairs
	expandedPolicies := config.ExpandPolicies(g.Policies, g.Zones)

	return &Config{
		Zones:             g.Zones,
		Interfaces:        g.Interfaces,
		IPSets:            g.IPSets,
		Policies:          expandedPolicies,
		NAT:               g.NAT,
		API:               g.API,
		Web:               g.Web,
		VPN:               g.VPN,
		DNS:               g.DNS,
		MDNS:              g.MDNS,
		Features:          g.Features,
		Protections:       g.Protections,
		EnableFlowOffload: g.EnableFlowOffload,
		MSSClamping:       g.MSSClamping,
		MarkRules:         g.MarkRules,
		UIDRouting:        g.UIDRouting,
		RuleLearning:      g.RuleLearning,
		NTP:               g.NTP,
		UPnP:              g.UPnP,
	}
}
