package ui

// TopologyPage returns the network topology page.
func TopologyPage() Page {
	return Page{
		ID:          MenuTopology,
		Title:       "Network Topology",
		Description: "Visual map of the network",
		Icon:        "share-2",
		Components: []Component{
			Stats{
				ComponentID: "topology-stats",
				Title:       "Network Size",
				Columns:     3,
				Values: []StatValue{
					{Key: "node_count", Label: "Nodes", Icon: "server", Format: "number"},
					{Key: "link_count", Label: "Links", Icon: "share-2", Format: "number"},
					{Key: "client_count", Label: "Clients", Icon: "users", Format: "number"},
				},
			},
			Table{
				ComponentID: "topology-nodes",
				Title:       "Nodes",
				DataSource:  "/api/topology/nodes",
				Columns: []TableColumn{
					{Key: "name", Label: "Node Name", Width: 20},
					{Key: "ip", Label: "IP Address", Width: 18},
					{Key: "role", Label: "Role", Width: 10},
					{Key: "clients", Label: "Clients", Width: 8},
				},
			},
		},
	}
}

// ClientsPage returns the connected clients page.
func ClientsPage() Page {
	return Page{
		ID:          MenuClients,
		Title:       "Clients",
		Description: "Manage connected devices",
		Icon:        "monitor",
		Components: []Component{
			Stats{
				ComponentID: "clients-stats",
				Title:       "Fleet Overview",
				Columns:     3,
				DataSource:  "/api/leases/stats", // Hypothetical endpoint
				Values: []StatValue{
					{Key: "total_clients", Label: "Total", Icon: "monitor", Format: "number"},
					{Key: "active_clients", Label: "Active (24h)", Icon: "activity", Format: "number"},
					{Key: "wifi_clients", Label: "WiFi", Icon: "wifi", Format: "number"},
				},
			},
			Table{
				ComponentID: "clients-table",
				Title:       "The Fleet",
				DataSource:  "/api/leases",
				Searchable:  true,
				Columns: []TableColumn{
					{Key: "hostname", Label: "Device Name", Width: 25, Sortable: true},
					{Key: "ip", Label: "IP Address", Width: 18},
					{Key: "mac", Label: "MAC Address", Width: 18},
					{Key: "active", Label: "Active", Width: 8},
				},
			},
		},
	}
}

// ScannerPage returns the WiFi scanner page.
func ScannerPage() Page {
	return Page{
		ID:          MenuScanner,
		Title:       "WiFi Scanner",
		Description: "Scan for nearby networks",
		Icon:        "wifi",
		Actions: []Action{
			{ID: "scan", Label: "Scan Now", Icon: "refresh-cw", Shortcut: "s", Variant: "primary"},
		},
		Components: []Component{
			Table{
				ComponentID: "scanner-results",
				Title:       "Nearby Networks",
				DataSource:  "/api/scanner/results",
				Columns: []TableColumn{
					{Key: "ssid", Label: "SSID", Width: 20},
					{Key: "bssid", Label: "BSSID", Width: 18},
					{Key: "signal", Label: "Signal", Width: 10},
					{Key: "channel", Label: "Channel", Width: 8},
					{Key: "security", Label: "Security", Width: 15},
				},
			},
		},
	}
}

// VPNPage returns the VPN configuration page.
func VPNPage() Page {
	return Page{
		ID:          MenuVPN,
		Title:       "VPN",
		Description: "Virtual Private Network",
		Icon:        "lock",
		Components: []Component{
			Table{
				ComponentID: "vpn-peers",
				Title:       "Peers",
				DataSource:  "/api/vpn/peers",
				Columns: []TableColumn{
					{Key: "name", Label: "Name", Width: 15},
					{Key: "public_key", Label: "Public Key", Width: 25, Truncate: true},
					{Key: "endpoint", Label: "Endpoint", Width: 20},
					{Key: "allowed_ips", Label: "Allowed IPs", Width: 20},
					{Key: "handshake", Label: "Last Handshake", Width: 15, Format: "relative"},
				},
			},
		},
	}
}

// AdvancedPage returns the advanced configuration page.
func AdvancedPage() Page {
	return Page{
		ID:          MenuAdvanced,
		Title:       "Advanced Configuration",
		Description: "Direct HCL configuration editing",
		Icon:        "file-code",
		Components: []Component{
			// TUI might just show a message or a simplified view
			Stats{
				ComponentID: "hcl-stats",
				Title:       "Configuration Info",
				Columns:     2,
				Values: []StatValue{
					{Key: "file_size", Label: "File Size", Icon: "file-text", Format: "bytes"},
					{Key: "last_modified", Label: "Last Modified", Icon: "clock", Format: "relative"},
				},
			},
		},
	}
}

// ImportWizardPage returns the import wizard page.
func ImportWizardPage() Page {
	return Page{
		ID:          MenuImport,
		Title:       "Import Wizard",
		Description: "Import configuration from other vendors",
		Icon:        "import",
		Components: []Component{
			Form{
				ComponentID: "import-form",
				Title:       "Import Configuration",
				SubmitLabel: "Upload & Analyze",
				Sections: []FormSection{
					{
						Title: "Upload",
						Fields: []FormField{
							{Key: "vendor", Label: "Vendor", Type: FieldSelect, Options: []SelectOption{
								{Value: "ubiquiti", Label: "Ubiquiti / EdgeOS"},
								{Value: "mikrotik", Label: "MikroTik / RouterOS"},
								{Value: "pfsense", Label: "pfSense / XML"},
							}},
						},
					},
				},
			},
		},
	}
}
