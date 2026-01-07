package api

import (
	"encoding/json"

	"grimm.is/glacic/internal/config"
)

// ConfigItemStatus represents the staging status of a config item
type ConfigItemStatus string

const (
	StatusLive          ConfigItemStatus = "live"           // Applied and unchanged
	StatusPendingAdd    ConfigItemStatus = "pending_add"    // New, not yet applied
	StatusPendingEdit   ConfigItemStatus = "pending_edit"   // Modified, not yet applied
	StatusPendingDelete ConfigItemStatus = "pending_delete" // Will be deleted on apply
)

// PolicyWithStatus wraps a Policy with its staging status
type PolicyWithStatus struct {
	config.Policy
	Status ConfigItemStatus `json:"_status"`
}

// NATRuleWithStatus wraps a NATRule with its staging status
type NATRuleWithStatus struct {
	config.NATRule
	Status ConfigItemStatus `json:"_status"`
}

// ZoneWithStatus wraps a Zone with its staging status
type ZoneWithStatus struct {
	config.Zone
	Status ConfigItemStatus `json:"_status"`
}

// IPSetWithStatus wraps an IPSet with its staging status
type IPSetWithStatus struct {
	config.IPSet
	Status ConfigItemStatus `json:"_status"`
}

// RouteWithStatus wraps a Route with its staging status
type RouteWithStatus struct {
	config.Route
	Status ConfigItemStatus `json:"_status"`
}

// ConfigWithStatus is the full config with status fields for UI rendering
type ConfigWithStatus struct {
	// Core fields from config.Config
	SchemaVersion     string `json:"schema_version,omitempty"`
	IPForwarding      bool   `json:"ip_forwarding"`
	MSSClamping       bool   `json:"mss_clamping"`
	EnableFlowOffload bool   `json:"enable_flow_offload"`

	// Items with status
	Policies []PolicyWithStatus  `json:"policies"`
	NAT      []NATRuleWithStatus `json:"nat"`
	Zones    []ZoneWithStatus    `json:"zones"`
	IPSets   []IPSetWithStatus   `json:"ipsets"`
	Routes   []RouteWithStatus   `json:"routes"`

	// Pass through other fields unchanged
	Interfaces    []config.Interface           `json:"interfaces"`
	RoutingTables []config.RoutingTable        `json:"routing_tables,omitempty"`
	PolicyRoutes  []config.PolicyRoute         `json:"policy_routes,omitempty"`
	MarkRules     []config.MarkRule            `json:"mark_rules,omitempty"`
	UIDRouting    []config.UIDRouting          `json:"uid_routing,omitempty"`
	DHCP          *config.DHCPServer           `json:"dhcp,omitempty"`
	DNSServer     *config.DNSServer            `json:"dns_server,omitempty"`
	DNS           *config.DNS                  `json:"dns,omitempty"`
	VPN           *config.VPNConfig            `json:"vpn,omitempty"`
	Scheduler     *config.SchedulerConfig      `json:"scheduler,omitempty"`
	QoSPolicies   []config.QoSPolicy           `json:"qos_policies,omitempty"`
	Protections   []config.InterfaceProtection `json:"protections,omitempty"`
	API           *config.APIConfig            `json:"api,omitempty"`

	// Missing pointers
	Features      *config.Features            `json:"features,omitempty"`
	System        *config.SystemConfig        `json:"system,omitempty"`
	Syslog        *config.SyslogConfig        `json:"syslog,omitempty"`
	NTP           *config.NTPConfig           `json:"ntp,omitempty"`
	MDNS          *config.MDNSConfig          `json:"mdns,omitempty"`
	UPnP          *config.UPnPConfig          `json:"upnp,omitempty"`
	DDNS          *config.DDNSConfig          `json:"ddns,omitempty"`
	Replication   *config.ReplicationConfig   `json:"replication,omitempty"`
	RuleLearning  *config.RuleLearningConfig  `json:"rule_learning,omitempty"`
	AnomalyConfig *config.AnomalyConfig       `json:"anomaly_detection,omitempty"`
	Notifications *config.NotificationsConfig `json:"notifications,omitempty"`
	ThreatIntel   *config.ThreatIntel         `json:"threat_intel,omitempty"`

	// Global status
	HasPendingChanges bool `json:"_has_pending_changes"`
}

// BuildConfigWithStatus creates a ConfigWithStatus by comparing staged and running configs
func BuildConfigWithStatus(staged, running *config.Config) *ConfigWithStatus {
	if staged == nil {
		return nil
	}

	result := &ConfigWithStatus{
		SchemaVersion:     staged.SchemaVersion,
		IPForwarding:      staged.IPForwarding,
		MSSClamping:       staged.MSSClamping,
		EnableFlowOffload: staged.EnableFlowOffload,
		Interfaces:        staged.Interfaces,
		RoutingTables:     staged.RoutingTables,
		PolicyRoutes:      staged.PolicyRoutes,
		MarkRules:         staged.MarkRules,
		UIDRouting:        staged.UIDRouting,
		DHCP:              staged.DHCP,
		DNSServer:         staged.DNSServer,
		DNS:               staged.DNS,
		VPN:               staged.VPN,
		Scheduler:         staged.Scheduler,
		QoSPolicies:       staged.QoSPolicies,
		Protections:       staged.Protections,
		API:               staged.API,
		Features:          staged.Features,
		System:            staged.System,
		Syslog:            staged.Syslog,
		NTP:               staged.NTP,
		MDNS:              staged.MDNS,
		UPnP:              staged.UPnP,
		DDNS:              staged.DDNS,
		Replication:       staged.Replication,
		RuleLearning:      staged.RuleLearning,
		AnomalyConfig:     staged.AnomalyConfig,
		Notifications:     staged.Notifications,
		ThreatIntel:       staged.ThreatIntel,
	}

	// If no running config, everything is pending_add
	if running == nil {
		result.Policies = wrapPoliciesAllPending(staged.Policies)
		result.NAT = wrapNATAllPending(staged.NAT)
		result.Zones = wrapZonesAllPending(staged.Zones)
		result.IPSets = wrapIPSetsAllPending(staged.IPSets)
		result.Routes = wrapRoutesAllPending(staged.Routes)
		result.HasPendingChanges = len(staged.Policies) > 0 || len(staged.NAT) > 0 ||
			len(staged.Zones) > 0 || len(staged.IPSets) > 0 || len(staged.Routes) > 0
		return result
	}

	// Compute status for each section
	result.Policies = computePolicyStatuses(staged.Policies, running.Policies)
	result.NAT = computeNATStatuses(staged.NAT, running.NAT)
	result.Zones = computeZoneStatuses(staged.Zones, running.Zones)
	result.IPSets = computeIPSetStatuses(staged.IPSets, running.IPSets)
	result.Routes = computeRouteStatuses(staged.Routes, running.Routes)

	// Check for deletions (items in running but not in staged)
	deletedPolicies := findDeletedPolicies(staged.Policies, running.Policies)
	deletedNAT := findDeletedNAT(staged.NAT, running.NAT)
	deletedZones := findDeletedZones(staged.Zones, running.Zones)
	deletedIPSets := findDeletedIPSets(staged.IPSets, running.IPSets)
	deletedRoutes := findDeletedRoutes(staged.Routes, running.Routes)

	result.Policies = append(result.Policies, deletedPolicies...)
	result.NAT = append(result.NAT, deletedNAT...)
	result.Zones = append(result.Zones, deletedZones...)
	result.IPSets = append(result.IPSets, deletedIPSets...)
	result.Routes = append(result.Routes, deletedRoutes...)

	// Set global flag
	result.HasPendingChanges = hasAnyPending(result)

	return result
}

// Helper functions for policies
func computePolicyStatuses(staged, running []config.Policy) []PolicyWithStatus {
	runningMap := make(map[string]config.Policy)
	for _, p := range running {
		runningMap[p.From+"->"+p.To] = p
	}

	result := make([]PolicyWithStatus, 0, len(staged))
	for _, p := range staged {
		key := p.From + "->" + p.To
		if runningP, exists := runningMap[key]; exists {
			if policiesEqual(runningP, p) {
				result = append(result, PolicyWithStatus{Policy: p, Status: StatusLive})
			} else {
				result = append(result, PolicyWithStatus{Policy: p, Status: StatusPendingEdit})
			}
		} else {
			result = append(result, PolicyWithStatus{Policy: p, Status: StatusPendingAdd})
		}
	}
	return result
}

func findDeletedPolicies(staged, running []config.Policy) []PolicyWithStatus {
	stagedMap := make(map[string]bool)
	for _, p := range staged {
		stagedMap[p.From+"->"+p.To] = true
	}

	var deleted []PolicyWithStatus
	for _, p := range running {
		key := p.From + "->" + p.To
		if !stagedMap[key] {
			deleted = append(deleted, PolicyWithStatus{Policy: p, Status: StatusPendingDelete})
		}
	}
	return deleted
}

func wrapPoliciesAllPending(policies []config.Policy) []PolicyWithStatus {
	result := make([]PolicyWithStatus, len(policies))
	for i, p := range policies {
		result[i] = PolicyWithStatus{Policy: p, Status: StatusPendingAdd}
	}
	return result
}

// Helper functions for NAT
func computeNATStatuses(staged, running []config.NATRule) []NATRuleWithStatus {
	runningMap := make(map[string]config.NATRule)
	for _, r := range running {
		runningMap[r.Name] = r
	}

	result := make([]NATRuleWithStatus, 0, len(staged))
	for _, r := range staged {
		if runningR, exists := runningMap[r.Name]; exists {
			if natRulesEqual(runningR, r) {
				result = append(result, NATRuleWithStatus{NATRule: r, Status: StatusLive})
			} else {
				result = append(result, NATRuleWithStatus{NATRule: r, Status: StatusPendingEdit})
			}
		} else {
			result = append(result, NATRuleWithStatus{NATRule: r, Status: StatusPendingAdd})
		}
	}
	return result
}

func findDeletedNAT(staged, running []config.NATRule) []NATRuleWithStatus {
	stagedMap := make(map[string]bool)
	for _, r := range staged {
		stagedMap[r.Name] = true
	}

	var deleted []NATRuleWithStatus
	for _, r := range running {
		if !stagedMap[r.Name] {
			deleted = append(deleted, NATRuleWithStatus{NATRule: r, Status: StatusPendingDelete})
		}
	}
	return deleted
}

func wrapNATAllPending(rules []config.NATRule) []NATRuleWithStatus {
	result := make([]NATRuleWithStatus, len(rules))
	for i, r := range rules {
		result[i] = NATRuleWithStatus{NATRule: r, Status: StatusPendingAdd}
	}
	return result
}

// Helper functions for Zones
func computeZoneStatuses(staged, running []config.Zone) []ZoneWithStatus {
	runningMap := make(map[string]config.Zone)
	for _, z := range running {
		runningMap[z.Name] = z
	}

	result := make([]ZoneWithStatus, 0, len(staged))
	for _, z := range staged {
		if runningZ, exists := runningMap[z.Name]; exists {
			if zonesEqual(runningZ, z) {
				result = append(result, ZoneWithStatus{Zone: z, Status: StatusLive})
			} else {
				result = append(result, ZoneWithStatus{Zone: z, Status: StatusPendingEdit})
			}
		} else {
			result = append(result, ZoneWithStatus{Zone: z, Status: StatusPendingAdd})
		}
	}
	return result
}

func findDeletedZones(staged, running []config.Zone) []ZoneWithStatus {
	stagedMap := make(map[string]bool)
	for _, z := range staged {
		stagedMap[z.Name] = true
	}

	var deleted []ZoneWithStatus
	for _, z := range running {
		if !stagedMap[z.Name] {
			deleted = append(deleted, ZoneWithStatus{Zone: z, Status: StatusPendingDelete})
		}
	}
	return deleted
}

func wrapZonesAllPending(zones []config.Zone) []ZoneWithStatus {
	result := make([]ZoneWithStatus, len(zones))
	for i, z := range zones {
		result[i] = ZoneWithStatus{Zone: z, Status: StatusPendingAdd}
	}
	return result
}

// Helper functions for IPSets
func computeIPSetStatuses(staged, running []config.IPSet) []IPSetWithStatus {
	runningMap := make(map[string]config.IPSet)
	for _, i := range running {
		runningMap[i.Name] = i
	}

	result := make([]IPSetWithStatus, 0, len(staged))
	for _, i := range staged {
		if runningI, exists := runningMap[i.Name]; exists {
			if ipsetsEqual(runningI, i) {
				result = append(result, IPSetWithStatus{IPSet: i, Status: StatusLive})
			} else {
				result = append(result, IPSetWithStatus{IPSet: i, Status: StatusPendingEdit})
			}
		} else {
			result = append(result, IPSetWithStatus{IPSet: i, Status: StatusPendingAdd})
		}
	}
	return result
}

func findDeletedIPSets(staged, running []config.IPSet) []IPSetWithStatus {
	stagedMap := make(map[string]bool)
	for _, i := range staged {
		stagedMap[i.Name] = true
	}

	var deleted []IPSetWithStatus
	for _, i := range running {
		if !stagedMap[i.Name] {
			deleted = append(deleted, IPSetWithStatus{IPSet: i, Status: StatusPendingDelete})
		}
	}
	return deleted
}

func wrapIPSetsAllPending(ipsets []config.IPSet) []IPSetWithStatus {
	result := make([]IPSetWithStatus, len(ipsets))
	for i, s := range ipsets {
		result[i] = IPSetWithStatus{IPSet: s, Status: StatusPendingAdd}
	}
	return result
}

// Helper functions for Routes
func computeRouteStatuses(staged, running []config.Route) []RouteWithStatus {
	runningMap := make(map[string]config.Route)
	for _, r := range running {
		runningMap[r.Destination] = r
	}

	result := make([]RouteWithStatus, 0, len(staged))
	for _, r := range staged {
		if runningR, exists := runningMap[r.Destination]; exists {
			if routesEqual(runningR, r) {
				result = append(result, RouteWithStatus{Route: r, Status: StatusLive})
			} else {
				result = append(result, RouteWithStatus{Route: r, Status: StatusPendingEdit})
			}
		} else {
			result = append(result, RouteWithStatus{Route: r, Status: StatusPendingAdd})
		}
	}
	return result
}

func findDeletedRoutes(staged, running []config.Route) []RouteWithStatus {
	stagedMap := make(map[string]bool)
	for _, r := range staged {
		stagedMap[r.Destination] = true
	}

	var deleted []RouteWithStatus
	for _, r := range running {
		if !stagedMap[r.Destination] {
			deleted = append(deleted, RouteWithStatus{Route: r, Status: StatusPendingDelete})
		}
	}
	return deleted
}

func wrapRoutesAllPending(routes []config.Route) []RouteWithStatus {
	result := make([]RouteWithStatus, len(routes))
	for i, r := range routes {
		result[i] = RouteWithStatus{Route: r, Status: StatusPendingAdd}
	}
	return result
}

// Equality helpers (using JSON comparison for simplicity)
func policiesEqual(a, b config.Policy) bool {
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	return string(aJSON) == string(bJSON)
}

func natRulesEqual(a, b config.NATRule) bool {
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	return string(aJSON) == string(bJSON)
}

func zonesEqual(a, b config.Zone) bool {
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	return string(aJSON) == string(bJSON)
}

func ipsetsEqual(a, b config.IPSet) bool {
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	return string(aJSON) == string(bJSON)
}

func routesEqual(a, b config.Route) bool {
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	return string(aJSON) == string(bJSON)
}

// hasAnyPending checks if any items have pending status
func hasAnyPending(c *ConfigWithStatus) bool {
	for _, p := range c.Policies {
		if p.Status != StatusLive {
			return true
		}
	}
	for _, n := range c.NAT {
		if n.Status != StatusLive {
			return true
		}
	}
	for _, z := range c.Zones {
		if z.Status != StatusLive {
			return true
		}
	}
	for _, i := range c.IPSets {
		if i.Status != StatusLive {
			return true
		}
	}
	for _, r := range c.Routes {
		if r.Status != StatusLive {
			return true
		}
	}
	return false
}
