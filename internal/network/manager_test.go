package network

import (
	"errors"
	"net"
	"testing"

	"grimm.is/glacic/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestSetIPForwarding(t *testing.T) {
	mockSys := new(MockSystemController)
	m := NewManagerWithDeps(nil, mockSys, nil)

	// Test case: enable forwarding successfully
	mockSys.On("WriteSysctl", "/proc/sys/net/ipv4/ip_forward", "1").Return(nil).Once()
	mockSys.On("WriteSysctl", "/proc/sys/net/ipv6/conf/all/forwarding", "1").Return(nil).Once()
	mockSys.On("WriteSysctl", "/proc/sys/net/ipv6/conf/default/forwarding", "1").Return(nil).Once()
	err := m.SetIPForwarding(true)
	assert.NoError(t, err)
	mockSys.AssertExpectations(t)

	// Test case: disable forwarding successfully
	mockSys = new(MockSystemController) // Reset mock
	m = NewManagerWithDeps(nil, mockSys, nil)
	mockSys.On("WriteSysctl", "/proc/sys/net/ipv4/ip_forward", "0").Return(nil).Once()
	mockSys.On("WriteSysctl", "/proc/sys/net/ipv6/conf/all/forwarding", "0").Return(nil).Once()
	mockSys.On("WriteSysctl", "/proc/sys/net/ipv6/conf/default/forwarding", "0").Return(nil).Once()
	err = m.SetIPForwarding(false)
	assert.NoError(t, err)
	mockSys.AssertExpectations(t)

	// Test case: IPv4 forwarding fails
	mockSys = new(MockSystemController) // Reset mock
	m = NewManagerWithDeps(nil, mockSys, nil)
	mockSys.On("WriteSysctl", "/proc/sys/net/ipv4/ip_forward", "1").Return(errors.New("permission denied")).Once()
	mockSys.On("WriteSysctl", "/proc/sys/net/ipv6/conf/all/forwarding", "1").Return(nil).Maybe()
	mockSys.On("WriteSysctl", "/proc/sys/net/ipv6/conf/default/forwarding", "1").Return(nil).Maybe()
	err = m.SetIPForwarding(true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to set net.ipv4.ip_forward")
	mockSys.AssertExpectations(t)
}

func TestSetIPForwardingIPv6(t *testing.T) {
	mockSys := new(MockSystemController)
	m := NewManagerWithDeps(nil, mockSys, nil)

	// Test case: enable IPv6 forwarding successfully
	mockSys.On("WriteSysctl", "/proc/sys/net/ipv4/ip_forward", "1").Return(nil).Once()
	mockSys.On("WriteSysctl", "/proc/sys/net/ipv6/conf/all/forwarding", "1").Return(nil).Once()
	mockSys.On("WriteSysctl", "/proc/sys/net/ipv6/conf/default/forwarding", "1").Return(nil).Once()
	err := m.SetIPForwarding(true)
	assert.NoError(t, err)
	mockSys.AssertExpectations(t)
}

func TestApplyInterfaceIPv6(t *testing.T) {
	mockNetlink := new(MockNetlinker)
	mockSys := new(MockSystemController) // Need mockSys for ApplyInterface usually (sysctls)
	// But ApplyInterface also uses m.sys for forwarding setup if RA/DHCPv6
	m := NewManagerWithDeps(mockNetlink, mockSys, nil)

	eth0Link := &netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 2}}
	mockNetlink.On("LinkByName", "eth0").Return(eth0Link, nil).Once()
	mockNetlink.On("LinkSetDown", eth0Link).Return(nil).Once()
	mockNetlink.On("LinkSetMTU", eth0Link, 1500).Return(nil).Once()
	mockNetlink.On("AddrList", eth0Link, unix.AF_UNSPEC).Return([]netlink.Addr{}, nil).Once()

	// IPv4 Expectations
	expectedAddr4, _ := netlink.ParseAddr("192.168.1.10/24")
	mockNetlink.On("ParseAddr", "192.168.1.10/24").Return(expectedAddr4, nil).Once()
	mockNetlink.On("AddrAdd", eth0Link, expectedAddr4).Return(nil).Once()

	// IPv6 Expectations
	expectedAddr6, _ := netlink.ParseAddr("2001:db8::1/64")
	mockNetlink.On("ParseAddr", "2001:db8::1/64").Return(expectedAddr6, nil).Once()
	mockNetlink.On("AddrAdd", eth0Link, expectedAddr6).Return(nil).Once()

	mockNetlink.On("LinkSetUp", eth0Link).Return(nil).Once()

	cfg := config.Interface{
		Name: "eth0",
		MTU:  1500,
		IPv4: []string{"192.168.1.10/24"},
		IPv6: []string{"2001:db8::1/64"},
		DHCP: false,
	}
	err := m.ApplyInterface(cfg)
	assert.NoError(t, err)
	mockNetlink.AssertExpectations(t)
}

func TestSetupLoopback(t *testing.T) {
	mockNetlink := new(MockNetlinker)
	m := NewManagerWithDeps(mockNetlink, nil, nil)

	// Mock LinkByName to return a dummy link
	dummyLink := &netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "lo", Index: 1}}

	// Test case: loopback setup successfully
	mockNetlink.On("LinkByName", "lo").Return(dummyLink, nil).Once()
	mockNetlink.On("LinkSetUp", dummyLink).Return(nil).Once()
	err := m.SetupLoopback()
	assert.NoError(t, err)
	mockNetlink.AssertExpectations(t)

	// Test case: LinkByName fails
	mockNetlink = new(MockNetlinker)
	m = NewManagerWithDeps(mockNetlink, nil, nil)
	mockNetlink.On("LinkByName", "lo").Return(netlink.Link(nil), errors.New("not found")).Once()
	err = m.SetupLoopback()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get loopback interface")
	mockNetlink.AssertExpectations(t)

	// Test case: LinkSetUp fails
	mockNetlink = new(MockNetlinker)
	m = NewManagerWithDeps(mockNetlink, nil, nil)
	mockNetlink.On("LinkByName", "lo").Return(dummyLink, nil).Once()
	mockNetlink.On("LinkSetUp", dummyLink).Return(errors.New("permission denied")).Once()
	err = m.SetupLoopback()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to bring up loopback interface")
	mockNetlink.AssertExpectations(t)
}

func TestApplyInterface(t *testing.T) {
	t.Run("BasicInterfaceApply", func(t *testing.T) {
		mockNetlink := new(MockNetlinker)
		m := NewManagerWithDeps(mockNetlink, nil, nil)

		eth0Link := &netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 2}}
		mockNetlink.On("LinkByName", "eth0").Return(eth0Link, nil).Once()
		mockNetlink.On("LinkSetDown", eth0Link).Return(nil).Once()
		mockNetlink.On("LinkSetMTU", eth0Link, 1500).Return(nil).Once()
		mockNetlink.On("AddrList", eth0Link, unix.AF_UNSPEC).Return([]netlink.Addr{}, nil).Once()

		expectedAddr, _ := netlink.ParseAddr("192.168.1.10/24")
		mockNetlink.On("ParseAddr", "192.168.1.10/24").Return(expectedAddr, nil).Once()
		mockNetlink.On("AddrAdd", eth0Link, expectedAddr).Return(nil).Once()
		mockNetlink.On("LinkSetUp", eth0Link).Return(nil).Once()

		cfg := config.Interface{
			Name: "eth0",
			MTU:  1500,
			IPv4: []string{"192.168.1.10/24"},
			DHCP: false,
		}
		err := m.ApplyInterface(cfg)
		assert.NoError(t, err)
		mockNetlink.AssertExpectations(t)
	})

	t.Run("InterfaceWithVLAN", func(t *testing.T) {
		mockNetlink := new(MockNetlinker)
		m := NewManagerWithDeps(mockNetlink, nil, nil)

		eth1Link := &netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "eth1", Index: 3}}
		mockNetlink.On("LinkByName", "eth1").Return(eth1Link, nil).Once()
		mockNetlink.On("LinkSetDown", eth1Link).Return(nil).Once()
		mockNetlink.On("AddrList", eth1Link, unix.AF_UNSPEC).Return([]netlink.Addr{}, nil).Once()
		mockNetlink.On("LinkSetUp", eth1Link).Return(nil).Once()

		// Expectations for vlan100
		vlan100Link := &netlink.Vlan{LinkAttrs: netlink.LinkAttrs{Name: "eth1.100", Index: 4, ParentIndex: eth1Link.Attrs().Index}, VlanId: 100}
		mockNetlink.On("LinkByName", "eth1.100").Return(netlink.Link(nil), errors.New("not found")).Once() // Simulate VLAN not existing
		mockNetlink.On("LinkAdd", mock.AnythingOfType("*netlink.Vlan")).Return(nil).Once()
		mockNetlink.On("LinkByName", "eth1.100").Return(vlan100Link, nil).Once() // After LinkAdd, it should exist
		mockNetlink.On("LinkSetUp", vlan100Link).Return(nil).Once()

		vlanAddr, _ := netlink.ParseAddr("10.0.0.1/24")
		mockNetlink.On("ParseAddr", "10.0.0.1/24").Return(vlanAddr, nil).Once()
		mockNetlink.On("AddrAdd", vlan100Link, vlanAddr).Return(nil).Once()

		cfgVLAN := config.Interface{
			Name: "eth1",
			VLANs: []config.VLAN{
				{
					ID:   "100",
					IPv4: []string{"10.0.0.1/24"},
				},
			},
		}
		err := m.ApplyInterface(cfgVLAN)
		assert.NoError(t, err)
		mockNetlink.AssertExpectations(t)
	})

	t.Run("InterfaceWithBond", func(t *testing.T) {
		mockNetlink := new(MockNetlinker)
		m := NewManagerWithDeps(mockNetlink, nil, nil)

		// Test scenario: configure 'bond0' and add 'eth2' as its member.
		// ApplyInterface will be called for "bond0", which is the master interface.
		bond0Name := "bond0"
		eth2Name := "eth2"

		bond0Link := &netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: bond0Name, Index: 6}}
		eth2Link := &netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: eth2Name, Index: 5}}

		// Expectations for bond0 (master interface)
		// Initial lookup of bond0 (returns not found, so it will be created)
		mockNetlink.On("LinkByName", bond0Name).Return(netlink.Link(nil), errors.New("not found")).Once()
		// LinkAdd for bond0 creation
		mockNetlink.On("LinkAdd", mock.AnythingOfType("*netlink.Bond")).Return(nil).Once()
		// Lookup of bond0 after creation
		mockNetlink.On("LinkByName", bond0Name).Return(bond0Link, nil).Once()
		// General interface apply calls for bond0 (master interface)
		mockNetlink.On("LinkSetDown", bond0Link).Return(nil).Once()
		mockNetlink.On("LinkSetMTU", bond0Link, 0).Return(nil).Maybe() // MTU is 0 in config, so it won't be set
		mockNetlink.On("AddrList", bond0Link, unix.AF_UNSPEC).Return([]netlink.Addr{}, nil).Once()
		// No IPs added to bond0 in this config, so no ParseAddr/AddrAdd expected
		mockNetlink.On("LinkSetUp", bond0Link).Return(nil).Once()

		// Expectations for member interface eth2
		mockNetlink.On("LinkByName", eth2Name).Return(eth2Link, nil).Once()
		mockNetlink.On("LinkSetDown", eth2Link).Return(nil).Once()
		mockNetlink.On("LinkSetMaster", eth2Link, bond0Link).Return(nil).Once()

		cfgBond := config.Interface{
			Name: bond0Name, // The bond interface itself
			Bond: &config.Bond{
				Mode:       "active-backup",
				Interfaces: []string{eth2Name}, // Members of the bond
			},
		}
		err := m.ApplyInterface(cfgBond)
		assert.NoError(t, err)
		mockNetlink.AssertExpectations(t)
	})
}

func TestApplyStaticRoutes(t *testing.T) {
	mockNetlink := new(MockNetlinker)
	m := NewManagerWithDeps(mockNetlink, nil, nil)

	// Test case: valid route with gateway
	gwIP := net.ParseIP("192.168.1.1")
	dstIPNet, _ := netlink.ParseIPNet("10.0.0.0/24")
	mockNetlink.On("ParseIPNet", "10.0.0.0/24").Return(dstIPNet, nil).Once()

	expectedRoute := &netlink.Route{
		Dst: dstIPNet,
		Gw:  gwIP,
	}
	mockNetlink.On("RouteAdd", expectedRoute).Return(nil).Once()

	routes := []config.Route{
		{
			Destination: "10.0.0.0/24",
			Gateway:     "192.168.1.1",
		},
	}
	err := m.ApplyStaticRoutes(routes)
	assert.NoError(t, err)
	mockNetlink.AssertExpectations(t)

	// Test case: interface route
	mockNetlink = new(MockNetlinker)
	m = NewManagerWithDeps(mockNetlink, nil, nil)

	link := &netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 10}}
	mockNetlink.On("LinkByName", "eth0").Return(link, nil).Once()

	mockNetlink.On("ParseIPNet", "10.1.0.0/24").Return(dstIPNet, nil).Once() // Recycling dstIPNet for simplicity

	expectedRouteIface := &netlink.Route{
		Dst:       dstIPNet,
		LinkIndex: 10,
	}
	mockNetlink.On("RouteAdd", expectedRouteIface).Return(nil).Once()

	routesIface := []config.Route{
		{
			Destination: "10.1.0.0/24",
			Interface:   "eth0",
		},
	}
	err = m.ApplyStaticRoutes(routesIface)
	assert.NoError(t, err)
	mockNetlink.AssertExpectations(t)

	// Test case: invalid destination (should log warning and continue, returning nil error for entire batch?)
	// Implementation says: continue loop.
	mockNetlink = new(MockNetlinker)
	m = NewManagerWithDeps(mockNetlink, nil, nil)
	mockNetlink.On("ParseIPNet", "invalid").Return((*net.IPNet)(nil), errors.New("invalid CIDR")).Once()

	routesInvalid := []config.Route{{Destination: "invalid"}}
	err = m.ApplyStaticRoutes(routesInvalid)
	assert.NoError(t, err) // Should not return error
	mockNetlink.AssertExpectations(t)

	// Test case: route exists warning
	mockNetlink = new(MockNetlinker)
	m = NewManagerWithDeps(mockNetlink, nil, nil)
	mockNetlink.On("ParseIPNet", "10.0.0.0/24").Return(dstIPNet, nil).Once()
	mockNetlink.On("RouteAdd", mock.Anything).Return(errors.New("file exists")).Once()

	err = m.ApplyStaticRoutes(routes) // Reuse first routes
	assert.NoError(t, err)
}

func TestIsVirtualInterface(t *testing.T) {
	tests := []struct {
		name      string
		isVirtual bool
	}{
		{"eth0", false},
		{"wlan0", false},
		{"veth1234", true},
		{"docker0", true},
		{"br-lan", true},
		{"tun0", true},
		{"tap0", true},
		{"virbr0", true},
		{"vnet0", true},
		{"something_else", false},
	}

	for _, tc := range tests {
		got := isVirtualInterface(tc.name)
		assert.Equal(t, tc.isVirtual, got, "interface %s", tc.name)
	}
}

func TestGetDHCPLeases(t *testing.T) {
	m := NewManager()
	leases := m.GetDHCPLeases()
	assert.NotNil(t, leases)
	assert.Empty(t, leases)
}
