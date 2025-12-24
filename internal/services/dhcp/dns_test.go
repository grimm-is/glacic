package dhcp

import (
	"net"
	"testing"

	"grimm.is/glacic/internal/config"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

type MockDNSUpdater struct {
	records map[string]net.IP
}

func (m *MockDNSUpdater) AddRecord(name string, ip net.IP) {
	if m.records == nil {
		m.records = make(map[string]net.IP)
	}
	m.records[name] = ip
}

func (m *MockDNSUpdater) RemoveRecord(name string) {
	delete(m.records, name)
}

func TestHandleRequest_DNSIntegration(t *testing.T) {
	// Setup LeaseStore
	startIP := net.ParseIP("192.168.1.100").To4()
	endIP := net.ParseIP("192.168.1.200").To4()
	routerIP := net.ParseIP("192.168.1.1").To4()

	store := &LeaseStore{
		Leases:       make(map[string]net.IP),
		TakenIPs:     make(map[string]string),
		Reservations: make(map[string]config.DHCPReservation),
		ReservedIPs:  make(map[string]string),
		RangeStart:   startIP,
		RangeEnd:     endIP,
	}

	// Setup Config
	scope := config.DHCPScope{
		Domain: "lan",
	}

	// Setup Mock DNS Updater
	dnsUpdater := &MockDNSUpdater{}

	// Create DHCP Request
	mac, _ := net.ParseMAC("00:11:22:33:44:55")
	req, _ := dhcpv4.NewDiscovery(mac)
	req.UpdateOption(dhcpv4.OptMessageType(dhcpv4.MessageTypeRequest))
	req.UpdateOption(dhcpv4.OptHostName("test-client"))

	// Call handleRequest
	resp, err := handleRequest(req, store, scope, routerIP, dnsUpdater, nil)
	if err != nil {
		t.Fatalf("handleRequest failed: %v", err)
	}

	// Verify IP Allocation
	if resp.YourIPAddr == nil {
		t.Error("Response missing allocated IP")
	}

	// Verify DNS Update
	expectedHostname := "test-client.lan"
	if ip, ok := dnsUpdater.records[expectedHostname]; !ok {
		t.Errorf("DNS record not updated for %s", expectedHostname)
	} else if !ip.Equal(resp.YourIPAddr) {
		t.Errorf("DNS record IP %v does not match allocated IP %v", ip, resp.YourIPAddr)
	}
}

func TestHandleRequest_StaticLeaseDNS(t *testing.T) {
	// Setup LeaseStore with Reservation
	startIP := net.ParseIP("192.168.1.100").To4()
	endIP := net.ParseIP("192.168.1.200").To4()
	routerIP := net.ParseIP("192.168.1.1").To4()

	store := &LeaseStore{
		Leases:       make(map[string]net.IP),
		TakenIPs:     make(map[string]string),
		Reservations: make(map[string]config.DHCPReservation),
		ReservedIPs:  make(map[string]string),
		RangeStart:   startIP,
		RangeEnd:     endIP,
	}

	macStr := "00:11:22:33:44:66"
	mac, _ := net.ParseMAC(macStr)
	staticIP := net.ParseIP("192.168.1.50").To4()
	staticHostname := "static-host"

	store.Reservations[macStr] = config.DHCPReservation{MAC: macStr, IP: staticIP.String(), Hostname: staticHostname}
	store.ReservedIPs[staticIP.String()] = macStr

	// Setup Config with reservation detail
	scope := config.DHCPScope{
		Domain: "lan",
		Reservations: []config.DHCPReservation{
			{MAC: macStr, IP: staticIP.String(), Hostname: staticHostname},
		},
	}

	dnsUpdater := &MockDNSUpdater{}

	// Create Request (client doesn't send hostname)
	req, _ := dhcpv4.NewDiscovery(mac)
	req.UpdateOption(dhcpv4.OptMessageType(dhcpv4.MessageTypeRequest))

	// Call handleRequest
	resp, err := handleRequest(req, store, scope, routerIP, dnsUpdater, nil)
	if err != nil {
		t.Fatalf("handleRequest failed: %v", err)
	}

	// Verify Static IP
	if !resp.YourIPAddr.Equal(staticIP) {
		t.Errorf("Expected static IP %v, got %v", staticIP, resp.YourIPAddr)
	}

	// Verify DNS Update using Configured Hostname
	expectedHostname := "static-host.lan"
	if ip, ok := dnsUpdater.records[expectedHostname]; !ok {
		t.Errorf("DNS record not updated for %s", expectedHostname)
	} else if !ip.Equal(staticIP) {
		t.Errorf("DNS record IP %v does not match static IP %v", ip, staticIP)
	}
}
