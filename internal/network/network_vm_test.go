//go:build linux

package network

import (
	"testing"
	"time"

	"github.com/vishvananda/netlink"
)

// useRealNetlink ensures we use real netlink for VM integration tests
func useRealNetlink() {
	DefaultNetlinker = &RealNetlinker{}
	DefaultSystemController = &RealSystemController{}
	DefaultCommandExecutor = &RealCommandExecutor{}
}

func TestNetworkManager_SetupLoopback_Integration(t *testing.T) {
	useRealNetlink()

	// Test that SetupLoopback actually brings up lo interface
	if err := SetupLoopback(); err != nil {
		t.Fatalf("SetupLoopback failed: %v", err)
	}

	// Verify lo is up
	link, err := netlink.LinkByName("lo")
	if err != nil {
		t.Fatalf("Failed to get lo interface: %v", err)
	}

	if link.Attrs().Flags&0x1 == 0 { // IFF_UP
		t.Error("Loopback interface is not up after SetupLoopback")
	}
}

func TestNetworkManager_WaitForLinkIP_Integration(t *testing.T) {
	useRealNetlink()

	// Ensure loopback is up with IP
	if err := SetupLoopback(); err != nil {
		t.Fatalf("SetupLoopback failed: %v", err)
	}

	// lo should already have 127.0.0.1
	err := WaitForLinkIP("lo", 2)
	if err != nil {
		t.Errorf("WaitForLinkIP failed for lo: %v", err)
	}
}

func TestNetworkManager_WaitForLinkIP_Timeout(t *testing.T) {
	useRealNetlink()

	// Test timeout for non-existent interface
	err := WaitForLinkIP("nonexistent_iface_12345", 1)
	if err == nil {
		t.Error("Expected error for non-existent interface, got nil")
	}
}

func TestNetworkManager_GetInterfaceAddrs_Integration(t *testing.T) {
	useRealNetlink()

	// Get lo interface
	link, err := netlink.LinkByName("lo")
	if err != nil {
		t.Skipf("Skipping: failed to get lo: %v", err)
	}

	addrs, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		t.Fatalf("Failed to list addresses: %v", err)
	}

	// lo should have at least 127.0.0.1
	if len(addrs) == 0 {
		t.Error("Expected at least one address on lo")
	}
}

func TestNetworkManager_LinkOperations_Integration(t *testing.T) {
	useRealNetlink()

	// Test basic link listing
	links, err := netlink.LinkList()
	if err != nil {
		t.Fatalf("Failed to list links: %v", err)
	}

	if len(links) == 0 {
		t.Error("Expected at least one network link")
	}

	// Should have lo at minimum
	foundLo := false
	for _, link := range links {
		if link.Attrs().Name == "lo" {
			foundLo = true
			break
		}
	}
	if !foundLo {
		t.Error("lo interface not found in link list")
	}
}

func TestNetworkManager_RouteOperations_Integration(t *testing.T) {
	useRealNetlink()

	// Test route listing
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		t.Fatalf("Failed to list routes: %v", err)
	}

	// In a VM environment, there might be very few routes
	// Just verify the operation doesn't fail
	t.Logf("Found %d IPv4 routes", len(routes))
}

func TestNetworkManager_DHCPLeases_Integration(t *testing.T) {
	useRealNetlink()

	// Test GetDHCPLeases returns valid slice (empty is OK in VM)
	leases := GetDHCPLeases()

	// Basic sanity: should be a valid slice (not nil behavior panic)
	if leases == nil {
		t.Error("GetDHCPLeases returned nil, expected empty slice")
	}

	t.Logf("Found %d DHCP leases", len(leases))

	// If there are leases, verify they have required fields
	for i, lease := range leases {
		if lease.MAC == "" {
			t.Errorf("Lease %d has empty MAC address", i)
		}
		if lease.IP == "" {
			t.Errorf("Lease %d has empty IP address", i)
		}
	}
}

func TestNetworkManager_IPForwarding_Integration(t *testing.T) {
	useRealNetlink()

	// Test enabling IP forwarding
	if err := SetIPForwarding(true); err != nil {
		t.Fatalf("SetIPForwarding(true) failed: %v", err)
	}

	// Test disabling IP forwarding
	if err := SetIPForwarding(false); err != nil {
		t.Fatalf("SetIPForwarding(false) failed: %v", err)
	}
}

func TestNetworkManager_Sysctl_Integration(t *testing.T) {
	useRealNetlink()

	// Test reading a known sysctl
	val, err := ReadSysctl("net.ipv4.ip_forward")
	if err != nil {
		t.Fatalf("ReadSysctl failed: %v", err)
	}
	t.Logf("net.ipv4.ip_forward = %s", val)
}

func TestNetworkManager_InitDHCPClientManager(t *testing.T) {
	useRealNetlink()

	// Test InitializeDHCPClientManager completes without error
	InitializeDHCPClientManager(nil)

	// Give it a moment to initialize
	time.Sleep(100 * time.Millisecond)

	// Verify the global manager is set
	if dhcpClientManager == nil {
		t.Error("DHCP client manager was not initialized")
	}

	// Clean up
	StopDHCPClientManager()

	// Verify cleanup worked
	if dhcpClientManager != nil {
		t.Error("DHCP client manager was not stopped")
	}
}
