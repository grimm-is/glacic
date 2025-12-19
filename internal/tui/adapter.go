package tui

import (
	"fmt"
	"strings"

	"grimm.is/glacic/internal/ctlplane"
)

// DataAdapter mediates between UI DataSource paths and Control Plane RPC calls
type DataAdapter struct {
	client *ctlplane.Client
}

func NewDataAdapter(client *ctlplane.Client) *DataAdapter {
	return &DataAdapter{client: client}
}

// Fetch retrieves data for a given DataSource URI
// It returns generic interface{} which specific renderers must type assert
func (d *DataAdapter) Fetch(dataSource string) (interface{}, error) {
	// Handle parameterized paths
	if strings.HasPrefix(dataSource, "/api/config/") {
		return d.fetchConfig(dataSource)
	}

	switch dataSource {
	case "/api/status":
		// Return enriched status with system stats for TUI dashboard
		return d.fetchEnrichedStatus()
	case "/api/interfaces":
		return d.client.GetInterfaces()
	case "/api/services":
		return d.client.GetServices()
	case "/api/dhcp/leases":
		return d.client.GetDHCPLeases()
	case "/api/logs":
		return d.client.GetLogs(&ctlplane.GetLogsArgs{Limit: 100})
	case "/api/logs/stats":
		return d.client.GetLogStats()
	default:
		return nil, fmt.Errorf("unknown data source: %s", dataSource)
	}
}

// EnrichedStatus combines Status and SystemStats for TUI display
type EnrichedStatus struct {
	Running         bool    `json:"running"`
	Uptime          string  `json:"uptime"`
	ConfigFile      string  `json:"config_file"`
	FirewallActive  bool    `json:"firewall_active"`
	FirewallApplied string  `json:"firewall_applied,omitempty"`
	CPU             float64 `json:"cpu"`         // CPU usage percentage
	Memory          float64 `json:"memory"`      // Memory usage percentage
	Connections     int     `json:"connections"` // Placeholder - TODO: track connections
}

func (d *DataAdapter) fetchEnrichedStatus() (*EnrichedStatus, error) {
	status, err := d.client.GetStatus()
	if err != nil {
		return nil, err
	}

	enriched := &EnrichedStatus{
		Running:         status.Running,
		Uptime:          status.Uptime,
		ConfigFile:      status.ConfigFile,
		FirewallActive:  status.FirewallActive,
		FirewallApplied: status.FirewallApplied,
	}

	// Fetch system stats and merge
	stats, err := d.client.GetSystemStats()
	if err == nil && stats != nil {
		enriched.CPU = stats.CPUUsage
		if stats.MemoryTotal > 0 {
			enriched.Memory = float64(stats.MemoryUsed) / float64(stats.MemoryTotal) * 100
		}
		// TODO: Get actual connection count from conntrack
		enriched.Connections = 0
	}

	return enriched, nil
}

func (d *DataAdapter) fetchConfig(path string) (interface{}, error) {
	// Fetch full config
	cfg, err := d.client.GetConfig()
	if err != nil {
		return nil, err
	}

	// Extract specific sections based on path
	// path is like /api/config/qos
	parts := strings.Split(strings.TrimPrefix(path, "/api/config/"), "/")
	if len(parts) == 0 {
		return cfg, nil
	}

	section := parts[0]
	switch section {
	case "qos":
		return cfg.QoSPolicies, nil
	case "firewall":
		return cfg.Policies, nil
	case "dhcp":
		if cfg.DHCP != nil && cfg.DHCP.Enabled {
			// Assuming the intent was to return the DHCP config itself,
			// and the 'status' lines were a misunderstanding of context.
			// If DHCP is enabled, return the DHCP configuration.
			return cfg.DHCP, nil
		} else {
			// If DHCP is not enabled or nil, return nil for the DHCP section
			// or the full config as a fallback if no specific section is found.
			// The original code returned cfg.DHCPServer, which is now cfg.DHCP.
			// This path will return the full config if DHCP is not enabled.
			return cfg, nil
		}
	case "dns":
		return cfg.DNSServer, nil
	default:
		// Return full config if specific section mapping missing
		return cfg, nil
	}
}
