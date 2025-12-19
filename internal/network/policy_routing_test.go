package network

import (
	"grimm.is/glacic/internal/config"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseRoutingMark(t *testing.T) {
	tests := []struct {
		input    string
		expected RoutingMark
		hasError bool
	}{
		{"0x100", 0x100, false},
		{"256", 256, false},
		{"0", 0, false},
		{"invalid", 0, true},
	}

	for _, tc := range tests {
		got, err := ParseRoutingMark(tc.input)
		if tc.hasError {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, got)
		}
	}
}

func TestGetTableForMark(t *testing.T) {
	tests := []struct {
		mark     RoutingMark
		expected RoutingTable
	}{
		{MarkWAN1, TableWAN1},
		{MarkWAN2, TableWAN2},
		{MarkBypassVPN, TableBypassVPN},
		{MarkFailover, TableFailover},
		{MarkVPNBase, TableWireGuardBase}, // WireGuard base is 0x200
		{MarkWireGuardBase, TableWireGuardBase},
		{MarkOpenVPNBase, TableOpenVPNBase},
		{MarkUserBase, TableMain}, // Default
	}

	for _, tc := range tests {
		got := GetTableForMark(tc.mark)
		assert.Equal(t, tc.expected, got, "Mark: 0x%x", tc.mark)
	}
}

func TestPolicyRoutingManager_AddRule(t *testing.T) {
	mockExec := new(MockCommandExecutor)
	originalExec := DefaultCommandExecutor
	DefaultCommandExecutor = mockExec // Inject global mock
	defer func() { DefaultCommandExecutor = originalExec }()

	prm := NewPolicyRoutingManager()
	// NewPolicyRoutingManager uses DefaultCommandExecutor

	rule := PolicyRoute{
		Name:     "test-rule",
		Priority: 100,
		Mark:     0x100,
		Table:    TableWAN1,
		Enabled:  true,
	}

	// Expect ip rule add
	mockExec.On("RunCommand", "ip", "rule", "add",
		"priority", "100",
		"from", "all",
		"fwmark", "0x100",
		"table", "10").Return("", nil).Once()

	err := prm.AddRule(rule)
	assert.NoError(t, err)

	mockExec.AssertExpectations(t)
}

func TestPolicyRoutingManager_CreateTable(t *testing.T) {
	mockExec := new(MockCommandExecutor)
	originalExec := DefaultCommandExecutor
	DefaultCommandExecutor = mockExec
	defer func() { DefaultCommandExecutor = originalExec }()

	prm := NewPolicyRoutingManager()

	tableCfg := &RoutingTableConfig{
		ID:   200,
		Name: "custom",
		Routes: []TableRoute{
			{Destination: "192.168.2.0/24", Gateway: "10.0.0.1"},
		},
		Default: &TableRoute{Gateway: "10.0.0.1"},
	}

	// Expect route add for the specific route
	mockExec.On("RunCommand", "ip", "route", "add",
		"192.168.2.0/24", "via", "10.0.0.1", "table", "200").Return("", nil).Once()

	// Expect default route add
	mockExec.On("RunCommand", "ip", "route", "add",
		"default", "via", "10.0.0.1", "table", "200").Return("", nil).Once()

	err := prm.CreateTable(tableCfg)
	assert.NoError(t, err)
	mockExec.AssertExpectations(t)
}

func TestPolicyRoutingManager_FlushRulesByMark(t *testing.T) {
	mockExec := new(MockCommandExecutor)
	originalExec := DefaultCommandExecutor
	DefaultCommandExecutor = mockExec
	defer func() { DefaultCommandExecutor = originalExec }()

	prm := NewPolicyRoutingManager()

	// Mock ip rule show output
	output := `0:	from all lookup local
100:	from all fwmark 0x100 lookup 10
200:	from all fwmark 0x200 lookup 20
`
	mockExec.On("RunCommand", "ip", "rule", "show").Return(output, nil).Once()

	// Expect deleting priority 100 which matches mark 0x100
	mockExec.On("RunCommand", "ip", "rule", "del", "priority", "100").Return("", nil).Once()

	err := prm.FlushRulesByMark(0x100)
	assert.NoError(t, err)
	mockExec.AssertExpectations(t)
}

func TestMarkAllocations(t *testing.T) {
	assert.Equal(t, MarkWAN1, MarkForWAN(0))
	assert.Equal(t, MarkNone, MarkForWAN(-1))
	assert.Equal(t, MarkWireGuardBase, MarkForWireGuard(0))
}

func TestTableAllocations(t *testing.T) {
	assert.Equal(t, TableWAN1, TableForWAN(0))
	assert.Equal(t, TableMain, TableForWAN(-1))
}

func TestPolicyRoutingManager_Reload(t *testing.T) {
	mockExec := new(MockCommandExecutor)
	originalExec := DefaultCommandExecutor
	DefaultCommandExecutor = mockExec
	defer func() { DefaultCommandExecutor = originalExec }()

	prm := NewPolicyRoutingManager()

	// Add an initial rule that should be deleted
	initialRule := PolicyRoute{Name: "old_rule", Priority: 100, Mark: 0x50, Table: 50, Enabled: true}

	// Expectation: Initial rule added
	mockExec.On("RunCommand", "ip", "rule", "add", "priority", "100", "from", "all", "fwmark", "0x50", "table", "50").Return("", nil).Once()

	prm.AddRule(initialRule)

	// Config input
	tables := []config.RoutingTable{
		{
			ID:   100,
			Name: "custom_table",
			Routes: []config.Route{
				{Destination: "1.2.3.4/32", Gateway: "192.168.1.1"},
			},
		},
	}
	rules := []config.PolicyRoute{
		{
			Name:     "new_rule",
			Priority: 200,
			Mark:     "0x100",
			Table:    100,
			Enabled:  true,
		},
	}

	// Expectation: Reload called

	// 1. Delete old rule
	mockExec.On("RunCommand", "ip", "rule", "del", "priority", "100", "fwmark", "0x50", "table", "50").Return("", nil).Once()

	// 2. Add route (table 100)
	mockExec.On("RunCommand", "ip", "route", "add", "1.2.3.4/32", "via", "192.168.1.1", "table", "100").Return("", nil).Once()

	// 3. Add new rule
	mockExec.On("RunCommand", "ip", "rule", "add", "priority", "200", "from", "all", "fwmark", "0x100", "table", "100").Return("", nil).Once()

	err := prm.Reload(tables, rules)
	assert.NoError(t, err)

	// Verify internal state
	prm.mu.RLock() // Lock for reading state
	assert.Len(t, prm.rules, 1)
	if len(prm.rules) > 0 {
		assert.Equal(t, "new_rule", prm.rules[0].Name)
	}
	prm.mu.RUnlock()

	mockExec.AssertExpectations(t)
}
