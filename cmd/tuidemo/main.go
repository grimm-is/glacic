// TUI Demo - runs on macOS without Linux dependencies
// This demonstrates the unified UI schema rendering in the terminal.
package main

import (
	"fmt"
	"os"

	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/ui"
	uitui "grimm.is/glacic/internal/ui/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Demo data
var demoInterfaces = []map[string]interface{}{
	{"name": "eth0", "zone": "WAN", "state": "up", "ipv4": "203.0.113.5/24", "mac": "00:11:22:33:44:55", "rx_bytes": 1234567890, "tx_bytes": 987654321},
	{"name": "eth1", "zone": "LAN", "state": "up", "ipv4": "192.168.1.1/24", "mac": "00:11:22:33:44:56", "rx_bytes": 5678901234, "tx_bytes": 1234567890},
	{"name": "eth2", "zone": "DMZ", "state": "up", "ipv4": "10.0.0.1/24", "mac": "00:11:22:33:44:57", "rx_bytes": 123456789, "tx_bytes": 98765432},
	{"name": "wlan0", "zone": "Guest", "state": "down", "ipv4": "-", "mac": "00:11:22:33:44:58", "rx_bytes": 0, "tx_bytes": 0},
}

var demoServices = []map[string]interface{}{
	{"name": "Firewall", "status": "✅ Running", "details": "nftables active, 42 rules"},
	{"name": "DHCP", "status": "✅ Running", "details": "12 active leases"},
	{"name": "DNS", "status": "✅ Running", "details": "Forwarding to 1.1.1.1"},
	{"name": "NTP", "status": "⏸ Stopped", "details": "Not configured"},
}

var demoStats = map[string]interface{}{
	"uptime":      "3d 14h 22m",
	"cpu":         "12",
	"memory":      "45",
	"connections": "1,247",
}

type model struct {
	pages       []ui.MenuID
	currentPage int
	pageModel   uitui.PageModel
	width       int
	height      int
}

func initialModel() model {
	pages := []ui.MenuID{
		ui.MenuDashboard,
		ui.MenuInterfaces,
		ui.MenuPolicies,
		ui.MenuDHCP,
		ui.MenuIPSets,
	}

	m := model{
		pages:       pages,
		currentPage: 0,
	}
	m.loadPage()
	return m
}

func (m *model) loadPage() {
	pageID := m.pages[m.currentPage]
	schema := ui.GetPage(pageID)
	m.pageModel = uitui.NewPageModel(schema)

	// Load demo data based on page
	data := make(map[string]interface{})
	switch pageID {
	case ui.MenuDashboard:
		data["system-stats"] = demoStats
		data["interface-summary"] = demoInterfaces
		data["service-status"] = demoServices
	case ui.MenuInterfaces:
		data["interfaces-table"] = demoInterfaces
	case ui.MenuPolicies:
		data["policies-table"] = []map[string]interface{}{
			{"order": 1, "name": "wan-to-lan", "from": "WAN", "to": "LAN", "default_action": "drop", "rule_count": 5, "enabled": true},
			{"order": 2, "name": "lan-to-wan", "from": "LAN", "to": "WAN", "default_action": "accept", "rule_count": 3, "enabled": true},
			{"order": 3, "name": "lan-to-dmz", "from": "LAN", "to": "DMZ", "default_action": "accept", "rule_count": 2, "enabled": true},
		}
	case ui.MenuDHCP:
		data["dhcp-scopes"] = []map[string]interface{}{
			{"name": "LAN", "interface": "eth1", "range": "192.168.1.100-200", "lease_time": "24h", "active_leases": 12, "enabled": true},
			{"name": "Guest", "interface": "wlan0", "range": "192.168.2.100-200", "lease_time": "2h", "active_leases": 0, "enabled": false},
		}
	case ui.MenuIPSets:
		data["ipsets-table"] = []map[string]interface{}{
			{"name": "blocklist", "type": "FireHOL", "source": "firehol_level1", "entries": 15234, "last_updated": "2h ago", "auto_update": true},
			{"name": "whitelist", "type": "Manual", "source": "config", "entries": 42, "last_updated": "-", "auto_update": false},
		}
	}

	m.pageModel.SetData(data)
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.pageModel.SetSize(msg.Width, msg.Height)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "left", "h":
			m.currentPage--
			if m.currentPage < 0 {
				m.currentPage = len(m.pages) - 1
			}
			m.loadPage()

		case "right", "l":
			m.currentPage++
			if m.currentPage >= len(m.pages) {
				m.currentPage = 0
			}
			m.loadPage()

		default:
			// Pass to page model
			var cmd tea.Cmd
			m.pageModel, cmd = m.pageModel.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m model) View() string {
	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFDF5")).
		Background(lipgloss.Color("#25A065")).
		Padding(0, 2).
		MarginBottom(1)

	header := headerStyle.Render("🛡️  " + brand.Name + " TUI Demo")

	// Tab bar
	var tabs []string
	for i, pageID := range m.pages {
		page := ui.GetPage(pageID)
		label := string(pageID)
		if page != nil {
			label = page.Title
		}

		style := lipgloss.NewStyle().Padding(0, 2)
		if i == m.currentPage {
			style = style.
				Foreground(lipgloss.Color("#25A065")).
				Bold(true).
				Border(lipgloss.NormalBorder(), false, false, true, false).
				BorderForeground(lipgloss.Color("#25A065"))
		} else {
			style = style.Foreground(lipgloss.Color("240"))
		}
		tabs = append(tabs, style.Render(label))
	}
	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)

	// Help
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		MarginTop(1)
	help := helpStyle.Render("← → Switch pages • ↑ ↓ Navigate • q Quit")

	// Combine
	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		tabBar,
		"",
		m.pageModel.View(),
		help,
	)

	return lipgloss.NewStyle().Margin(1, 2).Render(content)
}

func main() {
	fmt.Println("Starting TUI Demo...")
	fmt.Println("This demonstrates the unified UI schema rendering.")
	fmt.Println()

	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
