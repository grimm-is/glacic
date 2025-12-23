package main

import (
	"fmt"
	"os"
	"time"

	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/ctlplane"
	"grimm.is/glacic/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

// MockBackend implements tui.Backend for testing
type MockBackend struct{}

func (m *MockBackend) GetStatus() (*ctlplane.Status, error) {
	return &ctlplane.Status{
		Running:         true,
		Uptime:          "3d 14h 22m",
		ConfigFile:      "/etc/glacic/firewall.hcl",
		FirewallActive:  true,
		FirewallApplied: "2023-10-27 10:00:00",
	}, nil
}

func (m *MockBackend) GetNotifications(lastID int64) ([]ctlplane.Notification, int64, error) {
	return nil, lastID, nil
}

func (m *MockBackend) Fetch(dataSource string) (interface{}, error) {
	// Simulate API endpoints
	switch dataSource {
	case "/api/status":
		return &tui.EnrichedStatus{
			Running:        true,
			Uptime:         "3d 14h 22m",
			FirewallActive: true,
			CPU:            12.5,
			Memory:         45.2,
			Connections:    1247,
		}, nil

	case "/api/interfaces":
		return []map[string]interface{}{
			{"name": "eth0", "zone": "WAN", "state": "up", "ipv4": "203.0.113.5/24", "mac": "00:11:22:33:44:55"},
			{"name": "eth1", "zone": "LAN", "state": "up", "ipv4": "192.168.1.1/24", "mac": "00:11:22:33:44:56"},
		}, nil

	case "/api/services":
		return []map[string]interface{}{
			{"name": "Firewall", "status": "✅ Running", "details": "nftables active"},
			{"name": "DHCP", "status": "✅ Running", "details": "12 active leases"},
		}, nil

	// Learning Page Data
	case "/api/learning/mode":
		return map[string]interface{}{
			"enabled": true,
		}, nil
	case "/api/config/learning/dns":
		return map[string]interface{}{
			"dns_redirect_enabled": true,
			"source_interfaces":    []string{"br-lan", "wlan0"},
		}, nil
	case "/api/flows/stats":
		return map[string]interface{}{
			"pending_flows": 5,
			"allowed_flows": 120,
			"denied_flows":  12,
			"total_flows":   137,
		}, nil
	case "/api/flows?state=pending":
		return []map[string]interface{}{
			{"src_hostname": "unknown-device", "src_mac": "00:11:22:AA:BB:CC", "protocol": "tcp", "dst_port": 443, "domain_hint": "google.com", "state": "pending"},
		}, nil

	// DNS Page Data
	case "/api/config/dns":
		return map[string]interface{}{
			"enabled":          true,
			"local_domain":     "lan.home",
			"dhcp_integration": true,
			"forwarders":       []string{"1.1.1.1", "8.8.8.8"},
		}, nil
	case "/api/config/dns/blocklists":
		return []map[string]interface{}{
			{"name": "AdGuard", "url": "https://adguard.com/list.txt", "entries": 45000, "enabled": true},
			{"name": "Malware", "url": "https://malware.com/list.txt", "entries": 1200, "enabled": true},
		}, nil
	
	// Protection Page
	case "/api/config/protection":
		return map[string]interface{}{
			"anti_spoofing":        true,
			"bogon_filtering":      true,
			"syn_flood_protection": true,
			"syn_flood_rate":       25,
			"icmp_rate_limit":      true,
			"icmp_rate":            10,
			"invalid_packets":      true,
		}, nil

	default:
		return nil, fmt.Errorf("mock data not found for: %s", dataSource)
	}
}

// Submit is a mock implementation that simulates applying config
func (m *MockBackend) Submit(endpoint string, data map[string]interface{}) error {
	// Mock success - in real implementation this would call the control plane
	return nil
}

func main() {
	fmt.Printf("Starting %s TUI Demo...\n", brand.Name)
	fmt.Println("Verifying new components: Card, Form, Tabs, Alert")
	time.Sleep(1 * time.Second) // Give user time to see message

	backend := &MockBackend{}
	// Make sure internal/tui/app.go imports are correct and public
	p := tea.NewProgram(tui.NewModel(backend), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
