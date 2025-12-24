package dhcp

import (
	"grimm.is/glacic/internal/config"
	"net"
	"testing"
)

func TestLeaseStore_Allocate(t *testing.T) {
	startIP := net.ParseIP("192.168.1.100").To4()
	endIP := net.ParseIP("192.168.1.105").To4()

	store := &LeaseStore{
		Leases:       make(map[string]net.IP),
		TakenIPs:     make(map[string]string),
		Reservations: make(map[string]config.DHCPReservation),
		ReservedIPs:  make(map[string]string),
		RangeStart:   startIP,
		RangeEnd:     endIP,
	}

	// 1. Test Dynamic Allocation
	mac1 := "00:11:22:33:44:01"
	ip1, err := store.Allocate(mac1)
	if err != nil {
		t.Fatalf("Failed to allocate dynamic IP: %v", err)
	}
	if !ip1.Equal(startIP) {
		t.Errorf("Expected first IP %v, got %v", startIP, ip1)
	}

	// 2. Test Existing Allocation (Idempotency)
	ip1Again, err := store.Allocate(mac1)
	if err != nil {
		t.Fatalf("Failed to retrieve existing allocation: %v", err)
	}
	if !ip1Again.Equal(ip1) {
		t.Errorf("Expected same IP %v, got %v", ip1, ip1Again)
	}

	// 3. Test Static Reservation
	macStatic := "00:11:22:33:44:99"
	staticIP := net.ParseIP("192.168.1.50").To4() // Outside dynamic range
	store.Reservations[macStatic] = config.DHCPReservation{MAC: macStatic, IP: "192.168.1.50"}
	store.ReservedIPs[staticIP.String()] = macStatic

	ipStatic, err := store.Allocate(macStatic)
	if err != nil {
		t.Fatalf("Failed to allocate static IP: %v", err)
	}
	if !ipStatic.Equal(staticIP) {
		t.Errorf("Expected static IP %v, got %v", staticIP, ipStatic)
	}

	// 4. Test Reservation Collision Prevention
	// Add a reservation for an IP inside the dynamic pool
	macReserved := "00:11:22:33:44:AA"
	reservedIP := net.ParseIP("192.168.1.101").To4() // Next available dynamic IP
	store.Reservations[macReserved] = config.DHCPReservation{MAC: macReserved, IP: "192.168.1.101"}
	store.ReservedIPs[reservedIP.String()] = macReserved

	// Request dynamic IP for new client
	mac2 := "00:11:22:33:44:02"
	ip2, err := store.Allocate(mac2)
	if err != nil {
		t.Fatalf("Failed to allocate dynamic IP with reservation present: %v", err)
	}
	// Should skip .101 (reserved) and go to .102
	expectedIP2 := net.ParseIP("192.168.1.102").To4()
	if !ip2.Equal(expectedIP2) {
		t.Errorf("Expected IP %v (skipping reserved .101), got %v", expectedIP2, ip2)
	}

	// 5. Test Pool Exhaustion
	// Fill remaining spots: .103, .104, .105
	store.Allocate("00:11:22:33:44:03")
	store.Allocate("00:11:22:33:44:04")
	store.Allocate("00:11:22:33:44:05")

	_, err = store.Allocate("00:11:22:33:44:06")
	if err == nil {
		t.Errorf("Expected error on pool exhaustion, got success")
	}
}
