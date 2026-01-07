package network

import (
	"fmt"
	"grimm.is/glacic/internal/config"
	"net"
	"strconv"
	"strings"
	"sync"
)

// RoutingMark represents a firewall mark used for policy routing.
// Marks are 32-bit values that can be set on packets by nftables
// and matched by ip rule for routing decisions.
type RoutingMark uint32

// Mark layout (32-bit):
// Bits 0-7:   Category (0x00-0xFF)
// Bits 8-15:  Subcategory/Index within category
// Bits 16-23: Reserved for future use
// Bits 24-31: User-defined / application-specific
//
// This allows up to 256 items per category while keeping marks readable.

// Mark categories
const (
	MarkCategorySystem RoutingMark = 0x00 // System marks (0x00XX)
	MarkCategoryWAN    RoutingMark = 0x01 // WAN routing (0x01XX) - up to 256 WANs
	MarkCategoryVPN    RoutingMark = 0x02 // VPN routing (0x02XX) - up to 256 VPNs
	MarkCategoryZone   RoutingMark = 0x03 // Zone-based (0x03XX) - up to 256 zones
	MarkCategoryQoS    RoutingMark = 0x04 // QoS classes (0x04XX) - up to 256 classes
	MarkCategoryUser   RoutingMark = 0x10 // User-defined (0x10XX+)
)

// Mark masks for category extraction
const (
	MarkMaskCategory RoutingMark = 0xFF00 // Extract category
	MarkMaskIndex    RoutingMark = 0x00FF // Extract index within category
	MarkMaskFull     RoutingMark = 0xFFFF // Full mark (category + index)
)

// System marks (category 0x00)
const (
	MarkNone        RoutingMark = 0x0000
	MarkBypassVPN   RoutingMark = 0x0001 // Bypass all VPNs, use default route
	MarkForceVPN    RoutingMark = 0x0002 // Force through default VPN
	MarkLoadBalance RoutingMark = 0x0010 // Load balanced routing
	MarkFailover    RoutingMark = 0x0011 // Failover routing
	MarkLocal       RoutingMark = 0x0020 // Force local delivery
	MarkBlackhole   RoutingMark = 0x0021 // Drop (blackhole route)
	MarkTransparent RoutingMark = 0x0030 // Transparent proxy
	MarkNoTrack     RoutingMark = 0x0031 // Skip connection tracking
)

// WAN marks (category 0x01) - supports up to 256 WAN interfaces
const (
	MarkWANBase RoutingMark = 0x0100 // WAN marks: 0x0100 + index
	MarkWAN1    RoutingMark = 0x0100
	MarkWAN2    RoutingMark = 0x0101
	MarkWAN3    RoutingMark = 0x0102
	MarkWAN4    RoutingMark = 0x0103
	// ... up to MarkWAN256 = 0x01FF
)

// VPN marks (category 0x02) - supports up to 256 VPN tunnels
const (
	MarkVPNBase RoutingMark = 0x0200 // VPN marks: 0x0200 + index
	// Subcategories within VPN:
	// 0x0200-0x021F: WireGuard tunnels (32)
	// 0x0220-0x023F: Tailscale/Headscale (32)
	// 0x0240-0x025F: OpenVPN (32)
	// 0x0260-0x027F: IPsec (32)
	// 0x0280-0x02FF: Other/custom VPNs (128)
	MarkWireGuardBase RoutingMark = 0x0200
	MarkTailscaleBase RoutingMark = 0x0220
	MarkOpenVPNBase   RoutingMark = 0x0240
	MarkIPsecBase     RoutingMark = 0x0260
	MarkVPNCustomBase RoutingMark = 0x0280
)

// Zone marks (category 0x03) - for zone-based routing
const (
	MarkZoneBase RoutingMark = 0x0300 // Zone marks: 0x0300 + zone index
)

// QoS marks (category 0x04) - for traffic classification
const (
	MarkQoSBase        RoutingMark = 0x0400
	MarkQoSRealtime    RoutingMark = 0x0400 // Voice, video
	MarkQoSInteractive RoutingMark = 0x0401 // Gaming, SSH
	MarkQoSBulk        RoutingMark = 0x0402 // Downloads, backups
	MarkQoSBackground  RoutingMark = 0x0403 // Updates, sync
)

// User-defined marks (category 0x10+)
const (
	MarkUserBase RoutingMark = 0x1000 // User marks start here
)

// RoutingTable represents a routing table ID.
// Linux supports tables 0-255 by default, but can use 32-bit IDs.
type RoutingTable uint32

// Well-known routing tables
const (
	TableMain    RoutingTable = 254 // Main routing table
	TableLocal   RoutingTable = 255 // Local routing table
	TableDefault RoutingTable = 253 // Default routing table

	// WAN tables (10-29) - up to 20 WANs
	TableWANBase RoutingTable = 10
	TableWAN1    RoutingTable = 10
	TableWAN2    RoutingTable = 11
	TableWAN3    RoutingTable = 12
	TableWAN4    RoutingTable = 13

	// VPN tables (30-99) - up to 70 VPNs
	TableVPNBase       RoutingTable = 30
	TableWireGuardBase RoutingTable = 30 // 30-49: WireGuard (20)
	TableTailscaleBase RoutingTable = 50 // 50-59: Tailscale (10)
	TableOpenVPNBase   RoutingTable = 60 // 60-69: OpenVPN (10)
	TableIPsecBase     RoutingTable = 70 // 70-79: IPsec (10)
	TableVPNCustomBase RoutingTable = 80 // 80-99: Custom (20)

	// Special tables (100-109)
	TableBypassVPN   RoutingTable = 100
	TableLoadBalance RoutingTable = 101
	TableFailover    RoutingTable = 102
	TableTransparent RoutingTable = 103

	// User-defined tables (200-252)
	TableUserBase RoutingTable = 200
	TableUserMax  RoutingTable = 252
)

// PolicyRoute defines a policy-based routing rule.
type PolicyRoute struct {
	Name     string       // Descriptive name
	Priority int          // Rule priority (lower = higher priority)
	Mark     RoutingMark  // Firewall mark to match
	MarkMask RoutingMark  // Mask for mark matching (0 = exact match)
	Table    RoutingTable // Routing table to use

	// Match criteria (all optional, combined with AND)
	FromSource    string      // Source IP/CIDR
	ToDestination string      // Destination IP/CIDR
	IIF           string      // Input interface
	OIF           string      // Output interface
	IPProto       int         // IP protocol number
	FWMark        RoutingMark // Alternative: match fwmark directly

	// Actions
	Goto        RoutingTable // Jump to another table
	Blackhole   bool         // Drop matching packets
	Unreachable bool         // Return ICMP unreachable
	Prohibit    bool         // Return ICMP prohibited

	// Metadata
	Enabled bool
	Comment string
}

// RoutingTableConfig defines routes for a custom routing table.
type RoutingTableConfig struct {
	ID      RoutingTable
	Name    string
	Routes  []TableRoute
	Default *TableRoute // Default gateway for this table
}

// TableRoute represents a route within a routing table.
type TableRoute struct {
	Destination string // CIDR
	Gateway     string // Next hop IP
	Interface   string // Output interface
	Metric      int    // Route metric
	MTU         int    // Path MTU
	Source      string // Preferred source IP
}

// PolicyRoutingManager manages policy-based routing.
type PolicyRoutingManager struct {
	mu         sync.RWMutex
	tables     map[RoutingTable]*RoutingTableConfig
	rules      []PolicyRoute
	markAlloc  RoutingMark  // Next available user mark
	tableAlloc RoutingTable // Next available user table
	executor   CommandExecutor
}

// NewPolicyRoutingManager creates a new policy routing manager.
func NewPolicyRoutingManager() *PolicyRoutingManager {
	return &PolicyRoutingManager{
		tables:     make(map[RoutingTable]*RoutingTableConfig),
		rules:      []PolicyRoute{},
		markAlloc:  MarkUserBase,
		tableAlloc: TableUserBase,
		executor:   DefaultCommandExecutor,
	}
}

// AllocateMark allocates a new routing mark for user-defined policies.
func (m *PolicyRoutingManager) AllocateMark(name string) RoutingMark {
	m.mu.Lock()
	defer m.mu.Unlock()

	mark := m.markAlloc
	m.markAlloc++
	return mark
}

// AllocateTable allocates a new routing table for user-defined policies.
func (m *PolicyRoutingManager) AllocateTable(name string) RoutingTable {
	m.mu.Lock()
	defer m.mu.Unlock()

	table := m.tableAlloc
	m.tableAlloc++
	return table
}

// CreateTable creates a new routing table with the given configuration.
func (m *PolicyRoutingManager) CreateTable(cfg *RoutingTableConfig) error {
	m.mu.Lock()
	m.tables[cfg.ID] = cfg
	m.mu.Unlock()

	// Add routes to the table
	for _, route := range cfg.Routes {
		if err := m.addTableRoute(cfg.ID, route); err != nil {
			return fmt.Errorf("failed to add route to table %d: %w", cfg.ID, err)
		}
	}

	// Add default route if specified
	if cfg.Default != nil {
		if err := m.addTableRoute(cfg.ID, *cfg.Default); err != nil {
			return fmt.Errorf("failed to add default route to table %d: %w", cfg.ID, err)
		}
	}

	return nil
}

// addTableRoute adds a route to a specific routing table.
func (m *PolicyRoutingManager) addTableRoute(table RoutingTable, route TableRoute) error {
	args := []string{"route", "add"}

	if route.Destination == "" || route.Destination == "default" {
		args = append(args, "default")
	} else {
		args = append(args, route.Destination)
	}

	if route.Gateway != "" {
		args = append(args, "via", route.Gateway)
	}

	if route.Interface != "" {
		args = append(args, "dev", route.Interface)
	}

	if route.Metric > 0 {
		args = append(args, "metric", strconv.Itoa(route.Metric))
	}

	if route.MTU > 0 {
		args = append(args, "mtu", strconv.Itoa(route.MTU))
	}

	if route.Source != "" {
		args = append(args, "src", route.Source)
	}

	args = append(args, "table", strconv.Itoa(int(table)))

	_, err := m.executor.RunCommand("ip", args...)
	return err
}

// AddRule adds a policy routing rule.
func (m *PolicyRoutingManager) AddRule(rule PolicyRoute) error {
	m.mu.Lock()
	m.rules = append(m.rules, rule)
	m.mu.Unlock()

	return m.applyRule(rule)
}

// applyRule applies a single policy routing rule using ip rule.
func (m *PolicyRoutingManager) applyRule(rule PolicyRoute) error {
	if !rule.Enabled {
		return nil
	}

	args := []string{"rule", "add"}

	// Priority
	if rule.Priority > 0 {
		args = append(args, "priority", strconv.Itoa(rule.Priority))
	}

	// Match criteria
	if rule.FromSource != "" {
		args = append(args, "from", rule.FromSource)
	} else {
		args = append(args, "from", "all")
	}

	if rule.ToDestination != "" {
		args = append(args, "to", rule.ToDestination)
	}

	if rule.IIF != "" {
		args = append(args, "iif", rule.IIF)
	}

	if rule.OIF != "" {
		args = append(args, "oif", rule.OIF)
	}

	// Firewall mark matching
	if rule.Mark != MarkNone {
		if rule.MarkMask != MarkNone {
			args = append(args, "fwmark", fmt.Sprintf("0x%x/0x%x", rule.Mark, rule.MarkMask))
		} else {
			args = append(args, "fwmark", fmt.Sprintf("0x%x", rule.Mark))
		}
	}

	// Action
	if rule.Blackhole {
		args = append(args, "blackhole")
	} else if rule.Unreachable {
		args = append(args, "unreachable")
	} else if rule.Prohibit {
		args = append(args, "prohibit")
	} else if rule.Goto != 0 {
		args = append(args, "goto", strconv.Itoa(int(rule.Goto)))
	} else {
		args = append(args, "table", strconv.Itoa(int(rule.Table)))
	}

	_, err := m.executor.RunCommand("ip", args...)
	return err
}

// DeleteRule removes a policy routing rule.
func (m *PolicyRoutingManager) DeleteRule(rule PolicyRoute) error {
	args := []string{"rule", "del"}

	if rule.Priority > 0 {
		args = append(args, "priority", strconv.Itoa(rule.Priority))
	}

	if rule.FromSource != "" {
		args = append(args, "from", rule.FromSource)
	}

	if rule.Mark != MarkNone {
		args = append(args, "fwmark", fmt.Sprintf("0x%x", rule.Mark))
	}

	if rule.Table != 0 {
		args = append(args, "table", strconv.Itoa(int(rule.Table)))
	}

	_, err := m.executor.RunCommand("ip", args...)
	return err
}

// FlushTable removes all routes from a routing table.
func (m *PolicyRoutingManager) FlushTable(table RoutingTable) error {
	_, err := m.executor.RunCommand("ip", "route", "flush", "table", strconv.Itoa(int(table)))
	return err
}

// FlushRules removes all policy routing rules with a specific mark.
func (m *PolicyRoutingManager) FlushRulesByMark(mark RoutingMark) error {
	// List all rules and delete matching ones
	output, err := m.executor.RunCommand("ip", "rule", "show")
	if err != nil {
		return err
	}

	markStr := fmt.Sprintf("0x%x", mark)
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "fwmark "+markStr) {
			// Extract priority
			parts := strings.Fields(line)
			if len(parts) > 0 {
				priority := strings.TrimSuffix(parts[0], ":")
				m.executor.RunCommand("ip", "rule", "del", "priority", priority)
			}
		}
	}

	return nil
}

// SetupMultiWAN configures multi-WAN routing with failover/load balancing.
func (m *PolicyRoutingManager) SetupMultiWAN(wans []WANConfig) error {
	for i, wan := range wans {
		table := TableWAN1 + RoutingTable(i)
		mark := MarkWAN1 + RoutingMark(i)

		// Create routing table for this WAN
		cfg := &RoutingTableConfig{
			ID:   table,
			Name: wan.Name,
			Default: &TableRoute{
				Destination: "default",
				Gateway:     wan.Gateway,
				Interface:   wan.Interface,
			},
		}

		if err := m.CreateTable(cfg); err != nil {
			return fmt.Errorf("failed to create table for %s: %w", wan.Name, err)
		}

		// Add ip rule to use this table for marked packets
		rule := PolicyRoute{
			Name:     fmt.Sprintf("wan-%s", wan.Name),
			Priority: 100 + i,
			Mark:     mark,
			Table:    table,
			Enabled:  true,
		}

		if err := m.AddRule(rule); err != nil {
			return fmt.Errorf("failed to add rule for %s: %w", wan.Name, err)
		}
	}

	return nil
}

// WANConfig represents a WAN interface configuration for multi-WAN.
type WANConfig struct {
	Name      string
	Interface string
	Gateway   string
	Weight    int // For load balancing
	Priority  int // For failover (lower = preferred)
	Enabled   bool
}

// SetupVPNBypass configures routing to bypass VPN for specific traffic.
func (m *PolicyRoutingManager) SetupVPNBypass(mainGateway, mainInterface string) error {
	// Create bypass table with direct internet route
	cfg := &RoutingTableConfig{
		ID:   TableBypassVPN,
		Name: "bypass-vpn",
		Default: &TableRoute{
			Destination: "default",
			Gateway:     mainGateway,
			Interface:   mainInterface,
		},
	}

	if err := m.CreateTable(cfg); err != nil {
		return err
	}

	// Add rule for bypass mark
	rule := PolicyRoute{
		Name:     "vpn-bypass",
		Priority: 50,
		Mark:     MarkBypassVPN,
		Table:    TableBypassVPN,
		Enabled:  true,
	}

	return m.AddRule(rule)
}

// SetupSourceBasedRouting routes traffic based on source IP.
func (m *PolicyRoutingManager) SetupSourceBasedRouting(source string, table RoutingTable) error {
	rule := PolicyRoute{
		Name:       fmt.Sprintf("src-%s", source),
		Priority:   200,
		FromSource: source,
		Table:      table,
		Enabled:    true,
	}

	return m.AddRule(rule)
}

// MarkForWAN returns the routing mark for a WAN by index (0-based).
func MarkForWAN(index int) RoutingMark {
	if index < 0 || index > 255 {
		return MarkNone
	}
	return MarkWANBase + RoutingMark(index)
}

// MarkForWireGuard returns the routing mark for a WireGuard tunnel by index.
func MarkForWireGuard(index int) RoutingMark {
	if index < 0 || index > 31 {
		return MarkNone
	}
	return MarkWireGuardBase + RoutingMark(index)
}

// MarkForTailscale returns the routing mark for a Tailscale connection by index.
func MarkForTailscale(index int) RoutingMark {
	if index < 0 || index > 31 {
		return MarkNone
	}
	return MarkTailscaleBase + RoutingMark(index)
}

// MarkForOpenVPN returns the routing mark for an OpenVPN tunnel by index.
func MarkForOpenVPN(index int) RoutingMark {
	if index < 0 || index > 31 {
		return MarkNone
	}
	return MarkOpenVPNBase + RoutingMark(index)
}

// MarkForIPsec returns the routing mark for an IPsec tunnel by index.
func MarkForIPsec(index int) RoutingMark {
	if index < 0 || index > 31 {
		return MarkNone
	}
	return MarkIPsecBase + RoutingMark(index)
}

// MarkForZone returns the routing mark for a zone by index.
func MarkForZone(index int) RoutingMark {
	if index < 0 || index > 255 {
		return MarkNone
	}
	return MarkZoneBase + RoutingMark(index)
}

// GetMarkCategory returns the category of a mark.
func GetMarkCategory(mark RoutingMark) RoutingMark {
	return (mark & MarkMaskCategory) >> 8
}

// GetMarkIndex returns the index within a mark's category.
func GetMarkIndex(mark RoutingMark) int {
	return int(mark & MarkMaskIndex)
}

// TableForWAN returns the routing table for a WAN by index (0-based).
func TableForWAN(index int) RoutingTable {
	if index < 0 || index > 19 {
		return TableMain
	}
	return TableWANBase + RoutingTable(index)
}

// TableForWireGuard returns the routing table for a WireGuard tunnel by index.
func TableForWireGuard(index int) RoutingTable {
	if index < 0 || index > 19 {
		return TableMain
	}
	return TableWireGuardBase + RoutingTable(index)
}

// TableForTailscale returns the routing table for a Tailscale connection by index.
func TableForTailscale(index int) RoutingTable {
	if index < 0 || index > 9 {
		return TableMain
	}
	return TableTailscaleBase + RoutingTable(index)
}

// TableForOpenVPN returns the routing table for an OpenVPN tunnel by index.
func TableForOpenVPN(index int) RoutingTable {
	if index < 0 || index > 9 {
		return TableMain
	}
	return TableOpenVPNBase + RoutingTable(index)
}

// GetTableForVPNMark returns the routing table for a VPN mark.
func GetTableForVPNMark(mark RoutingMark) RoutingTable {
	if mark < MarkVPNBase || mark >= MarkZoneBase {
		return TableMain
	}

	// Determine VPN type and index
	switch {
	case mark >= MarkWireGuardBase && mark < MarkTailscaleBase:
		idx := int(mark - MarkWireGuardBase)
		return TableForWireGuard(idx)
	case mark >= MarkTailscaleBase && mark < MarkOpenVPNBase:
		idx := int(mark - MarkTailscaleBase)
		return TableForTailscale(idx)
	case mark >= MarkOpenVPNBase && mark < MarkIPsecBase:
		idx := int(mark - MarkOpenVPNBase)
		return TableForOpenVPN(idx)
	case mark >= MarkIPsecBase && mark < MarkVPNCustomBase:
		idx := int(mark - MarkIPsecBase)
		return TableIPsecBase + RoutingTable(idx)
	default:
		// Custom VPN
		idx := int(mark - MarkVPNCustomBase)
		if idx < 20 {
			return TableVPNCustomBase + RoutingTable(idx)
		}
		return TableMain
	}
}

// GetMarkForInterface returns the routing mark for a specific WAN interface.
func GetMarkForInterface(ifaceName string) RoutingMark {
	switch {
	case strings.HasPrefix(ifaceName, "wan") || ifaceName == "eth0":
		return MarkWAN1
	case ifaceName == "eth1" || strings.Contains(ifaceName, "wan2"):
		return MarkWAN2
	case ifaceName == "eth2" || strings.Contains(ifaceName, "wan3"):
		return MarkWAN3
	case ifaceName == "eth3" || strings.Contains(ifaceName, "wan4"):
		return MarkWAN4
	default:
		return MarkNone
	}
}

// GetTableForMark returns the routing table for a given mark.
func GetTableForMark(mark RoutingMark) RoutingTable {
	switch mark {
	case MarkWAN1:
		return TableWAN1
	case MarkWAN2:
		return TableWAN2
	case MarkWAN3:
		return TableWAN3
	case MarkWAN4:
		return TableWAN4
	case MarkBypassVPN:
		return TableBypassVPN
	case MarkForceVPN:
		return TableVPNBase // Default VPN table
	case MarkLoadBalance:
		return TableLoadBalance
	case MarkFailover:
		return TableFailover
	default:
		// Check if it's a VPN mark and return appropriate table
		if mark >= MarkVPNBase && mark < MarkZoneBase {
			return GetTableForVPNMark(mark)
		}
		// Check if it's a WAN mark
		if mark >= MarkWANBase && mark < MarkVPNBase {
			idx := int(mark - MarkWANBase)
			return TableWANBase + RoutingTable(idx)
		}
		return TableMain
	}
}

// ValidateRoutingMark checks if a mark value is valid.
// Currently all uint32 values are valid, but this provides
// a hook for future validation (e.g., reserved ranges).
func ValidateRoutingMark(mark uint32) error {
	// All uint32 values are valid marks
	// Reserved range check could be added here
	return nil
}

// ParseRoutingMark parses a mark from string (supports hex with 0x prefix).
func ParseRoutingMark(s string) (RoutingMark, error) {
	s = strings.TrimSpace(s)

	var val uint64
	var err error

	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		val, err = strconv.ParseUint(s[2:], 16, 32)
	} else {
		val, err = strconv.ParseUint(s, 10, 32)
	}

	if err != nil {
		return 0, fmt.Errorf("invalid mark value: %w", err)
	}

	return RoutingMark(val), nil
}

// ListRules returns current ip rules.
func (m *PolicyRoutingManager) ListRules() ([]string, error) {
	output, err := m.executor.RunCommand("ip", "rule", "show")
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSpace(output), "\n"), nil
}

// ListTableRoutes returns routes in a specific table.
func (m *PolicyRoutingManager) ListTableRoutes(table RoutingTable) ([]string, error) {
	output, err := m.executor.RunCommand("ip", "route", "show", "table", strconv.Itoa(int(table)))
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSpace(output), "\n"), nil
}

// SyncRules synchronizes the desired rules with the system.
func (m *PolicyRoutingManager) SyncRules() error {
	m.mu.RLock()
	rules := make([]PolicyRoute, len(m.rules))
	copy(rules, m.rules)
	m.mu.RUnlock()

	for _, rule := range rules {
		// Delete existing rule (ignore errors)
		m.DeleteRule(rule)

		// Re-add rule
		if err := m.applyRule(rule); err != nil {
			return fmt.Errorf("failed to apply rule %s: %w", rule.Name, err)
		}
	}

	return nil
}

// Reload applies the full policy routing configuration, replacing existing state.
func (m *PolicyRoutingManager) Reload(tables []config.RoutingTable, rules []config.PolicyRoute) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Clear existing rules
	// We iterate backwards or just flush commonly used priorities?
	// Better: Flush all user rules we know about.
	// Since we track them in m.rules, we can delete them.
	for _, rule := range m.rules {
		m.DeleteRule(rule)
	}
	m.rules = []PolicyRoute{}

	// 2. Clear existing tables (optional, but good for idempotency if we track them)
	// We don't strictly track tables yet fully for deletion, but let's clear internal state
	m.tables = make(map[RoutingTable]*RoutingTableConfig)

	// Note: We don't flush actual kernel tables because they might contain other things?
	// User tables (200+) are usually safe to flush.
	// For now, let's proceed with overwriting/adding which is safer than blind flush.

	// 3. Apply new tables
	for _, tblCfg := range tables {
		// Convert config.RoutingTable to internal RoutingTableConfig
		// Map Routes...
		var internalRoutes []TableRoute
		for _, r := range tblCfg.Routes {
			internalRoutes = append(internalRoutes, TableRoute{
				Destination: r.Destination,
				Gateway:     r.Gateway,
				Interface:   r.Interface,
				Metric:      r.Metric,
				// No MTU/Source in config.Route yet?
				// We'll trust defaults for now.
			})
		}

		// Use helper to create table
		internalCfg := &RoutingTableConfig{
			ID:     RoutingTable(tblCfg.ID),
			Name:   tblCfg.Name,
			Routes: internalRoutes,
		}

		// Save to state
		m.tables[internalCfg.ID] = internalCfg

		// Apply routes (using internal call that doesn't lock again if we were careful,
		// but CreateTable LOCKS. So we must use a non-locking version or release lock.)
		// Refactoring CreateTable to split logic is best, but for now let's just
		// rely on internal calls.
		// Wait, I am holding lock. Calling CreateTable (which locks) will dead-lock.

		// Solution: Do logic inline here or unlock briefly?
		// Inline is safer.

		for _, route := range internalRoutes {
			if err := m.addTableRoute(internalCfg.ID, route); err != nil {
				return fmt.Errorf("failed to add route to table %s: %w", tblCfg.Name, err)
			}
		}
	}

	// 4. Apply new rules
	for _, ruleCfg := range rules {
		// Convert config.PolicyRoute to internal PolicyRoute (struct copy if compatible)
		// Fields look identical.

		priority := ruleCfg.Priority
		if priority == 0 {
			priority = 20000 // Default low priority for user rules
		}

		// Parse mark strings
		mark, _ := ParseRoutingMark(ruleCfg.Mark)
		markMask, _ := ParseRoutingMark(ruleCfg.MarkMask)
		fwmark, _ := ParseRoutingMark(ruleCfg.FWMark)

		internalRule := PolicyRoute{
			Name:          ruleCfg.Name,
			Priority:      priority,
			Mark:          mark,
			MarkMask:      markMask,
			Table:         RoutingTable(ruleCfg.Table),
			FromSource:    ruleCfg.FromSource,
			ToDestination: ruleCfg.To,
			IIF:           ruleCfg.IIF,
			OIF:           ruleCfg.OIF,
			FWMark:        fwmark,
			// Goto:          RoutingTable(ruleCfg.Table), // Removed: misuse of Goto
			Blackhole: ruleCfg.Blackhole,
			Prohibit:  ruleCfg.Prohibit,
			Enabled:   ruleCfg.Enabled,
		}

		if ruleCfg.Table != 0 {
			internalRule.Table = RoutingTable(ruleCfg.Table)
		}

		m.rules = append(m.rules, internalRule)
		if err := m.applyRule(internalRule); err != nil {
			return fmt.Errorf("failed to apply policy rule %s: %w", ruleCfg.Name, err)
		}
	}

	return nil
}

// Ensure net import is used
var _ = net.ParseIP
