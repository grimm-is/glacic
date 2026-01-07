package network

import (
	"testing"
	"time"

	"grimm.is/glacic/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/vishvananda/netlink"
)

func TestUplinkGroup_AddUplink(t *testing.T) {
	g := NewUplinkGroup("test-group", nil)

	u1 := &Uplink{
		Name:      "wan1",
		Type:      UplinkTypeWAN,
		Interface: "eth0",
		Tier:      0,
	}

	g.AddUplink(u1)

	assert.Len(t, g.Uplinks, 1)
	assert.Equal(t, u1, g.Uplinks[0])
	// Should allocate mark/table
	assert.NotEqual(t, MarkNone, u1.Mark)
	assert.NotEqual(t, 0, u1.Table)

	// Check active state
	assert.Len(t, g.ActiveUplinks, 1)
	assert.Equal(t, u1.Mark, g.CurrentMark)

	// Add second uplink
	u2 := &Uplink{
		Name:      "wan2",
		Type:      UplinkTypeWAN,
		Interface: "eth1",
		Tier:      1,
	}
	g.AddUplink(u2)
	assert.Len(t, g.Uplinks, 2)

	// Allocation should be different
	assert.NotEqual(t, u1.Mark, u2.Mark)
	assert.NotEqual(t, u1.Table, u2.Table)
}

func TestUplinkGroup_GetBestUplink(t *testing.T) {
	g := NewUplinkGroup("failover-group", nil)

	wan1 := &Uplink{Name: "wan1", Tier: 0, Enabled: true, Healthy: true, Weight: 100}
	wan2 := &Uplink{Name: "wan2", Tier: 0, Enabled: true, Healthy: true, Weight: 50}
	backup := &Uplink{Name: "backup", Tier: 1, Enabled: true, Healthy: true}

	g.AddUplink(wan1)
	g.AddUplink(wan2)
	g.AddUplink(backup)

	// Default: should pick from Tier 0
	best := g.GetBestUplink()
	assert.NotNil(t, best)
	assert.Equal(t, 0, best.Tier)
	// Default load balance is None, so it picks first healthy?
	// Implementation: "Return first (highest priority within tier)" -> wan1 if ordered?
	// AddUplink appends. So wan1 is first.
	assert.Equal(t, "wan1", best.Name)

	// Fail wan1
	wan1.Healthy = false
	best = g.GetBestUplink()
	assert.Equal(t, "wan2", best.Name)

	// Fail wan2 -> Failover to Tier 1
	wan2.Healthy = false
	best = g.GetBestUplink()
	assert.Equal(t, "backup", best.Name)

	// Recover wan1 -> Failback to Tier 0
	wan1.Healthy = true
	best = g.GetBestUplink()
	assert.Equal(t, "wan1", best.Name)
}

func TestUplinkGroup_LoadBalancing(t *testing.T) {
	g := NewUplinkGroup("lb-group", nil)
	g.LoadBalanceMode = LoadBalanceWeighted

	// selectByWeight picks the uplink with highest weight for deterministic active uplink selection.
	// Traffic distribution is handled by nftables numgen for actual load balancing.
	u1 := &Uplink{Name: "u1", Tier: 0, Enabled: true, Healthy: true, Weight: 10}
	u2 := &Uplink{Name: "u2", Tier: 0, Enabled: true, Healthy: true, Weight: 20}

	g.AddUplink(u1)
	g.AddUplink(u2)

	best := g.GetBestUplink()
	assert.Equal(t, "u2", best.Name) // Highest weight
}

func TestUplinkGroup_LatencySelection(t *testing.T) {
	g := NewUplinkGroup("latency-group", nil)
	g.LoadBalanceMode = LoadBalanceLatency

	u1 := &Uplink{Name: "u1", Tier: 0, Enabled: true, Healthy: true, Latency: 100}
	u2 := &Uplink{Name: "u2", Tier: 0, Enabled: true, Healthy: true, Latency: 50}

	g.AddUplink(u1)
	g.AddUplink(u2)

	best := g.GetBestUplink()
	assert.Equal(t, "u2", best.Name)
}

func TestUplinkGroup_SwitchTo(t *testing.T) {
	// Need to mock executor because SwitchTo runs nft commands
	mockExec := new(MockCommandExecutor)

	g := NewUplinkGroup("switch-group", nil)
	g.executor = mockExec
	g.SourceNetworks = []string{"192.168.1.0/24"}

	u1 := &Uplink{Name: "u1", Mark: 0x100, Tier: 0}
	g.AddUplink(u1) // Takes executor from group? No, AddUplink doesn't use executor.
	// But SwitchTo uses g.executor.

	u2 := &Uplink{Name: "u2", Mark: 0x200, Tier: 1}

	// Expect nft command for updateNewConnectionMark
	// "nft add rule inet firewall mark_prerouting ip saddr ... comment ..."
	mockExec.On("RunCommand", "nft", "add", "rule", "inet", "glacic", "mark_prerouting",
		"ip", "saddr", "192.168.1.0/24",
		"ct", "state", "new",
		"meta", "mark", "set", "0x200",
		"ct", "mark", "set", "meta", "mark",
		"comment", "\"uplink_switch-group_192.168.1.0_24\"").Return("", nil).Once()

	err := g.SwitchTo(u2)
	assert.NoError(t, err)
	assert.Equal(t, u2.Mark, g.CurrentMark)
	assert.Equal(t, 1, g.ActiveTier)

	mockExec.AssertExpectations(t)
}

func TestUplinkManager_Groups(t *testing.T) {
	m := NewUplinkManager()
	g := m.CreateGroup("group1")
	assert.NotNil(t, g)
	assert.Equal(t, "group1", g.Name)

	g2 := m.GetGroup("group1")
	assert.Equal(t, g, g2)

	list := m.ListGroups()
	assert.Contains(t, list, "group1")
}
func TestUplinkHealthChecker_Hysteresis(t *testing.T) {
	mockExec := new(MockCommandExecutor)
	m := NewUplinkManager()
	m.executor = mockExec

	g := m.CreateGroup("test-group")
	u := &Uplink{Name: "u1", Interface: "eth0", Enabled: true, Healthy: true, Tier: 0}
	g.AddUplink(u)
	g.HealthCheck = &config.WANHealth{Threshold: 3, Timeout: 1}

	// Create checker
	h := NewUplinkHealthChecker(m, time.Second, []string{"8.8.8.8"})

	// 1. Fail once (Ping returns error)
	args := []interface{}{"ping", "-c", "1", "-W", "1", "-I", "eth0", "8.8.8.8"}
	mockExec.On("RunCommand", args...).Return("", assert.AnError).Once()
	h.checkGroup(g)
	assert.True(t, u.Healthy, "Should valid be healthy (1/3 failures)")
	assert.Equal(t, 1, u.FailureCount)

	// 2. Fail twice
	mockExec.On("RunCommand", args...).Return("", assert.AnError).Once()
	h.checkGroup(g)
	assert.True(t, u.Healthy, "Should valid be healthy (2/3 failures)")
	assert.Equal(t, 2, u.FailureCount)

	// 3. Fail third time -> Unhealthy
	mockExec.On("RunCommand", args...).Return("", assert.AnError).Once()
	h.checkGroup(g)
	assert.False(t, u.Healthy, "Should be unhealthy (3/3 failures)")

	// 4. Succeed once
	mockExec.On("RunCommand", args...).Return("pong", nil).Once()
	h.checkGroup(g)
	assert.False(t, u.Healthy, "Should valid be unhealthy (1/3 successes)")
	assert.Equal(t, 1, u.SuccessCount)

	// 5. Succeed twice
	mockExec.On("RunCommand", args...).Return("pong", nil).Once()
	h.checkGroup(g)
	assert.False(t, u.Healthy, "Should valid be unhealthy (2/3 successes)")

	// 6. Succeed third time -> Healthy
	mockExec.On("RunCommand", args...).Return("pong", nil).Once()
	h.checkGroup(g)
	assert.True(t, u.Healthy, "Should be healthy (3/3 successes)")
}

func TestUplinkManager_Reload(t *testing.T) {
	mockExec := new(MockCommandExecutor)
	m := NewUplinkManager()

	// Initial group to teardown
	g := m.CreateGroup("old-group")
	g.Uplinks = append(g.Uplinks, &Uplink{Name: "old_u1", Interface: "eth0", Gateway: "1.2.3.4", Table: 100, Tier: 0, Mark: 0x10})
	m.groups["old-group"].executor = mockExec

	// Teardown expectations
	// ip route del default via 1.2.3.4 ...
	mockExec.On("RunCommand", "ip", "route", "del", "default", "via", "1.2.3.4", "dev", "eth0", "table", "100").Return("", nil).Once()
	// ip rule del ...
	mockExec.On("RunCommand", "ip", "rule", "del", "priority", "100", "fwmark", "0x10", "table", "100").Return("", nil).Once()

	// Setup expectations (for new group)
	// We'll skip precise Setup() mocks as Setup() is complex,
	// but we need to ensure Reload calls SetupAll -> Setup.
	// Since Setup calls mocks, if we don't mock them, test fails.
	// Let's rely on MockCommandExecutor default behavior if we can, or just mock expected calls.
	// Actually, easier to use a new mockExec for the NEW group or reused one?
	// New group is created via CreateGroup which sets m.executor.
	// So if we set m.executor = mockExec, new group uses it.
	m.executor = mockExec

	// New config
	newGroups := []config.UplinkGroup{
		{
			Name:    "new-group",
			Enabled: true,
			Uplinks: []config.UplinkDef{
				{
					Name:      "new_u1",
					Interface: "eth1",
					Gateway:   "5.6.7.8",
					Tier:      0,
					Enabled:   true,
				},
			},
		},
	}

	// Expect Setup calls for new uplink
	// Connmark restore (if source interfaces - none here)
	// IP Rule (via prm.AddRule -> ip rule add ...)
	// NewPolicyRoutingManager uses DefaultCommandExecutor.
	// Wait, internal/network/policy_routing.go:NewPolicyRoutingManager() uses DefaultCommandExecutor.
	// We need to swap DefaultCommandExecutor globally or inject it?
	// The `prm` created in `UplinkGroup.Setup` is hardcoded to `NewPolicyRoutingManager()`.
	// Accessing `DefaultCommandExecutor` variable in `manager.go` allows swapping.
	oldExecutor := DefaultCommandExecutor
	DefaultCommandExecutor = mockExec
	defer func() { DefaultCommandExecutor = oldExecutor }()

	// Setup expectations:
	// 1. IP Rule: "ip rule add priority ... fwmark ... table ..."
	mockExec.On("RunCommand", "ip", "rule", "add", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("", nil).Once()
	// 2. Routing Table: "ip route add default via 5.6.7.8 ..."
	mockExec.On("RunCommand", "ip", "route", "add", "default", "via", "5.6.7.8", "dev", "eth1", "table", mock.Anything).Return("", nil).Once()

	err := m.Reload(newGroups)
	assert.NoError(t, err)

	assert.Len(t, m.groups, 1)
	assert.NotNil(t, m.GetGroup("new-group"))
	assert.Nil(t, m.GetGroup("old-group"))
}

func TestUplinkGroup_WeightedBalanceRule(t *testing.T) {
	mockExec := new(MockCommandExecutor)
	g := NewUplinkGroup("weighted-group", nil)
	// Swap global executor
	origExec := DefaultCommandExecutor
	DefaultCommandExecutor = mockExec
	defer func() { DefaultCommandExecutor = origExec }()
	g.executor = mockExec // Redundant if Setup uses Global for PolicyRouting, but safe. Wait, Setup uses NewPolicyRoutingManager which uses DefaultCommandExecutor.

	g.LoadBalanceMode = LoadBalanceWeighted
	g.SourceNetworks = []string{"10.0.0.0/24"}

	u1 := &Uplink{Name: "u1", Mark: 0x10, Weight: 60, Enabled: true, Healthy: true}
	u2 := &Uplink{Name: "u2", Mark: 0x20, Weight: 40, Enabled: true, Healthy: true}
	g.AddUplink(u1)
	g.AddUplink(u2)

	// Map order depends on slice order (AddUplink appends)
	// u1: start=0, end=60. Element: "0-59 : 0x10"
	// u2: start=60, end=100. Element: "60-99 : 0x20"
	expectedMap := "0-59 : 0x10, 60-99 : 0x20"

	// Relaxed matching for nft arguments
	args := make([]interface{}, 30)
	args[0] = "nft"
	for i := 1; i < 30; i++ {
		args[i] = mock.Anything
	}
	args[21] = expectedMap
	mockExec.On("RunCommand", args...).Return("", nil).Once()

	// Expect "ip" commands from PolicyRoutingManager due to Setup()
	// u1: ip rule add ...
	// u2: ip rule add ...
	// Args: "ip" + 10 = 11 args total
	// Use array to be precise and avoid counting errors
	ipArgs := make([]interface{}, 11)
	ipArgs[0] = "ip"
	for i := 1; i < 11; i++ {
		ipArgs[i] = mock.Anything
	}
	mockExec.On("RunCommand", ipArgs...).Return("", nil).Times(2)

	err := g.Setup()
	assert.NoError(t, err)
}

func TestUplinkHealthChecker_AdaptiveWeights(t *testing.T) {
	mockExec := new(MockCommandExecutor)

	// Swap global executor
	origExec := DefaultCommandExecutor
	DefaultCommandExecutor = mockExec
	defer func() { DefaultCommandExecutor = origExec }()

	mockNL := new(MockNetlinker)

	m := NewUplinkManager()
	m.executor = mockExec
	m.netlinker = mockNL

	g := m.CreateGroup("adaptive")
	g.LoadBalanceMode = LoadBalanceAdaptive
	g.SourceNetworks = []string{"10.0.0.0/24"}

	u1 := &Uplink{
		Name:      "u1",
		Interface: "eth0",
		Enabled:   true,
		Healthy:   true,
		Weight:    50,
		RxBytes:   0, TxBytes: 0, // Initial state
	}
	g.AddUplink(u1)

	h := NewUplinkHealthChecker(m, time.Second, []string{"8.8.8.8"})

	// 1. Setup stats mock
	// Simulate 200KB delta in 1 second.
	stats := &netlink.LinkStatistics{RxBytes: 102400, TxBytes: 102400} // 200KB total
	dummyLink := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Statistics: stats}}

	mockNL.On("LinkByName", "eth0").Return(dummyLink, nil)

	// 2. Setup Ping mock
	// Simulate success (Latency ~0 in test, but we rely on stats)
	pingArgs := make([]interface{}, 8)
	pingArgs[0] = "ping"
	for i := 1; i < 8; i++ {
		pingArgs[i] = mock.Anything
	}
	mockExec.On("RunCommand", pingArgs...).Return("pong", nil).Once()

	// 3. Setup NFT update mock
	// Expect updateLoadBalancedMark to be called due to weight change.
	// Initial DynamicWeight=0. New calculated weight will be finite.
	nftArgs := make([]interface{}, 30)
	nftArgs[0] = "nft"
	for i := 1; i < 30; i++ {
		nftArgs[i] = mock.Anything
	}
	mockExec.On("RunCommand", nftArgs...).Return("", nil).Once()

	h.checkGroup(g)

	// Verify Throughput calc
	// 200KB in ~0 time? TimeDelta depends on `h.interval` approximation if `uplink.LastCheck` updated.
	// In `checkGroup`, it assumes intervals.
	// Since verification runs fast, `Throughput` might be calculated against `h.interval` (1s).
	// Throughput = 204800 bytes / 1s = 204800.
	assert.Equal(t, uint64(204800), u1.Throughput)

	// Verify DynamicWeight updated
	// Score = Latency(0) + (200KBps / 100).
	// 200KB = 200.
	// Score = 3.
	// Weight = 10000 / 3 = 3333.
	assert.Equal(t, 3333, u1.DynamicWeight)
}
