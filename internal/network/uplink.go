package network

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"grimm.is/glacic/internal/clock"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
)

// UplinkType identifies the type of uplink.
type UplinkType string

const (
	UplinkTypeWAN       UplinkType = "wan"
	UplinkTypeWireGuard UplinkType = "wireguard"
	UplinkTypeTailscale UplinkType = "tailscale"
	UplinkTypeOpenVPN   UplinkType = "openvpn"
	UplinkTypeIPsec     UplinkType = "ipsec"
	UplinkTypeCustom    UplinkType = "custom"
)

// Uplink represents any network path that can be used for routing traffic.
// This generalizes WANs, VPN tunnels, and any other egress path.
type Uplink struct {
	Name      string       // Unique identifier
	Type      UplinkType   // wan, wireguard, tailscale, etc.
	Interface string       // Network interface
	Gateway   string       // Gateway IP (for WANs)
	LocalIP   string       // Local IP on this link (for SNAT)
	Mark      RoutingMark  // Routing mark for this uplink
	Table     RoutingTable // Routing table for this uplink

	// Tier and weight for failover/load balancing
	Tier   int // Lower tier = higher priority (0 = primary, 1 = secondary, etc.)
	Weight int // Weight within tier for load balancing (1-100)

	Enabled bool

	// Health status
	Healthy      bool
	LastCheck    time.Time
	LastHealthy  time.Time
	Latency      time.Duration
	Jitter       time.Duration
	PacketLoss   float64
	FailureCount int
	SuccessCount int

	// Stats for adaptive balancing
	RxBytes       uint64
	TxBytes       uint64
	Throughput    uint64 // Bytes per second (Rx+Tx)
	DynamicWeight int    // Calculated weight based on metrics

	// Custom health check (optional, overrides default)
	CustomHealthCheck func(*Uplink) bool

	// Metadata
	Tags    map[string]string
	Comment string
}

// UplinkGroup manages a group of uplinks with failover and load balancing.
type UplinkGroup struct {
	Name    string
	Uplinks []*Uplink

	// Source traffic that uses this group
	SourceNetworks   []string // CIDRs
	SourceInterfaces []string // Interface names for connmark restore
	SourceZones      []string // Zone names

	// Failover configuration
	FailoverMode  FailoverMode
	FailbackMode  FailbackMode
	FailoverDelay time.Duration // Wait before failing over
	FailbackDelay time.Duration // Wait before failing back

	// Load balancing configuration
	LoadBalanceMode   LoadBalanceMode
	StickyConnections bool // Keep connections on same uplink

	// Current state
	ActiveTier    int         // Current active tier
	ActiveUplinks []*Uplink   // Currently active uplinks (within tier)
	CurrentMark   RoutingMark // Mark for new connections

	// Switch decision callback (for programmatic "best link" logic)
	// Switch decision callback (for programmatic "best link" logic)
	SwitchDecider SwitchDecider

	// Event callbacks
	OnSwitch       func(from, to *Uplink)
	OnTierChange   func(oldTier, newTier int)
	OnHealthChange func(uplink *Uplink, healthy bool)

	HealthCheck *config.WANHealth

	mu       sync.RWMutex
	executor CommandExecutor // For ip commands (routing)
	nftMgr   NFTManager      // For nftables operations (native netlink)
	logger   *logging.Logger
}

// FailoverMode determines how failover is triggered.
type FailoverMode string

const (
	FailoverImmediate    FailoverMode = "immediate"    // Switch immediately on failure
	FailoverGraceful     FailoverMode = "graceful"     // Wait for failover delay
	FailoverManual       FailoverMode = "manual"       // Only switch manually
	FailoverProgrammatic FailoverMode = "programmatic" // Use SwitchDecider callback
)

// FailbackMode determines how failback to primary is handled.
type FailbackMode string

const (
	FailbackImmediate FailbackMode = "immediate" // Switch back immediately when primary recovers
	FailbackGraceful  FailbackMode = "graceful"  // Wait for failback delay
	FailbackManual    FailbackMode = "manual"    // Only failback manually
	FailbackNever     FailbackMode = "never"     // Stay on current until it fails
)

// LoadBalanceMode determines how traffic is distributed across uplinks.
type LoadBalanceMode string

const (
	LoadBalanceNone       LoadBalanceMode = "none"       // Use single active uplink
	LoadBalanceRoundRobin LoadBalanceMode = "roundrobin" // Rotate through uplinks
	LoadBalanceWeighted   LoadBalanceMode = "weighted"   // Distribute by weight
	LoadBalanceLatency    LoadBalanceMode = "latency"    // Prefer lowest latency
	LoadBalanceRandom     LoadBalanceMode = "random"     // Random selection
	LoadBalanceAdaptive   LoadBalanceMode = "adaptive"   // Adaptive based on latency/throughput
)

// SwitchDecider is a callback for programmatic switch decisions.
// It receives the current group state and returns the uplink to use.
// Return nil to keep current selection.
type SwitchDecider func(group *UplinkGroup, uplinks []*Uplink) *Uplink

// NewUplinkGroup creates a new uplink group.
func NewUplinkGroup(name string, logger *logging.Logger) *UplinkGroup {
	if logger == nil {
		logger = logging.New(logging.DefaultConfig())
	}
	return &UplinkGroup{
		Name:              name,
		Uplinks:           make([]*Uplink, 0),
		FailoverMode:      FailoverGraceful,
		FailbackMode:      FailbackGraceful,
		FailoverDelay:     5 * time.Second,
		FailbackDelay:     30 * time.Second,
		LoadBalanceMode:   LoadBalanceNone,
		StickyConnections: true,
		ActiveTier:        0,
		executor:          DefaultCommandExecutor,
		logger:            logger,
	}
}

// AddUplink adds an uplink to the group.
func (g *UplinkGroup) AddUplink(uplink *Uplink) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Auto-assign mark and table if not set
	if uplink.Mark == MarkNone {
		uplink.Mark = g.allocateMark(uplink)
	}
	if uplink.Table == 0 {
		uplink.Table = g.allocateTable(uplink)
	}

	g.Uplinks = append(g.Uplinks, uplink)

	// Initialize active uplinks if this is the first
	if len(g.Uplinks) == 1 {
		g.ActiveUplinks = []*Uplink{uplink}
		g.CurrentMark = uplink.Mark
		g.ActiveTier = uplink.Tier
	}
}

// allocateMark assigns a routing mark based on uplink type and index.
func (g *UplinkGroup) allocateMark(uplink *Uplink) RoutingMark {
	// Count existing uplinks of same type
	count := 0
	for _, u := range g.Uplinks {
		if u.Type == uplink.Type {
			count++
		}
	}

	switch uplink.Type {
	case UplinkTypeWAN:
		return MarkForWAN(count)
	case UplinkTypeWireGuard:
		return MarkForWireGuard(count)
	case UplinkTypeTailscale:
		return MarkForTailscale(count)
	case UplinkTypeOpenVPN:
		return MarkForOpenVPN(count)
	case UplinkTypeIPsec:
		return MarkForIPsec(count)
	default:
		return MarkUserBase + RoutingMark(len(g.Uplinks))
	}
}

// allocateTable assigns a routing table based on uplink type and index.
func (g *UplinkGroup) allocateTable(uplink *Uplink) RoutingTable {
	count := 0
	for _, u := range g.Uplinks {
		if u.Type == uplink.Type {
			count++
		}
	}

	switch uplink.Type {
	case UplinkTypeWAN:
		return TableForWAN(count)
	case UplinkTypeWireGuard:
		return TableForWireGuard(count)
	case UplinkTypeTailscale:
		return TableForTailscale(count)
	case UplinkTypeOpenVPN:
		return TableForOpenVPN(count)
	default:
		return TableUserBase + RoutingTable(len(g.Uplinks))
	}
}

// GetUplinksInTier returns all uplinks in a specific tier.
func (g *UplinkGroup) GetUplinksInTier(tier int) []*Uplink {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var result []*Uplink
	for _, u := range g.Uplinks {
		if u.Tier == tier && u.Enabled {
			result = append(result, u)
		}
	}
	return result
}

// GetHealthyUplinksInTier returns healthy uplinks in a specific tier.
func (g *UplinkGroup) GetHealthyUplinksInTier(tier int) []*Uplink {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var result []*Uplink
	for _, u := range g.Uplinks {
		if u.Tier == tier && u.Enabled && u.Healthy {
			result = append(result, u)
		}
	}
	return result
}

// GetBestTier returns the lowest tier number that has healthy uplinks.
func (g *UplinkGroup) GetBestTier() int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	bestTier := -1
	for _, u := range g.Uplinks {
		if u.Enabled && u.Healthy {
			if bestTier < 0 || u.Tier < bestTier {
				bestTier = u.Tier
			}
		}
	}
	return bestTier
}

// GetBestUplink returns the best uplink based on current configuration.
func (g *UplinkGroup) GetBestUplink() *Uplink {
	// If we have a programmatic decider, use it
	if g.FailoverMode == FailoverProgrammatic && g.SwitchDecider != nil {
		g.mu.RLock()
		uplinks := make([]*Uplink, len(g.Uplinks))
		copy(uplinks, g.Uplinks)
		g.mu.RUnlock()

		if best := g.SwitchDecider(g, uplinks); best != nil {
			return best
		}
	}

	// Get healthy uplinks in best tier
	bestTier := g.GetBestTier()
	if bestTier < 0 {
		return nil
	}

	healthy := g.GetHealthyUplinksInTier(bestTier)
	if len(healthy) == 0 {
		return nil
	}

	// Select based on load balance mode
	switch g.LoadBalanceMode {
	case LoadBalanceLatency:
		return g.selectByLatency(healthy)
	case LoadBalanceWeighted:
		return g.selectByWeight(healthy)
	default:
		// Return first (highest priority within tier)
		return healthy[0]
	}
}

func (g *UplinkGroup) selectByLatency(uplinks []*Uplink) *Uplink {
	var best *Uplink
	for _, u := range uplinks {
		if best == nil || u.Latency < best.Latency {
			best = u
		}
	}
	return best
}

func (g *UplinkGroup) selectByWeight(uplinks []*Uplink) *Uplink {
	if len(uplinks) == 0 {
		return nil
	}
	if len(uplinks) == 1 {
		return uplinks[0]
	}

	// Deterministic selection: Pick uplink with highest weight
	// This ensures stability for "Active Uplink" concept (e.g. Gateway).
	// Traffic load balancing is handled by nftables numgen.
	var best *Uplink
	maxWeight := -1

	for _, u := range uplinks {
		w := u.Weight
		if g.LoadBalanceMode == LoadBalanceAdaptive && u.DynamicWeight > 0 {
			w = u.DynamicWeight
		}
		if w > maxWeight {
			maxWeight = w
			best = u
		}
	}
	return best
}

// SwitchTo switches new connections to a specific uplink.
// Existing connections continue on their current uplink (via connmark restore).
func (g *UplinkGroup) SwitchTo(uplink *Uplink) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if uplink == nil {
		return fmt.Errorf("uplink is nil")
	}

	oldMark := g.CurrentMark
	oldTier := g.ActiveTier
	newMark := uplink.Mark

	if oldMark == newMark {
		return nil // Already using this uplink
	}

	// Find previous active uplink for callback
	var oldUplink *Uplink
	for _, u := range g.Uplinks {
		if u.Mark == oldMark {
			oldUplink = u
			break
		}
	}

	// Update mark rules for NEW connections
	for _, srcNet := range g.SourceNetworks {
		if err := g.updateNewConnectionMark(srcNet, oldMark, newMark); err != nil {
			return fmt.Errorf("failed to update mark for %s: %w", srcNet, err)
		}
	}

	g.CurrentMark = newMark
	g.ActiveTier = uplink.Tier
	g.ActiveUplinks = []*Uplink{uplink}

	// Fire callbacks
	if g.OnSwitch != nil && oldUplink != nil {
		g.OnSwitch(oldUplink, uplink)
	}
	if g.OnTierChange != nil && oldTier != uplink.Tier {
		g.OnTierChange(oldTier, uplink.Tier)
	}

	return nil
}

// SwitchToBest switches to the best available uplink.
func (g *UplinkGroup) SwitchToBest() error {
	best := g.GetBestUplink()
	if best == nil {
		return fmt.Errorf("no healthy uplinks available")
	}
	return g.SwitchTo(best)
}

// SwitchToTier switches to the best uplink in a specific tier.
func (g *UplinkGroup) SwitchToTier(tier int) error {
	healthy := g.GetHealthyUplinksInTier(tier)
	if len(healthy) == 0 {
		return fmt.Errorf("no healthy uplinks in tier %d", tier)
	}
	return g.SwitchTo(healthy[0])
}

// GetUplinks returns a copy of all uplinks in the group.
func (g *UplinkGroup) GetUplinks() []*Uplink {
	g.mu.RLock()
	defer g.mu.RUnlock()

	uplinks := make([]*Uplink, len(g.Uplinks))
	copy(uplinks, g.Uplinks)
	return uplinks
}

// GetUplink returns a specific uplink by name.
func (g *UplinkGroup) GetUplink(name string) *Uplink {
	g.mu.RLock()
	defer g.mu.RUnlock()

	for _, u := range g.Uplinks {
		if u.Name == name {
			return u
		}
	}
	return nil
}

// GetActiveUplinks returns the currently active uplinks.
func (g *UplinkGroup) GetActiveUplinks() []*Uplink {
	g.mu.RLock()
	defer g.mu.RUnlock()

	uplinks := make([]*Uplink, len(g.ActiveUplinks))
	copy(uplinks, g.ActiveUplinks)
	return uplinks
}

// GetCurrentMark returns the current routing mark.
func (g *UplinkGroup) GetCurrentMark() RoutingMark {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.CurrentMark
}

// GetActiveTier returns the current active tier.
func (g *UplinkGroup) GetActiveTier() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.ActiveTier
}

// SetUplinkEnabled enables or disables an uplink.
func (g *UplinkGroup) SetUplinkEnabled(name string, enabled bool) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, u := range g.Uplinks {
		if u.Name == name {
			u.Enabled = enabled
			return true
		}
	}
	return false
}

// SetUplinkHealth sets the health status of an uplink.
func (g *UplinkGroup) SetUplinkHealth(name string, healthy bool) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, u := range g.Uplinks {
		if u.Name == name {
			u.Healthy = healthy
			u.LastCheck = clock.Now()
			if healthy {
				u.LastHealthy = clock.Now()
			}
			return true
		}
	}
	return false
}

// GetFailoverMode returns the failover mode.
func (g *UplinkGroup) GetFailoverMode() FailoverMode {
	return g.FailoverMode
}

// GetFailbackMode returns the failback mode.
func (g *UplinkGroup) GetFailbackMode() FailbackMode {
	return g.FailbackMode
}

// GetLoadBalanceMode returns the load balance mode.
func (g *UplinkGroup) GetLoadBalanceMode() LoadBalanceMode {
	return g.LoadBalanceMode
}

// GetSourceNetworks returns the source networks.
func (g *UplinkGroup) GetSourceNetworks() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nets := make([]string, len(g.SourceNetworks))
	copy(nets, g.SourceNetworks)
	return nets
}

// GetSourceInterfaces returns the source interfaces.
func (g *UplinkGroup) GetSourceInterfaces() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	ifaces := make([]string, len(g.SourceInterfaces))
	copy(ifaces, g.SourceInterfaces)
	return ifaces
}

// updateNewConnectionMark updates the nftables rule for marking NEW connections.
// It attempts to remove the rule for the old mark before adding the new one.
func (g *UplinkGroup) updateNewConnectionMark(srcNet string, oldMark, newMark RoutingMark) error {
	comment := fmt.Sprintf("uplink_%s_%s", g.Name, strings.ReplaceAll(srcNet, "/", "_"))

	if g.nftMgr != nil {
		// Native manager should handle replacement/update
		// For now, delete then add is safest API usage if 'Update' isn't available
		_ = g.nftMgr.DeleteRulesByComment("mark_prerouting", comment)
		if err := g.nftMgr.AddMarkRule("mark_prerouting", srcNet, "new", uint32(newMark), comment); err != nil {
			return err
		}
		return g.nftMgr.Flush()
	}

	// Fallback to shell command
	// CRITICAL: We must explicitly DELETE the old rule before adding a new one when using the shell fallback.
	// Unlike the native NFT manager which handles atomic replacements or handle-based updates,
	// 'nft add rule' simply appends. If we don't delete, we end up with multiple rules for the
	// same source network, potentially shadowing the new correct mark with the old dead one.
	//
	// 1. Delete old rule (ignore error as it might not exist on first run)
	// Since we are in shell fallback mode (no native NFT manager), we must construct the EXACT command to delete the old rule.
	// NOTE: This assumes the previous rule perfectly matches this construction.
	if oldMark != 0 {
		_, _ = g.executor.RunCommand("nft", "delete", "rule", "inet", "glacic", "mark_prerouting",
			"ip", "saddr", srcNet,
			"ct", "state", "new",
			"meta", "mark", "set", fmt.Sprintf("0x%x", oldMark),
			"ct", "mark", "set", "meta", "mark",
			"comment", fmt.Sprintf(`"%s"`, comment))
	}

	// 2. Add new rule
	// This rule marks NEW connections entering from `srcNet` with the `newMark`.
	// This directs them to the routing table associated with that mark (and thus the specific uplink).
	_, err := g.executor.RunCommand("nft", "add", "rule", "inet", "glacic", "mark_prerouting",
		"ip", "saddr", srcNet,
		"ct", "state", "new",
		"meta", "mark", "set", fmt.Sprintf("0x%x", newMark),
		"ct", "mark", "set", "meta", "mark",
		"comment", fmt.Sprintf(`"%s"`, comment))

	if err != nil {
		logging.Error(fmt.Sprintf("[Uplink] Failed to add mark rule: %v", err))
	} else {
		logging.Info(fmt.Sprintf("[Uplink] Updated mark rule for %s: 0x%x -> 0x%x", srcNet, oldMark, newMark))
	}

	return err
}

// Teardown removes all nftables rules and routes for this uplink group.
func (g *UplinkGroup) Teardown() error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// 1. Remove routing table entries
	for _, uplink := range g.Uplinks {
		if uplink.Gateway != "" {
			// ip route del default via GW table ID
			DefaultCommandExecutor.RunCommand("ip", "route", "del", "default",
				"via", uplink.Gateway,
				"dev", uplink.Interface,
				"table", strconv.Itoa(int(uplink.Table)))
		}

		// Remove SNAT
		if uplink.LocalIP != "" {
			// nft delete rule ... (Need handle? Or match?)
			// Deleting by match is hard in nft without handle.
			// Ideally we use comments to finding handle, then delete.
			// For now, simpler approach: Flush chain? No.
			// We can try to delete by exact match.
			DefaultCommandExecutor.RunCommand("nft", "delete", "rule", "inet", "glacic", "nat_postrouting",
				"meta", "mark", fmt.Sprintf("0x%x", uplink.Mark),
				"oifname", uplink.Interface,
				"snat", "to", uplink.LocalIP)
		}

		// Remove ip rule
		// ip rule del prio ...
		// We need to know priority.
		prio := 100 + uplink.Tier*10
		DefaultCommandExecutor.RunCommand("ip", "rule", "del",
			"priority", strconv.Itoa(prio),
			"fwmark", fmt.Sprintf("0x%x", uplink.Mark),
			"table", strconv.Itoa(int(uplink.Table)))
	}

	// 2. Remove mark rules
	// User rules might leak if we don't clean up.
	// Glacic often flushes everything on apply?
	// ctlplane.Server ApplyConfig does NOT flush nftables entirely? It calls ReloadAll.
	// firewall service ReloadAll re-generates main rules.
	// Uplink manager rules are dynamic.
	// If firewall service flushes ruleset, our rules die.
	// UplinkManager.Setup is called AFTER firewall reload?
	// ctlplane order: 1. network, 2. policyRouting, 3. ReloadAll (services).
	// If Firewall service flushes, Uplink rules die.
	// We should ensure Uplink rules are PERSISTENT or re-applied.
	// Actually, if we use separate chains/tables, manageable.
	// 'mark_prerouting' is likely a main chain.
	// Let's assume for now we try best-effort deletion.

	// Using "delete rule ... comment ..." if supported.
	// Assuming nft supports delete by match.

	// For now, return nil to allow Reload to proceed.
	return nil
}

// Setup configures all nftables rules for this uplink group.
func (g *UplinkGroup) Setup() error {
	g.mu.RLock()
	sourceIfaces := g.SourceInterfaces
	sourceNets := g.SourceNetworks
	uplinks := make([]*Uplink, len(g.Uplinks))
	copy(uplinks, g.Uplinks)
	currentMark := g.CurrentMark
	g.mu.RUnlock()

	// 1. Setup connmark restore for each source interface
	for _, iface := range sourceIfaces {
		if err := g.setupConnmarkRestore(iface); err != nil {
			return fmt.Errorf("failed to setup connmark restore for %s: %w", iface, err)
		}
	}

	// 2. Setup mark rules for each source network
	// Calculate weights if needed
	var weightMap map[*Uplink]int
	if g.LoadBalanceMode == LoadBalanceWeighted || g.LoadBalanceMode == LoadBalanceAdaptive {
		weightMap = make(map[*Uplink]int)
		for _, u := range uplinks {
			w := u.Weight
			if g.LoadBalanceMode == LoadBalanceAdaptive && u.DynamicWeight > 0 {
				w = u.DynamicWeight
			}
			weightMap[u] = w
		}
	}

	for _, srcNet := range sourceNets {
		if len(weightMap) > 1 {
			if err := g.updateLoadBalancedMark(srcNet, weightMap); err != nil {
				return fmt.Errorf("failed to setup load balanced mark for %s: %w", srcNet, err)
			}
		} else {
			if err := g.updateNewConnectionMark(srcNet, 0, currentMark); err != nil {
				return fmt.Errorf("failed to setup mark for %s: %w", srcNet, err)
			}
		}
	}

	// 3. Setup SNAT and routing for each uplink
	prm := NewPolicyRoutingManager()
	for _, uplink := range uplinks {
		// Add ip rule for this mark
		rule := PolicyRoute{
			Name:     fmt.Sprintf("uplink-%s-%s", g.Name, uplink.Name),
			Priority: 100 + uplink.Tier*10,
			Mark:     uplink.Mark,
			Table:    uplink.Table,
			Enabled:  true,
		}
		if err := prm.AddRule(rule); err != nil {
			return fmt.Errorf("failed to add policy route for %s: %w", uplink.Name, err)
		}

		// Setup SNAT if local IP specified
		if uplink.LocalIP != "" {
			if err := g.setupSNAT(uplink); err != nil {
				return fmt.Errorf("failed to setup SNAT for %s: %w", uplink.Name, err)
			}
		}

		// Setup routing table
		if uplink.Gateway != "" {
			if err := g.setupRoutingTable(uplink); err != nil {
				return fmt.Errorf("failed to setup routing table for %s: %w", uplink.Name, err)
			}
		}
	}

	return nil
}

func (g *UplinkGroup) setupConnmarkRestore(iface string) error {
	if g.nftMgr != nil {
		if err := g.nftMgr.AddConnmarkRestore("mark_prerouting", iface); err != nil {
			return err
		}
		return g.nftMgr.Flush()
	}

	// Fallback to shell command
	_, err := g.executor.RunCommand("nft", "insert", "rule", "inet", "glacic", "mark_prerouting",
		"iifname", iface,
		"ct", "state", "established,related",
		"meta", "mark", "set", "ct", "mark")
	return err
}

// UpdateWeights recalculates dynamic weights for adaptive load balancing.
func (g *UplinkGroup) UpdateWeights() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	changed := false

	// Calculate scores
	for _, u := range g.Uplinks {
		if !u.Enabled || !u.Healthy {
			continue
		}

		// Score = Latency (ms) + Throughput factor
		// Latency is Duration (ns). ms = ns / 1e6.
		latencyMs := float64(u.Latency) / 1e6
		if latencyMs < 1 {
			latencyMs = 1
		}

		// Throughput factor: penalize heavy usage.
		// E.g. 1 weight point per 100KB/s?
		// Throughput is bytes/sec. KB/s = / 1024.
		kbps := float64(u.Throughput) / 1024.0

		// Simple formula: Score = LatencyMs + (KBps / 100)
		score := latencyMs + (kbps / 100.0)
		if score < 1 {
			score = 1
		}

		// Weight = 10000 / Score
		// Higher score (bad) -> Lower weight.
		// E.g. Latency 10ms, 0 load -> Score 10 -> Weight 1000.
		// Latency 50ms, 1000KB/s -> Score 50 + 10 = 60 -> Weight 166.
		newWeight := int(10000.0 / score)
		if newWeight < 1 {
			newWeight = 1
		}
		// Cap at 100 or 1000? Uplink.Weight is usually 1-100.
		// Let's normalize to approximate 1-100 scale relative to others?
		// Or usage absolute weights (numgen handles proportion).
		// Let's use absolute.

		// Check for significant change (dampening)
		// Only update if difference > 10%
		diff := newWeight - u.DynamicWeight
		if diff < 0 {
			diff = -diff
		}
		threshold := u.DynamicWeight / 10
		if threshold < 5 {
			threshold = 5
		} // Minimum threshold

		if diff > threshold || u.DynamicWeight == 0 {
			u.DynamicWeight = newWeight
			changed = true
		}
	}
	return changed
}

// updateLoadBalancedMark creates a numgen rule for weighted load balancing.
func (g *UplinkGroup) updateLoadBalancedMark(srcNet string, weights map[*Uplink]int) error {
	comment := fmt.Sprintf("uplink_%s_%s", g.Name, strings.ReplaceAll(srcNet, "/", "_"))

	if g.nftMgr != nil {
		// Build weights slice for native API
		var numgenWeights []NumgenWeight
		for _, u := range g.Uplinks {
			w, ok := weights[u]
			if !ok || w <= 0 {
				continue
			}
			numgenWeights = append(numgenWeights, NumgenWeight{
				Mark:   uint32(u.Mark),
				Weight: w,
			})
		}

		if err := g.nftMgr.AddNumgenMarkRule("mark_prerouting", srcNet, numgenWeights, comment); err != nil {
			return err
		}
		return g.nftMgr.Flush()
	}

	// Fallback to shell command
	var mapElements []string
	totalWeight := 0

	for _, u := range g.Uplinks {
		w, ok := weights[u]
		if !ok || w <= 0 {
			continue
		}

		start := totalWeight
		end := totalWeight + w
		totalWeight += w

		element := fmt.Sprintf("%d-%d : 0x%x", start, end-1, u.Mark)
		mapElements = append(mapElements, element)
	}

	if totalWeight == 0 {
		return fmt.Errorf("total weight is 0")
	}

	mapStr := strings.Join(mapElements, ", ")

	_, err := g.executor.RunCommand("nft", "add", "rule", "inet", "glacic", "mark_prerouting",
		"ip", "saddr", srcNet,
		"ct", "state", "new",
		"meta", "mark", "set", "numgen", "random", "mod", strconv.Itoa(totalWeight), "map", "{", mapStr, "}",
		"ct", "mark", "set", "meta", "mark",
		"comment", fmt.Sprintf("\"%s\"", comment))
	return err
}

func (g *UplinkGroup) setupSNAT(uplink *Uplink) error {
	if g.nftMgr != nil {
		if err := g.nftMgr.AddSNAT("nat_postrouting", uint32(uplink.Mark), uplink.Interface, uplink.LocalIP); err != nil {
			return err
		}
		return g.nftMgr.Flush()
	}

	// Fallback to shell command
	_, err := g.executor.RunCommand("nft", "add", "rule", "inet", "glacic", "nat_postrouting",
		"meta", "mark", fmt.Sprintf("0x%x", uplink.Mark),
		"oifname", uplink.Interface,
		"snat", "to", uplink.LocalIP)
	return err
}

func (g *UplinkGroup) setupRoutingTable(uplink *Uplink) error {
	_, err := g.executor.RunCommand("ip", "route", "add", "default",
		"via", uplink.Gateway,
		"dev", uplink.Interface,
		"table", strconv.Itoa(int(uplink.Table)))
	return err
}

// UplinkManager manages multiple uplink groups.
type UplinkManager struct {
	groups    map[string]*UplinkGroup
	mu        sync.RWMutex
	executor  CommandExecutor
	netlinker Netlinker
	nftMgr    NFTManager // Native nftables manager

	// Global health checker
	healthChecker *UplinkHealthChecker

	// Global callback for all groups
	globalHealthCallback func(uplink *Uplink, healthy bool)
}

// SetHealthCallback sets a global callback for health changes.
func (m *UplinkManager) SetHealthCallback(cb func(uplink *Uplink, healthy bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.globalHealthCallback = cb
	// Update existing groups
	for _, g := range m.groups {
		g.OnHealthChange = cb
	}
}

// NewUplinkManager creates a new uplink manager.
func NewUplinkManager() *UplinkManager {
	return &UplinkManager{
		groups:    make(map[string]*UplinkGroup),
		executor:  DefaultCommandExecutor,
		netlinker: &RealNetlinker{},
	}
}

// CreateGroup creates a new uplink group.
func (m *UplinkManager) CreateGroup(name string) *UplinkGroup {
	m.mu.Lock()
	defer m.mu.Unlock()

	group := NewUplinkGroup(name, nil)
	group.executor = m.executor
	group.nftMgr = m.nftMgr
	m.groups[name] = group
	return group
}

// SetNFTManager sets the native nftables manager for all groups.
func (m *UplinkManager) SetNFTManager(mgr NFTManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nftMgr = mgr
	// Update existing groups
	for _, g := range m.groups {
		g.nftMgr = mgr
	}
}

// GetGroup returns an uplink group by name.
func (m *UplinkManager) GetGroup(name string) *UplinkGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.groups[name]
}

// SetupAll configures all uplink groups.
func (m *UplinkManager) SetupAll() error {
	m.mu.RLock()
	groups := make([]*UplinkGroup, 0, len(m.groups))
	for _, g := range m.groups {
		groups = append(groups, g)
	}
	m.mu.RUnlock()

	for _, group := range groups {
		if err := group.Setup(); err != nil {
			return fmt.Errorf("failed to setup group %s: %w", group.Name, err)
		}
	}
	return nil
}

// Reload reloads the uplink manager with new configuration.
func (m *UplinkManager) Reload(configGroups []config.UplinkGroup) error {
	m.StopHealthChecking()

	m.mu.Lock()
	// Teardown existing groups
	for _, g := range m.groups {
		if err := g.Teardown(); err != nil {
			// Log but continue
			g.logger.Warn("failed to teardown group", "group", g.Name, "error", err)
		}
	}
	m.groups = make(map[string]*UplinkGroup)
	m.mu.Unlock()

	// Create new groups
	for _, cfgGroup := range configGroups {
		if !cfgGroup.Enabled {
			continue
		}

		g := m.CreateGroup(cfgGroup.Name)
		g.HealthCheck = cfgGroup.HealthCheck
		g.OnHealthChange = m.globalHealthCallback // Inherit callback

		// Configure logic modes
		// g.FailoverMode = FailoverMode(cfgGroup.FailoverMode) // Need validation/conversion
		// g.LoadBalanceMode = LoadBalanceMode(cfgGroup.LoadBalanceMode) // Need validation
		// Simplification for now: use defaults or parse string if needed.
		if cfgGroup.FailoverMode != "" {
			g.FailoverMode = FailoverMode(cfgGroup.FailoverMode)
		}
		if cfgGroup.FailbackMode != "" {
			g.FailbackMode = FailbackMode(cfgGroup.FailbackMode)
		}
		if cfgGroup.LoadBalanceMode != "" {
			g.LoadBalanceMode = LoadBalanceMode(cfgGroup.LoadBalanceMode)
		}

		g.SourceNetworks = cfgGroup.SourceNetworks
		g.SourceInterfaces = cfgGroup.SourceInterfaces
		g.SourceZones = cfgGroup.SourceZones

		// Add Uplinks
		for _, cfgUplink := range cfgGroup.Uplinks {
			if !cfgUplink.Enabled {
				continue
			}

			u := &Uplink{
				Name:      cfgUplink.Name,
				Interface: cfgUplink.Interface,
				Gateway:   cfgUplink.Gateway,
				LocalIP:   cfgUplink.LocalIP,
				Tier:      cfgUplink.Tier,
				Weight:    cfgUplink.Weight,
				Enabled:   true,
				Healthy:   true, // Optimistic start
			}
			// Type inference or explicit
			if cfgUplink.Type != "" {
				u.Type = UplinkType(cfgUplink.Type)
			} else {
				u.Type = UplinkTypeWAN // Default
			}

			g.AddUplink(u)
		}
	}

	// Setup all groups
	return m.SetupAll()
}

// StartHealthChecking starts health checking for all groups.
func (m *UplinkManager) StartHealthChecking(interval time.Duration, targets []string) {
	m.healthChecker = NewUplinkHealthChecker(m, interval, targets)
	m.healthChecker.Start()
}

// StopHealthChecking stops health checking.
func (m *UplinkManager) StopHealthChecking() {
	if m.healthChecker != nil {
		m.healthChecker.Stop()
	}
}

// ListGroups returns all uplink group names.
func (m *UplinkManager) ListGroups() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.groups))
	for name := range m.groups {
		names = append(names, name)
	}
	return names
}

// GetAllGroups returns all uplink groups.
func (m *UplinkManager) GetAllGroups() []*UplinkGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()

	groups := make([]*UplinkGroup, 0, len(m.groups))
	for _, g := range m.groups {
		groups = append(groups, g)
	}
	return groups
}

// IsHealthCheckRunning returns whether health checking is active.
func (m *UplinkManager) IsHealthCheckRunning() bool {
	return m.healthChecker != nil
}

// UplinkHealthChecker periodically checks uplink health.
type UplinkHealthChecker struct {
	manager  *UplinkManager
	interval time.Duration
	targets  []string
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewUplinkHealthChecker creates a new health checker.
func NewUplinkHealthChecker(manager *UplinkManager, interval time.Duration, targets []string) *UplinkHealthChecker {
	return &UplinkHealthChecker{
		manager:  manager,
		interval: interval,
		targets:  targets,
		stopCh:   make(chan struct{}),
	}
}

// Start begins health checking.
func (h *UplinkHealthChecker) Start() {
	h.wg.Add(1)
	go h.run()
}

// Stop stops health checking.
func (h *UplinkHealthChecker) Stop() {
	close(h.stopCh)
	h.wg.Wait()
}

func (h *UplinkHealthChecker) run() {
	defer h.wg.Done()

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	// Initial check
	h.checkAll()

	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			h.checkAll()
		}
	}
}

func (h *UplinkHealthChecker) checkAll() {
	h.manager.mu.RLock()
	groups := make([]*UplinkGroup, 0, len(h.manager.groups))
	for _, g := range h.manager.groups {
		groups = append(groups, g)
	}
	h.manager.mu.RUnlock()

	for _, group := range groups {
		h.checkGroup(group)
	}
}

func (h *UplinkHealthChecker) checkGroup(group *UplinkGroup) {
	group.mu.Lock()
	uplinks := make([]*Uplink, len(group.Uplinks))
	copy(uplinks, group.Uplinks)
	currentTier := group.ActiveTier
	failoverMode := group.FailoverMode
	failbackMode := group.FailbackMode
	group.mu.Unlock()

	tierChanged := false
	needsSwitch := false

	for _, uplink := range uplinks {
		wasHealthy := uplink.Healthy
		// Check health (returns isReachable, not isHealthy)
		isReachable := h.checkUplink(uplink, group.HealthCheck)

		threshold := 3 // Default
		if group.HealthCheck != nil && group.HealthCheck.Threshold > 0 {
			threshold = group.HealthCheck.Threshold
		}

		if isReachable {
			uplink.FailureCount = 0
			uplink.SuccessCount++
			if uplink.SuccessCount >= threshold {
				uplink.Healthy = true
			}
		} else {
			uplink.SuccessCount = 0
			uplink.FailureCount++
			if uplink.FailureCount >= threshold {
				uplink.Healthy = false
			}
		}

		uplink.LastCheck = clock.Now()

		if uplink.Healthy && !wasHealthy {
			uplink.LastHealthy = clock.Now()
		}

		// Fire health change callback
		if wasHealthy != uplink.Healthy && group.OnHealthChange != nil {
			group.OnHealthChange(uplink, uplink.Healthy)
		}

		// Check if we need to switch
		if uplink.Tier == currentTier && wasHealthy && !uplink.Healthy {
			// Current tier uplink went down
			needsSwitch = true
		}

		// Collect stats for adaptive balancing
		if group.LoadBalanceMode == LoadBalanceAdaptive && uplink.Interface != "" {
			link, err := h.manager.netlinker.LinkByName(uplink.Interface)
			if err == nil && link.Attrs() != nil && link.Attrs().Statistics != nil {
				stats := link.Attrs().Statistics
				// now := clock.Now()
				// Calculate throughput
				// We need Store LastStatsTime to calc delta.
				// Uplink struct usage:
				// Throughput = (Rx + Tx - OldRx - OldTx) / DeltaTime
				// We verify if this is first run.
				// For simplicity, we store raw Bytes and calculate.
				// We need 'LastCheck' is updated above.
				// But we need Previous Bytes.
				// The Uplink struct stores RxBytes, TxBytes.
				// These are FROM PREVIOUS CHECK?
				// Yes, so we can calculate delta now, update struct after.

				currentRx := stats.RxBytes
				currentTx := stats.TxBytes

				// Avoid huge spikes on overflow/restart (if current < old)
				if currentRx >= uplink.RxBytes && currentTx >= uplink.TxBytes {
					deltaBytes := (currentRx - uplink.RxBytes) + (currentTx - uplink.TxBytes)
					// Interval is typically h.interval.
					// Or use TimeSince(LastCheck).
					// uplink.LastCheck was just updated to Now() above?
					// Wait, line 986 updated LastCheck.
					// So timeDelta is effectively h.interval (approx).
					// But precise calculation needs previous time.
					// Let's assume h.interval seconds.
					seconds := h.interval.Seconds()
					if seconds > 0 {
						uplink.Throughput = uint64(float64(deltaBytes) / seconds)
					}
				}

				uplink.RxBytes = currentRx
				uplink.TxBytes = currentTx
			}
		}
	}

	// Update adaptive weights
	if group.LoadBalanceMode == LoadBalanceAdaptive {
		if group.UpdateWeights() {
			// Weights changed significantly. Update rule.
			// We need to re-apply load balance rules for ALL source networks.
			weightMap := make(map[*Uplink]int)
			for _, u := range group.Uplinks {
				if u.DynamicWeight > 0 {
					weightMap[u] = u.DynamicWeight
				} else {
					weightMap[u] = u.Weight // Fallback
				}
			}

			for _, srcNet := range group.SourceNetworks {
				if len(weightMap) > 1 {
					// Error handling log?
					_ = group.updateLoadBalancedMark(srcNet, weightMap)
				}
			}
		}
	}

	// Check for tier change requirements (Failover or Failback)
	bestTier := group.GetBestTier()
	if bestTier >= 0 && bestTier != currentTier {
		if bestTier < currentTier {
			// Failback: Moving to a higher priority tier
			if failbackMode != FailbackNever {
				tierChanged = true
			}
		} else {
			// Failover: Moving to a lower priority tier
			// This implies the current tier is unhealthy (otherwise it would be the best tier)
			if failoverMode != FailoverManual {
				tierChanged = true
			}
		}
	}

	// Handle failover/failback based on mode
	if failoverMode == FailoverManual && !tierChanged {
		return // Don't auto-switch unless it's a forced tier change
	}

	// Double-check: If we are not changing tier, but current active uplinks are ALL unhealthy,
	// and we have ANY healthy uplink in this tier, we should switch/re-evaluate.
	//
	// CRITICAL: This covers the "All Down -> One Recovers" scenario within the same tier.
	// If the system was in a total blackout (ActiveUplinks empty), 'bestTier' might equal 'currentTier'
	// (e.g. both 0), so 'tierChanged' is false.
	// But we effectively have a "recovery" event that requires re-populating ActiveUplinks/Routes.
	if !tierChanged && !needsSwitch && bestTier == currentTier && bestTier >= 0 {
		hasHealthyActive := false
		group.mu.Lock()
		for _, u := range group.ActiveUplinks {
			if u.Healthy {
				hasHealthyActive = true
				break
			}
		}
		group.mu.Unlock()

		if !hasHealthyActive {
			// We have a healthy uplink in this tier (implied by bestTier == currentTier),
			// but no currently active uplink is healthy. We MUST switch/recover.
			needsSwitch = true
		}
	}

	if needsSwitch || tierChanged {
		// Always try to switch if needed. SwitchToBest handles "no change" efficiently.
		if err := group.SwitchToBest(); err != nil {
			logging.Error(fmt.Sprintf("[Uplink] Failed to switch to best uplink: %v", err))
		}
	}
}

func (h *UplinkHealthChecker) checkUplink(uplink *Uplink, healthCfg *config.WANHealth) bool {
	if !uplink.Enabled {
		return false
	}

	// Use custom health check if provided
	if uplink.CustomHealthCheck != nil {
		return uplink.CustomHealthCheck(uplink)
	}

	// Determine targets and timeout via config or defaults
	targets := h.targets
	timeout := 2 // Seconds

	if healthCfg != nil {
		if len(healthCfg.Targets) > 0 {
			targets = healthCfg.Targets
		}
		if healthCfg.Timeout > 0 {
			timeout = healthCfg.Timeout
		}
	}

	// Default: ping through the interface
	for _, target := range targets {
		start := clock.Now()
		_, err := h.manager.executor.RunCommand("ping",
			"-c", "1",
			"-W", strconv.Itoa(timeout),
			"-I", uplink.Interface,
			target)
		if err == nil {
			uplink.Latency = time.Since(start)
			return true
		}
	}

	return false
}

// Helper function to create common uplink configurations

// NewWANUplink creates a WAN uplink.
func NewWANUplink(name, iface, gateway string, tier int) *Uplink {
	return &Uplink{
		Name:      name,
		Type:      UplinkTypeWAN,
		Interface: iface,
		Gateway:   gateway,
		Tier:      tier,
		Weight:    100,
		Enabled:   true,
		Healthy:   true, // Assume healthy until checked
		Tags:      make(map[string]string),
	}
}

// NewWireGuardUplink creates a WireGuard uplink.
func NewWireGuardUplink(name, iface, localIP string, tier int) *Uplink {
	return &Uplink{
		Name:      name,
		Type:      UplinkTypeWireGuard,
		Interface: iface,
		LocalIP:   localIP,
		Tier:      tier,
		Weight:    100,
		Enabled:   true,
		Healthy:   true,
		Tags:      make(map[string]string),
	}
}

// NewTailscaleUplink creates a Tailscale uplink.
func NewTailscaleUplink(name, iface string, tier int) *Uplink {
	return &Uplink{
		Name:      name,
		Type:      UplinkTypeTailscale,
		Interface: iface,
		Tier:      tier,
		Weight:    100,
		Enabled:   true,
		Healthy:   true,
		Tags:      make(map[string]string),
	}
}
