//go:build linux
// +build linux

package firewall

import (
	"testing"

	"github.com/google/nftables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNativeIPSetManager_CreateSet(t *testing.T) {
	mockConn := NewMockNFTablesConn()
	table := &nftables.Table{Name: "glacic", Family: nftables.TableFamilyINet}

	// Setup expectations
	mockConn.On("ListTables").Return([]*nftables.Table{table}, nil)
	mockConn.On("AddSet", mock.AnythingOfType("*nftables.Set"), mock.Anything).Return(nil)
	mockConn.On("Flush").Return(nil)

	mgr := NewNativeIPSetManager(mockConn, "glacic")

	// Test valid set creation
	err := mgr.CreateSet("test_set", SetTypeIPv4Addr)
	assert.NoError(t, err)

	// Verify set was created
	mockConn.AssertCalled(t, "AddSet", mock.AnythingOfType("*nftables.Set"), mock.Anything)
	mockConn.AssertCalled(t, "Flush")
}

func TestNativeIPSetManager_CreateSet_InvalidName(t *testing.T) {
	mockConn := NewMockNFTablesConn()
	mgr := NewNativeIPSetManager(mockConn, "glacic")

	// Test invalid names
	invalidNames := []string{
		"set; rm -rf /",
		"set$(whoami)",
		"set|cat /etc/passwd",
		"set space",
		"set/slash",
	}

	for _, name := range invalidNames {
		err := mgr.CreateSet(name, SetTypeIPv4Addr)
		assert.Error(t, err, "should fail for: %s", name)
		assert.Contains(t, err.Error(), "invalid set name")
	}
}

func TestNativeIPSetManager_AddElements(t *testing.T) {
	mockConn := NewMockNFTablesConn()
	table := &nftables.Table{Name: "glacic", Family: nftables.TableFamilyINet}
	set := &nftables.Set{Name: "test_set", Table: table, KeyType: nftables.TypeIPAddr}

	// Pre-populate the mock's internal sets
	mockConn.sets["test_set"] = set

	mockConn.On("ListTables").Return([]*nftables.Table{table}, nil)
	mockConn.On("GetSets", table).Return([]*nftables.Set{set}, nil)
	mockConn.On("SetAddElements", set, mock.Anything).Return(nil)
	mockConn.On("Flush").Return(nil)

	mgr := NewNativeIPSetManager(mockConn, "glacic")

	err := mgr.AddElements("test_set", []string{"1.1.1.1", "8.8.8.8"})
	assert.NoError(t, err)

	mockConn.AssertCalled(t, "SetAddElements", set, mock.Anything)
}

func TestNativeIPSetManager_FlushSet(t *testing.T) {
	mockConn := NewMockNFTablesConn()
	table := &nftables.Table{Name: "glacic", Family: nftables.TableFamilyINet}
	set := &nftables.Set{Name: "test_set", Table: table, KeyType: nftables.TypeIPAddr}

	mockConn.sets["test_set"] = set

	mockConn.On("ListTables").Return([]*nftables.Table{table}, nil)
	mockConn.On("GetSets", table).Return([]*nftables.Set{set}, nil)
	mockConn.On("FlushSet", set)
	mockConn.On("Flush").Return(nil)

	mgr := NewNativeIPSetManager(mockConn, "glacic")

	err := mgr.FlushSet("test_set")
	assert.NoError(t, err)

	mockConn.AssertCalled(t, "FlushSet", set)
}

func TestNativeIPSetManager_ReloadSet(t *testing.T) {
	mockConn := NewMockNFTablesConn()
	table := &nftables.Table{Name: "glacic", Family: nftables.TableFamilyINet}
	set := &nftables.Set{Name: "test_set", Table: table, KeyType: nftables.TypeIPAddr}

	mockConn.sets["test_set"] = set

	mockConn.On("ListTables").Return([]*nftables.Table{table}, nil)
	mockConn.On("GetSets", table).Return([]*nftables.Set{set}, nil)
	mockConn.On("FlushSet", set)
	mockConn.On("SetAddElements", set, mock.Anything).Return(nil)
	mockConn.On("Flush").Return(nil)

	mgr := NewNativeIPSetManager(mockConn, "glacic")

	err := mgr.ReloadSet("test_set", []string{"1.1.1.1", "8.8.8.8"})
	assert.NoError(t, err)

	// Should flush then add
	mockConn.AssertCalled(t, "FlushSet", set)
	mockConn.AssertCalled(t, "SetAddElements", set, mock.Anything)
	mockConn.AssertCalled(t, "Flush")
}

func TestNativeIPSetManager_ReloadSet_InvalidName(t *testing.T) {
	mockConn := NewMockNFTablesConn()
	mgr := NewNativeIPSetManager(mockConn, "glacic")

	err := mgr.ReloadSet("set;reboot", []string{"1.1.1.1"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid set name")
}

func TestNativeIPSetManager_GetSetElements(t *testing.T) {
	mockConn := NewMockNFTablesConn()
	table := &nftables.Table{Name: "glacic", Family: nftables.TableFamilyINet}
	set := &nftables.Set{Name: "test_set", Table: table, KeyType: nftables.TypeIPAddr}

	mockConn.sets["test_set"] = set

	// Set up mock to return elements directly
	expectedElements := []nftables.SetElement{
		{Key: []byte{1, 1, 1, 1}},
		{Key: []byte{8, 8, 8, 8}},
	}

	mockConn.On("ListTables").Return([]*nftables.Table{table}, nil)
	mockConn.On("GetSets", table).Return([]*nftables.Set{set}, nil)
	mockConn.On("GetSetElements", set).Return(expectedElements, nil)

	mgr := NewNativeIPSetManager(mockConn, "glacic")

	elements, err := mgr.GetSetElements("test_set")
	assert.NoError(t, err)
	assert.Len(t, elements, 2)
	assert.Contains(t, elements, "1.1.1.1")
	assert.Contains(t, elements, "8.8.8.8")
}
