package ui

import "grimm.is/glacic/internal/brand"

// Page defines a complete page/view in the UI.
type Page struct {
	ID          MenuID      `json:"id"`
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	Icon        string      `json:"icon,omitempty"`
	Components  []Component `json:"components"`
	Actions     []Action    `json:"actions,omitempty"` // Page-level actions (e.g., "Add New")
}

// PageRegistry holds all page definitions.
// This is the single source of truth for page layouts.
var PageRegistry = map[MenuID]func() Page{
	MenuDashboard:    DashboardPage,
	MenuTopology:     TopologyPage,
	MenuClients:      ClientsPage,
	MenuInterfaces:   InterfacesPage,
	MenuZones:        ZonesPage,
	MenuPolicies:     PoliciesPage,
	MenuNAT:          NATPage,
	MenuProtection:   ProtectionPage,
	MenuIPSets:       IPSetsPage,
	MenuQoS:          QoSPage,
	MenuFlowTriage:   FlowTriagePage,
	MenuLearning:     LearningPage,
	MenuDHCP:         DHCPPage,
	MenuDNS:          DNSPage,
	MenuStaticRoutes: StaticRoutesPage,
	MenuUsers:        UsersPage,
	MenuBackups:      BackupsPage,
	MenuLogs:         LogsPage,
	MenuScanner:      ScannerPage,
	MenuVPN:          VPNPage,
	MenuAdvanced:     AdvancedPage,
	MenuImport:       ImportWizardPage,
}

// GetPage returns the page definition for a menu ID.
func GetPage(id MenuID) *Page {
	if fn, ok := PageRegistry[id]; ok {
		page := fn()
		return &page
	}
	return nil
}

// DashboardPage returns the dashboard page definition.
func DashboardPage() Page {
	return Page{
		ID:          MenuDashboard,
		Title:       "Dashboard",
		Description: "System overview and status",
		Icon:        "home",
		Components: []Component{
			Stats{
				ComponentID: "system-stats",
				Title:       "System Status",
				Columns:     4,
				DataSource:  "/api/status",
				Values: []StatValue{
					{Key: "uptime", Label: "Uptime", Icon: "clock", Format: "duration"},
					{Key: "cpu", Label: "CPU", Icon: "cpu", Format: "percent"},
					{Key: "memory", Label: "Memory", Icon: "hard-drive", Format: "percent"},
					{Key: "connections", Label: "Connections", Icon: "activity", Format: "number"},
				},
			},
			Table{
				ComponentID: "interface-summary",
				Title:       "Interfaces",
				DataSource:  "/api/interfaces",
				Columns: []TableColumn{
					{Key: "name", Label: "Interface", Width: 12},
					{Key: "zone", Label: "Zone", Width: 10},
					{Key: "state", Label: "State", Width: 8},
					{Key: "ipv4", Label: "IPv4 Address", Width: 18},
					{Key: "rx_bytes", Label: "RX", Width: 10, Format: "bytes"},
					{Key: "tx_bytes", Label: "TX", Width: 10, Format: "bytes"},
				},
				Searchable: false,
				Paginated:  false,
			},
			Table{
				ComponentID: "service-status",
				Title:       "Services",
				DataSource:  "/api/services",
				Columns: []TableColumn{
					{Key: "name", Label: "Service", Width: 15},
					{Key: "status", Label: "Status", Width: 10},
					{Key: "details", Label: "Details", Width: 30},
				},
			},
		},
	}
}

// InterfacesPage returns the interfaces page definition.
func InterfacesPage() Page {
	return Page{
		ID:          MenuInterfaces,
		Title:       "Network Interfaces",
		Description: "Configure network interfaces",
		Icon:        "network",
		Actions: []Action{
			{ID: "add-vlan", Label: "Add VLAN", Icon: "plus", Shortcut: "v", Variant: "secondary"},
			{ID: "add-bond", Label: "Add Bond", Icon: "plus", Shortcut: "b", Variant: "secondary"},
		},
		Components: []Component{
			Table{
				ComponentID: "interfaces-table",
				Title:       "Interfaces",
				DataSource:  "/api/interfaces",
				Selectable:  true,
				Searchable:  true,
				Columns: []TableColumn{
					{Key: "name", Label: "Name", Width: 12, Sortable: true},
					{Key: "type", Label: "Type", Width: 10},
					{Key: "zone", Label: "Zone", Width: 10, Sortable: true},
					{Key: "state", Label: "State", Width: 8},
					{Key: "mac", Label: "MAC Address", Width: 18},
					{Key: "ipv4", Label: "IPv4", Width: 18},
					{Key: "ipv6", Label: "IPv6", Width: 24, Hidden: true},
					{Key: "mtu", Label: "MTU", Width: 6},
					{Key: "speed", Label: "Speed", Width: 10},
				},
				Actions: []TableAction{
					{ID: "edit", Label: "Edit", Icon: "edit"},
					{ID: "enable", Label: "Enable", Icon: "play"},
					{ID: "disable", Label: "Disable", Icon: "pause"},
					{ID: "delete", Label: "Delete", Icon: "trash", Destructive: true},
				},
				EmptyText: "No interfaces found",
			},
		},
	}
}

// InterfaceEditForm returns the form for editing an interface.
func InterfaceEditForm() Form {
	return Form{
		ComponentID:  "interface-edit",
		Title:        "Edit Interface",
		SubmitLabel:  "Save",
		CancelLabel:  "Cancel",
		DataSource:   "/api/interfaces/{name}",
		SubmitAction: "/api/interfaces/update",
		Sections: []FormSection{
			{
				Title: "Basic Settings",
				Fields: []FormField{
					{Key: "name", Label: "Interface Name", Type: FieldText, ReadOnly: true},
					{Key: "zone", Label: "Zone", Type: FieldSelect, Options: []SelectOption{
						{Value: "", Label: "None"},
						{Value: "wan", Label: "WAN"},
						{Value: "lan", Label: "LAN"},
						{Value: "dmz", Label: "DMZ"},
						{Value: "guest", Label: "Guest"},
					}},
					{Key: "enabled", Label: "Enabled", Type: FieldToggle, DefaultValue: true},
					{Key: "mtu", Label: "MTU", Type: FieldNumber, Placeholder: "1500",
						Validation: FieldValidation{Min: intPtr(576), Max: intPtr(9000)}},
				},
			},
			{
				Title: "IPv4 Configuration",
				Fields: []FormField{
					{Key: "ipv4_method", Label: "Method", Type: FieldSelect, Options: []SelectOption{
						{Value: "disabled", Label: "Disabled"},
						{Value: "dhcp", Label: "DHCP"},
						{Value: "static", Label: "Static"},
					}},
					{Key: "ipv4_address", Label: "IP Address", Type: FieldCIDR,
						Placeholder: "192.168.1.1/24", Condition: "ipv4_method == 'static'"},
					{Key: "ipv4_gateway", Label: "Gateway", Type: FieldIP,
						Placeholder: "192.168.1.254", Condition: "ipv4_method == 'static'"},
					{Key: "ipv4_dns", Label: "DNS Servers", Type: FieldTags,
						Placeholder: "Add DNS server", Condition: "ipv4_method == 'static'"},
				},
			},
			{
				Title:       "IPv6 Configuration",
				Collapsible: true,
				Collapsed:   true,
				Fields: []FormField{
					{Key: "ipv6_method", Label: "Method", Type: FieldSelect, Options: []SelectOption{
						{Value: "disabled", Label: "Disabled"},
						{Value: "auto", Label: "Auto (SLAAC)"},
						{Value: "dhcpv6", Label: "DHCPv6"},
						{Value: "static", Label: "Static"},
					}},
					{Key: "ipv6_address", Label: "IP Address", Type: FieldCIDR,
						Condition: "ipv6_method == 'static'"},
				},
			},
		},
	}
}

// ZonesPage returns the zones page definition.
func ZonesPage() Page {
	return Page{
		ID:          MenuZones,
		Title:       "Security Zones",
		Description: "Configure security zones",
		Icon:        "layers",
		Actions: []Action{
			{ID: "add-zone", Label: "Add Zone", Icon: "plus", Shortcut: "a", Variant: "primary"},
		},
		Components: []Component{
			Table{
				ComponentID: "zones-table",
				Title:       "Zones",
				DataSource:  "/api/config/zones",
				Columns: []TableColumn{
					{Key: "name", Label: "Zone", Width: 15, Sortable: true},
					{Key: "description", Label: "Description", Width: 30},
					{Key: "interfaces", Label: "Interfaces", Width: 25},
					{Key: "policy_count", Label: "Policies", Width: 10},
				},
				Actions: []TableAction{
					{ID: "edit", Label: "Edit", Icon: "edit"},
					{ID: "delete", Label: "Delete", Icon: "trash", Destructive: true},
				},
			},
		},
	}
}

// PoliciesPage returns the firewall policies page definition.
func PoliciesPage() Page {
	return Page{
		ID:          MenuPolicies,
		Title:       "Firewall Policies",
		Description: "Traffic policies between zones",
		Icon:        "list",
		Actions: []Action{
			{ID: "add-policy", Label: "Add Policy", Icon: "plus", Shortcut: "a", Variant: "primary"},
		},
		Components: []Component{
			Table{
				ComponentID: "policies-table",
				Title:       "Policies",
				DataSource:  "/api/config/policies",
				Selectable:  true,
				Searchable:  true,
				Columns: []TableColumn{
					{Key: "order", Label: "#", Width: 4},
					{Key: "name", Label: "Name", Width: 20, Sortable: true},
					{Key: "from", Label: "From", Width: 10},
					{Key: "to", Label: "To", Width: 10},
					{Key: "default_action", Label: "Default", Width: 10},
					{Key: "rule_count", Label: "Rules", Width: 8},
					{Key: "enabled", Label: "Enabled", Width: 8},
				},
				Actions: []TableAction{
					{ID: "edit", Label: "Edit", Icon: "edit", Shortcut: "e"},
					{ID: "rules", Label: "Rules", Icon: "list", Shortcut: "r"},
					{ID: "enable", Label: "Enable", Icon: "play", Shortcut: "n"},
					{ID: "disable", Label: "Disable", Icon: "pause", Shortcut: "x"},
					{ID: "delete", Label: "Delete", Icon: "trash", Shortcut: "d", Destructive: true},
				},
			},
		},
	}
}

// NATPage returns the NAT configuration page.
func NATPage() Page {
	return Page{
		ID:          MenuNAT,
		Title:       "NAT Rules",
		Description: "Network Address Translation",
		Icon:        "shuffle",
		Actions: []Action{
			{ID: "add-snat", Label: "Add SNAT", Icon: "plus", Shortcut: "s", Variant: "secondary"},
			{ID: "add-dnat", Label: "Add DNAT/Port Forward", Icon: "plus", Shortcut: "d", Variant: "primary"},
		},
		Components: []Component{
			Table{
				ComponentID: "nat-table",
				Title:       "NAT Rules",
				DataSource:  "/api/config/nat",
				Columns: []TableColumn{
					{Key: "order", Label: "#", Width: 4},
					{Key: "name", Label: "Name", Width: 20},
					{Key: "type", Label: "Type", Width: 12},
					{Key: "source", Label: "Source", Width: 18},
					{Key: "destination", Label: "Destination", Width: 18},
					{Key: "translate_to", Label: "Translate To", Width: 18},
					{Key: "enabled", Label: "Enabled", Width: 8},
				},
				Actions: []TableAction{
					{ID: "edit", Label: "Edit", Icon: "edit"},
					{ID: "delete", Label: "Delete", Icon: "trash", Destructive: true},
				},
			},
		},
	}
}

// ProtectionPage returns the protection settings page.
func ProtectionPage() Page {
	return Page{
		ID:          MenuProtection,
		Title:       "Protection Settings",
		Description: "DDoS and attack protection",
		Icon:        "shield-check",
		Components: []Component{
			Form{
				ComponentID:  "protection-form",
				Title:        "Protection Settings",
				DataSource:   "/api/config/protection",
				SubmitAction: "/api/config/protection",
				SubmitLabel:  "Save",
				Sections: []FormSection{
					{
						Title: "Anti-Spoofing",
						Fields: []FormField{
							{Key: "anti_spoofing", Label: "Enable Anti-Spoofing", Type: FieldToggle,
								HelpText: "Block packets with spoofed source addresses"},
							{Key: "bogon_filtering", Label: "Bogon Filtering", Type: FieldToggle,
								HelpText: "Block packets from reserved/invalid IP ranges"},
						},
					},
					{
						Title: "Flood Protection",
						Fields: []FormField{
							{Key: "syn_flood_protection", Label: "SYN Flood Protection", Type: FieldToggle},
							{Key: "syn_flood_rate", Label: "SYN Rate Limit", Type: FieldNumber,
								Placeholder: "25", HelpText: "Packets per second", Condition: "syn_flood_protection"},
							{Key: "syn_flood_burst", Label: "SYN Burst Limit", Type: FieldNumber,
								Placeholder: "50", Condition: "syn_flood_protection"},
							{Key: "icmp_rate_limit", Label: "ICMP Rate Limit", Type: FieldToggle},
							{Key: "icmp_rate", Label: "ICMP Rate", Type: FieldNumber,
								Placeholder: "10", Condition: "icmp_rate_limit"},
						},
					},
					{
						Title: "Invalid Packets",
						Fields: []FormField{
							{Key: "invalid_packets", Label: "Drop Invalid Packets", Type: FieldToggle,
								HelpText: "Drop packets in INVALID connection state"},
						},
					},
				},
			},
		},
	}
}

// IPSetsPage returns the IP sets page.
func IPSetsPage() Page {
	return Page{
		ID:          MenuIPSets,
		Title:       "IP Sets",
		Description: "IP address lists and blocklists",
		Icon:        "database",
		Actions: []Action{
			{ID: "add-ipset", Label: "Add IP Set", Icon: "plus", Shortcut: "a", Variant: "primary"},
			{ID: "refresh-all", Label: "Refresh All", Icon: "refresh-cw", Shortcut: "r", Variant: "secondary"},
		},
		Components: []Component{
			Table{
				ComponentID: "ipsets-table",
				Title:       "IP Sets",
				DataSource:  "/api/config/ipsets",
				Columns: []TableColumn{
					{Key: "name", Label: "Name", Width: 20, Sortable: true},
					{Key: "type", Label: "Type", Width: 12},
					{Key: "source", Label: "Source", Width: 25},
					{Key: "entries", Label: "Entries", Width: 10},
					{Key: "last_updated", Label: "Last Updated", Width: 18, Format: "date"},
					{Key: "auto_update", Label: "Auto Update", Width: 10},
				},
				Actions: []TableAction{
					{ID: "edit", Label: "Edit", Icon: "edit"},
					{ID: "view", Label: "View Entries", Icon: "eye"},
					{ID: "refresh", Label: "Refresh", Icon: "refresh-cw"},
					{ID: "delete", Label: "Delete", Icon: "trash", Destructive: true},
				},
			},
		},
	}
}

// QoSPage returns the QoS configuration page.
func QoSPage() Page {
	return Page{
		ID:          MenuQoS,
		Title:       "Quality of Service",
		Description: "Traffic shaping and prioritization",
		Icon:        "gauge",
		Components: []Component{
			Form{
				ComponentID:  "qos-form",
				Title:        "QoS Settings",
				DataSource:   "/api/config/qos",
				SubmitAction: "/api/config/qos",
				SubmitLabel:  "Save",
				Sections: []FormSection{
					{
						Title: "General",
						Fields: []FormField{
							{Key: "enabled", Label: "Enable QoS", Type: FieldToggle},
							{Key: "profile", Label: "Profile", Type: FieldSelect, Options: []SelectOption{
								{Value: "custom", Label: "Custom"},
								{Value: "gaming", Label: "Gaming (Low Latency)"},
								{Value: "voip", Label: "VoIP (Voice Priority)"},
								{Value: "balanced", Label: "Balanced"},
							}},
						},
					},
				},
			},
		},
	}
}

// DHCPPage returns the DHCP configuration page.
func DHCPPage() Page {
	return Page{
		ID:          MenuDHCP,
		Title:       "DHCP Server",
		Description: "DHCP server configuration",
		Icon:        "wifi",
		Actions: []Action{
			{ID: "add-scope", Label: "Add Scope", Icon: "plus", Shortcut: "a", Variant: "primary"},
		},
		Components: []Component{
			Table{
				ComponentID: "dhcp-scopes",
				Title:       "DHCP Scopes",
				DataSource:  "/api/config/dhcp",
				Columns: []TableColumn{
					{Key: "name", Label: "Scope", Width: 15},
					{Key: "interface", Label: "Interface", Width: 12},
					{Key: "range", Label: "Range", Width: 25},
					{Key: "lease_time", Label: "Lease Time", Width: 12},
					{Key: "active_leases", Label: "Active", Width: 8},
					{Key: "enabled", Label: "Enabled", Width: 8},
				},
				Actions: []TableAction{
					{ID: "edit", Label: "Edit", Icon: "edit"},
					{ID: "leases", Label: "View Leases", Icon: "list"},
					{ID: "delete", Label: "Delete", Icon: "trash", Destructive: true},
				},
			},
			Table{
				ComponentID: "dhcp-reservations",
				Title:       "Static Reservations",
				DataSource:  "/api/config/dhcp/reservations",
				Columns: []TableColumn{
					{Key: "hostname", Label: "Hostname", Width: 20},
					{Key: "mac", Label: "MAC Address", Width: 18},
					{Key: "ip", Label: "IP Address", Width: 15},
					{Key: "scope", Label: "Scope", Width: 12},
				},
				Actions: []TableAction{
					{ID: "edit", Label: "Edit", Icon: "edit"},
					{ID: "delete", Label: "Delete", Icon: "trash", Destructive: true},
				},
			},
		},
	}
}

// DNSPage returns the DNS configuration page.
func DNSPage() Page {
	return Page{
		ID:          MenuDNS,
		Title:       "DNS Server",
		Description: "DNS server and filtering",
		Icon:        "globe",
		Components: []Component{
			Tabs{
				ComponentID: "dns-tabs",
				DefaultTab:  "general",
				Tabs: []Tab{
					{
						ID:    "general",
						Label: "General",
						Content: []Component{
							Form{
								ComponentID:  "dns-general",
								DataSource:   "/api/config/dns",
								SubmitAction: "/api/config/dns",
								SubmitLabel:  "Save",
								Sections: []FormSection{
									{
										Fields: []FormField{
											{Key: "enabled", Label: "Enable DNS Server", Type: FieldToggle},
											{Key: "local_domain", Label: "Local Domain", Type: FieldText, Placeholder: "lan"},
											{Key: "dhcp_integration", Label: "DHCP Integration", Type: FieldToggle,
												HelpText: "Auto-register DHCP hostnames"},
										},
									},
								},
							},
						},
					},
					{
						ID:    "upstream",
						Label: "Upstream",
						Content: []Component{
							Form{
								ComponentID:  "dns-upstream",
								DataSource:   "/api/config/dns",
								SubmitAction: "/api/config/dns",
								SubmitLabel:  "Save",
								Sections: []FormSection{
									{
										Title: "Forwarders",
										Fields: []FormField{
											{Key: "forwarders", Label: "DNS Servers", Type: FieldTags,
												Placeholder: "Add DNS server (e.g., 1.1.1.1)"},
										},
									},
								},
							},
						},
					},
					{
						ID:    "filtering",
						Label: "Filtering",
						Content: []Component{
							Table{
								ComponentID: "dns-blocklists",
								Title:       "Blocklists",
								DataSource:  "/api/config/dns/blocklists",
								Columns: []TableColumn{
									{Key: "name", Label: "Name", Width: 20},
									{Key: "url", Label: "URL", Width: 40},
									{Key: "entries", Label: "Entries", Width: 10},
									{Key: "enabled", Label: "Enabled", Width: 8},
								},
							},
						},
					},
				},
			},
		},
	}
}

// StaticRoutesPage returns the static routes page.
func StaticRoutesPage() Page {
	return Page{
		ID:          MenuStaticRoutes,
		Title:       "Static Routes",
		Description: "Static route configuration",
		Icon:        "map",
		Actions: []Action{
			{ID: "add-route", Label: "Add Route", Icon: "plus", Shortcut: "a", Variant: "primary"},
		},
		Components: []Component{
			Table{
				ComponentID: "routes-table",
				Title:       "Static Routes",
				DataSource:  "/api/config/routes",
				Columns: []TableColumn{
					{Key: "destination", Label: "Destination", Width: 20},
					{Key: "gateway", Label: "Gateway", Width: 15},
					{Key: "interface", Label: "Interface", Width: 12},
					{Key: "metric", Label: "Metric", Width: 8},
					{Key: "enabled", Label: "Enabled", Width: 8},
				},
				Actions: []TableAction{
					{ID: "edit", Label: "Edit", Icon: "edit"},
					{ID: "delete", Label: "Delete", Icon: "trash", Destructive: true},
				},
			},
		},
	}
}

// UsersPage returns the users management page.
func UsersPage() Page {
	return Page{
		ID:          MenuUsers,
		Title:       "User Management",
		Description: "Manage user accounts",
		Icon:        "users",
		Actions: []Action{
			{ID: "add-user", Label: "Add User", Icon: "plus", Shortcut: "a", Variant: "primary"},
		},
		Components: []Component{
			Table{
				ComponentID: "users-table",
				Title:       "Users",
				DataSource:  "/api/users",
				Columns: []TableColumn{
					{Key: "username", Label: "Username", Width: 20, Sortable: true},
					{Key: "role", Label: "Role", Width: 15},
					{Key: "last_login", Label: "Last Login", Width: 20, Format: "date"},
					{Key: "created", Label: "Created", Width: 20, Format: "date"},
				},
				Actions: []TableAction{
					{ID: "edit", Label: "Edit", Icon: "edit"},
					{ID: "reset-password", Label: "Reset Password", Icon: "key"},
					{ID: "delete", Label: "Delete", Icon: "trash", Destructive: true},
				},
			},
		},
	}
}

// BackupsPage returns the backups management page.
func BackupsPage() Page {
	return Page{
		ID:          MenuBackups,
		Title:       "Configuration Backups",
		Description: "Backup and restore configuration",
		Icon:        "archive",
		Actions: []Action{
			{ID: "create-backup", Label: "Create Backup", Icon: "plus", Shortcut: "c", Variant: "primary"},
			{ID: "upload-backup", Label: "Upload Backup", Icon: "upload", Shortcut: "u", Variant: "secondary"},
		},
		Components: []Component{
			Table{
				ComponentID: "backups-table",
				Title:       "Backups",
				DataSource:  "/api/backups",
				Columns: []TableColumn{
					{Key: "name", Label: "Name", Width: 25},
					{Key: "created", Label: "Created", Width: 20, Format: "date"},
					{Key: "size", Label: "Size", Width: 10, Format: "bytes"},
					{Key: "pinned", Label: "Pinned", Width: 8},
					{Key: "description", Label: "Description", Width: 30},
				},
				Actions: []TableAction{
					{ID: "restore", Label: "Restore", Icon: "upload", Destructive: true},
					{ID: "download", Label: "Download", Icon: "download"},
					{ID: "pin", Label: "Pin/Unpin", Icon: "pin"},
					{ID: "delete", Label: "Delete", Icon: "trash", Destructive: true},
				},
			},
		},
	}
}

// LogsPage returns the logs viewer page.
func LogsPage() Page {
	return Page{
		ID:          MenuLogs,
		Title:       "System Logs",
		Description: "View system and firewall logs",
		Icon:        "file-text",
		Components: []Component{
			// LogViewer would be a special component for streaming logs
			// For now, represented as a table
			Table{
				ComponentID: "logs-table",
				Title:       "Recent Logs",
				DataSource:  "/api/logs",
				Searchable:  true,
				Paginated:   true,
				PageSize:    50,
				Columns: []TableColumn{
					{Key: "timestamp", Label: "Time", Width: 20, Format: "date"},
					{Key: "level", Label: "Level", Width: 8},
					{Key: "source", Label: "Source", Width: 15},
					{Key: "message", Label: "Message", Width: 60, Truncate: true},
				},
			},
		},
	}
}

// FlowTriagePage returns the flow triage page.
// This is the main interface for reviewing and triaging learned network flows.
func FlowTriagePage() Page {
	return Page{
		ID:          MenuFlowTriage,
		Title:       brand.Name + " Flow Triage",
		Description: "Review and triage learned network flows",
		Icon:        "list-checks",
		Actions: []Action{
			{ID: "toggle-learning", Label: "Toggle Learning Mode", Icon: "brain", Shortcut: "l", Variant: "secondary"},
			{ID: "allow-all", Label: "Allow All Pending", Icon: "check-circle", Shortcut: "A", Variant: "secondary"},
			{ID: "refresh", Label: "Refresh", Icon: "refresh-cw", Shortcut: "r", Variant: "ghost"},
		},
		Components: []Component{
			// Stats showing flow counts by state
			Stats{
				ComponentID: "flow-stats",
				Title:       "Flow Statistics",
				Columns:     5,
				DataSource:  "/api/flows/stats",
				Values: []StatValue{
					{Key: "pending_flows", Label: "Pending", Icon: "help-circle", Format: "number", Color: "yellow"},
					{Key: "allowed_flows", Label: "Allowed", Icon: "check-circle", Format: "number", Color: "green"},
					{Key: "denied_flows", Label: "Denied", Icon: "x-circle", Format: "number", Color: "red"},
					{Key: "scrutiny_flows", Label: "Scrutiny", Icon: "eye", Format: "number", Color: "orange"},
					{Key: "total_flows", Label: "Total", Icon: "activity", Format: "number"},
				},
			},
			// Alert for learning mode status
			Alert{
				ComponentID: "learning-mode-alert",
				Title:       "Learning Mode",
				Message:     "Learning mode is active. New flows are automatically allowed.",
				Severity:    "info",
				Dismissible: false,
			},
			// Main flow triage table
			Table{
				ComponentID: "flows-table",
				Title:       "Pending Flows",
				DataSource:  "/api/flows?state=pending",
				Searchable:  true,
				Paginated:   true,
				PageSize:    20,
				Columns: []TableColumn{
					{Key: "src_hostname", Label: "Source", Width: 20},
					{Key: "src_mac", Label: "MAC", Width: 18},
					{Key: "protocol", Label: "Proto", Width: 6},
					{Key: "dst_port", Label: "Port", Width: 8},
					{Key: "domain_hint", Label: "Domain Hint", Width: 25},
					{Key: "last_seen", Label: "Last Seen", Width: 15, Format: "relative"},
					{Key: "occurrences", Label: "Hits", Width: 8, Format: "number"},
					{Key: "state", Label: "State", Width: 10},
				},
				Actions: []TableAction{
					{ID: "allow", Label: "Allow", Icon: "check-circle", Shortcut: "a"},
					{ID: "deny", Label: "Deny", Icon: "x-circle", Shortcut: "d", Destructive: true},
					{ID: "scrutiny", Label: "Allow + Scrutiny", Icon: "eye", Shortcut: "s"},
					{ID: "details", Label: "Details", Icon: "info", Shortcut: "enter"},
				},
			},
		},
	}
}

// LearningPage returns the learning engine settings page.
func LearningPage() Page {
	return Page{
		ID:          MenuLearning,
		Title:       "Learning Engine",
		Description: "Configure the " + brand.Name + " learning engine",
		Icon:        "brain",
		Components: []Component{
			// Learning mode toggle card
			Card{
				ComponentID: "learning-mode-card",
				Title:       "Learning Mode (TOFU)",
				Subtitle:    "Trust On First Use - automatically allow new flows during initial setup",
				Icon:        "brain",
				Content: []Component{
					Form{
						ComponentID: "learning-mode-form",
						DataSource:  "/api/learning/mode",
						Sections: []FormSection{
							{
								Fields: []FormField{
									{
										Key:      "enabled",
										Label:    "Learning Mode Active",
										Type:     FieldToggle,
										HelpText: "When enabled, new flows are automatically frozen (allowed)",
									},
								},
							},
						},
					},
				},
			},
			// DNS Visibility settings
			Card{
				ComponentID: "dns-visibility-card",
				Title:       "DNS Visibility",
				Subtitle:    "Transparent DNS redirection for IP-to-domain correlation",
				Icon:        "globe",
				Content: []Component{
					Form{
						ComponentID: "dns-visibility-form",
						DataSource:  "/api/config/learning/dns",
						Sections: []FormSection{
							{
								Fields: []FormField{
									{
										Key:      "dns_redirect_enabled",
										Label:    "Enable DNS Redirection",
										Type:     FieldToggle,
										HelpText: "Redirect DNS queries through the firewall for visibility",
									},
									{
										Key:      "source_interfaces",
										Label:    "Source Interfaces",
										Type:     FieldMultiSelect,
										HelpText: "Interfaces to redirect DNS from",
										Options: []SelectOption{
											{Value: "br-lan", Label: "br-lan (LAN Bridge)"},
											{Value: "eth1", Label: "eth1"},
											{Value: "wlan0", Label: "wlan0 (WiFi)"},
										},
									},
								},
							},
						},
					},
				},
			},
			// Statistics
			Stats{
				ComponentID: "learning-stats",
				Title:       "Engine Statistics",
				Columns:     3,
				DataSource:  "/api/flows/stats",
				Values: []StatValue{
					{Key: "total_flows", Label: "Total Flows", Icon: "activity", Format: "number"},
					{Key: "total_occurrences", Label: "Total Packets", Icon: "package", Format: "number"},
					{Key: "dns_cache_size", Label: "DNS Cache Size", Icon: "database", Format: "number"},
				},
			},
			// Retention settings
			Form{
				ComponentID: "retention-form",
				Title:       "Data Retention",
				DataSource:  "/api/config/learning",
				Sections: []FormSection{
					{
						Fields: []FormField{
							{
								Key:        "retention_days",
								Label:      "Retention Period (days)",
								Type:       FieldNumber,
								HelpText:   "How long to keep pending flows before cleanup",
								Validation: FieldValidation{Min: intPtr(1), Max: intPtr(365)},
							},
							{
								Key:         "ignore_networks",
								Label:       "Ignored Networks",
								Type:        FieldTextarea,
								HelpText:    "Networks to exclude from learning (one CIDR per line)",
								Placeholder: "10.0.0.0/8\n127.0.0.0/8",
							},
						},
					},
				},
			},
		},
	}
}

// Helper function
func intPtr(i int) *int {
	return &i
}
