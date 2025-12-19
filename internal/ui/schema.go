// Package ui provides a unified UI abstraction layer that can render
// to both TUI (Bubble Tea) and Web (HTML/React) interfaces.
//
// The goal is to define UI structure once and render it consistently
// across both interfaces, so users can switch between TUI and Web
// without having to relearn the system.
package ui

// MenuID uniquely identifies a menu/page in the navigation hierarchy.
type MenuID string

// Standard menu IDs - these are the same in both TUI and Web
const (
	MenuDashboard MenuID = "dashboard"
	MenuTopology  MenuID = "topology"
	MenuClients   MenuID = "clients"
	MenuConsole   MenuID = "console"

	// Console Sub-groups
	MenuGroupGateway MenuID = "group.gateway"
	MenuGroupLAN     MenuID = "group.lan"
	MenuGroupRadio   MenuID = "group.radio"
	MenuGroupShield  MenuID = "group.shield"
	MenuGroupSystem  MenuID = "group.system"

	// Leaf Pages
	MenuInterfaces MenuID = "interfaces"
	MenuZones      MenuID = "zones"
	MenuFirewall   MenuID = "firewall" // Used as group in old, policy in new? Let's check app.js
	// app.js has 'firewall' as child of 'group_shield'. Label 'Firewall Policies'.

	MenuPolicies     MenuID = "firewall.policies"
	MenuNAT          MenuID = "firewall.nat"
	MenuProtection   MenuID = "firewall.protection"
	MenuIPSets       MenuID = "firewall.ipsets"
	MenuQoS          MenuID = "firewall.qos"
	MenuLearning     MenuID = "firewall.learning"
	MenuFlowTriage   MenuID = "firewall.triage"
	MenuServices     MenuID = "services"
	MenuDHCP         MenuID = "services.dhcp"
	MenuDNS          MenuID = "services.dns"
	MenuRouting      MenuID = "routing"
	MenuStaticRoutes MenuID = "routing.static"
	MenuPolicyRoutes MenuID = "routing.policy"
	MenuSystem       MenuID = "system"
	MenuUsers        MenuID = "system.users"
	MenuBackups      MenuID = "system.backups"
	MenuLogs         MenuID = "system.logs"
	MenuSettings     MenuID = "system.settings"
	MenuScanner      MenuID = "scanner"
	MenuVPN          MenuID = "vpn"
	MenuAdvanced     MenuID = "advanced"
	MenuImport       MenuID = "import_wizard"
)

// MenuItem represents a single item in the navigation menu.
type MenuItem struct {
	ID          MenuID     `json:"id"`
	Label       string     `json:"label"`
	Icon        string     `json:"icon"`        // Icon name (e.g., "network", "shield", "settings")
	Description string     `json:"description"` // Tooltip/help text
	Children    []MenuItem `json:"children,omitempty"`
	Badge       string     `json:"badge,omitempty"` // Optional badge (e.g., "3" for notifications)
	Disabled    bool       `json:"disabled,omitempty"`
}

// MainMenu returns the complete navigation menu structure.
// This is the single source of truth for both TUI and Web navigation.
func MainMenu() []MenuItem {
	return []MenuItem{
		{
			ID:          MenuDashboard,
			Label:       "Dashboard",
			Icon:        "activity",
			Description: "System overview and status",
		},
		{
			ID:          MenuTopology,
			Label:       "Topology",
			Icon:        "share-2",
			Description: "Network topology visualization",
		},
		{
			ID:          MenuClients,
			Label:       "Clients",
			Icon:        "monitor",
			Description: "Connected devices and bandwidth",
		},
		{
			ID:          MenuConsole,
			Label:       "Console",
			Icon:        "settings",
			Description: "All configuration settings",
			Children: []MenuItem{
				{
					ID:    MenuGroupGateway,
					Label: "Gateway (WAN)",
					Icon:  "globe",
					Children: []MenuItem{
						{ID: MenuInterfaces, Label: "Interfaces", Icon: "network"},
						{ID: MenuDNS, Label: "DNS", Icon: "globe"},
						{ID: MenuNAT, Label: "Port Forwarding (NAT)", Icon: "arrow-left-right"},
						{ID: MenuStaticRoutes, Label: "Static Routes", Icon: "route"},
					},
				},
				{
					ID:    MenuGroupLAN,
					Label: "LAN Network",
					Icon:  "network",
					Children: []MenuItem{
						{ID: MenuDHCP, Label: "DHCP Server", Icon: "server"},
						{ID: MenuZones, Label: "Zones / VLANs", Icon: "layers"},
					},
				},
				/*
					{
						ID:    MenuGroupRadio,
						Label: "Radio (WiFi)",
						Icon:  "wifi",
						Children: []MenuItem{
							{ID: MenuScanner, Label: "WiFi Scanner", Icon: "wifi"},
						},
					},
				*/
				{
					ID:    MenuGroupShield,
					Label: "Shield",
					Icon:  "shield",
					Children: []MenuItem{
						{ID: MenuPolicies, Label: "Firewall Policies", Icon: "shield"},
						{ID: MenuProtection, Label: "Protection Rules", Icon: "shield-alert"},
						{ID: MenuIPSets, Label: "IP Sets", Icon: "list-filter"},
						{ID: MenuVPN, Label: "VPN", Icon: "lock"},
					},
				},
				{
					ID:    MenuGroupSystem,
					Label: "System",
					Icon:  "settings",
					Children: []MenuItem{
						{ID: MenuSystem, Label: "Status & Resource", Icon: "info"},
						{ID: MenuUsers, Label: "User Management", Icon: "users"},
						{ID: MenuBackups, Label: "Backups", Icon: "save"},
						{ID: MenuLogs, Label: "System Logs", Icon: "scroll-text"},
						{ID: MenuLearning, Label: "Traffic Analysis", Icon: "brain"},
						{ID: MenuQoS, Label: "QoS", Icon: "gauge"},
						{ID: MenuImport, Label: "Import Wizard", Icon: "import"},
						{ID: MenuAdvanced, Label: "Advanced (HCL)", Icon: "file-code"},
					},
				},
			},
		},
	}
}

// FlattenMenu returns a flat list of all menu items (for search, etc.)
func FlattenMenu(items []MenuItem) []MenuItem {
	var result []MenuItem
	for _, item := range items {
		result = append(result, item)
		if len(item.Children) > 0 {
			result = append(result, FlattenMenu(item.Children)...)
		}
	}
	return result
}

// FindMenuItem finds a menu item by ID.
func FindMenuItem(items []MenuItem, id MenuID) *MenuItem {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
		if found := FindMenuItem(items[i].Children, id); found != nil {
			return found
		}
	}
	return nil
}

// GetBreadcrumb returns the path to a menu item (for breadcrumb navigation).
func GetBreadcrumb(id MenuID) []MenuItem {
	var find func(items []MenuItem, path []MenuItem) []MenuItem
	find = func(items []MenuItem, path []MenuItem) []MenuItem {
		for _, item := range items {
			newPath := append(path, item)
			if item.ID == id {
				return newPath
			}
			if result := find(item.Children, newPath); result != nil {
				return result
			}
		}
		return nil
	}
	return find(MainMenu(), nil)
}
