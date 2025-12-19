package tui

import (
	"fmt"
	"strings"

	"grimm.is/glacic/internal/brand"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Color scheme for flow states
var (
	// Allowed colors (green)
	colorAllowed     = lipgloss.Color("#00FF7F") // Spring green
	colorAllowedDark = lipgloss.Color("#228B22") // Forest green

	// Denied colors (red)
	colorDenied     = lipgloss.Color("#FF4500") // Orange-red
	colorDeniedDark = lipgloss.Color("#8B0000") // Dark red

	// Pending colors (yellow)
	colorPending     = lipgloss.Color("#FFD700") // Gold/Yellow
	colorPendingDark = lipgloss.Color("#B8860B") // Dark goldenrod

	// Scrutiny colors (orange - needs review)
	colorScrutiny = lipgloss.Color("#FFA500") // Orange

	// Styles
	styleAllowed = lipgloss.NewStyle().
			Foreground(colorAllowed).
			Bold(true)

	styleDenied = lipgloss.NewStyle().
			Foreground(colorDenied).
			Bold(true)

	styleScrutiny = lipgloss.NewStyle().
			Foreground(colorScrutiny).
			Bold(true)

	stylePending = lipgloss.NewStyle().
			Foreground(colorPending).
			Bold(true)

	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFDF5")).
			Background(lipgloss.Color("#25A065")).
			Padding(0, 2)

	styleHelp = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	styleStats = lipgloss.NewStyle().
			Padding(0, 1).
			MarginBottom(1)
)

// FlowData represents a flow for display in the triage interface
type FlowData struct {
	ID          int64  `json:"id"`
	SrcHostname string `json:"src_hostname"`
	SrcMAC      string `json:"src_mac"`
	Protocol    string `json:"protocol"`
	DstPort     int    `json:"dst_port"`
	DomainHint  string `json:"domain_hint"`
	LastSeen    string `json:"last_seen"`
	Occurrences int    `json:"occurrences"`
	State       string `json:"state"`    // "pending", "allowed", "denied"
	Scrutiny    bool   `json:"scrutiny"` // Needs extra logging/review
}

// TriageKeyMap defines key bindings for the triage interface
type TriageKeyMap struct {
	Allow    key.Binding
	Deny     key.Binding
	Scrutiny key.Binding
	Details  key.Binding
	Refresh  key.Binding
	AllowAll key.Binding
	Quit     key.Binding
	Up       key.Binding
	Down     key.Binding
}

// DefaultTriageKeyMap returns the default key bindings
func DefaultTriageKeyMap() TriageKeyMap {
	return TriageKeyMap{
		Allow: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "allow"),
		),
		Deny: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "deny"),
		),
		Scrutiny: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "allow+scrutiny"),
		),
		Details: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "details"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		AllowAll: key.NewBinding(
			key.WithKeys("A"),
			key.WithHelp("A", "allow all pending"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
	}
}

// TriageModel is the Bubble Tea model for the Fire & Ice triage interface
type TriageModel struct {
	flows        []FlowData
	table        table.Model
	keyMap       TriageKeyMap
	width        int
	height       int
	learningMode bool

	// Stats
	pendingCount  int
	allowedCount  int
	deniedCount   int
	scrutinyCount int

	// Callbacks
	OnAllow    func(id int64) error
	OnDeny     func(id int64) error
	OnScrutiny func(id int64) error
	OnRefresh  func() ([]FlowData, error)
}

// NewTriageModel creates a new triage model
func NewTriageModel() TriageModel {
	columns := []table.Column{
		{Title: "Source", Width: 18},
		{Title: "MAC", Width: 17},
		{Title: "Proto", Width: 5},
		{Title: "Port", Width: 6},
		{Title: "Domain Hint", Width: 25},
		{Title: "Last Seen", Width: 12},
		{Title: "Hits", Width: 6},
		{Title: "State", Width: 8},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(15),
	)

	// Custom styles with thermal colors
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color("252"))
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	return TriageModel{
		table:  t,
		keyMap: DefaultTriageKeyMap(),
	}
}

// SetFlows updates the flow data
func (m *TriageModel) SetFlows(flows []FlowData) {
	m.flows = flows
	m.updateStats()
	m.updateRows()
}

// SetLearningMode sets the learning mode status
func (m *TriageModel) SetLearningMode(enabled bool) {
	m.learningMode = enabled
}

// SetSize updates the dimensions
func (m *TriageModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.table.SetHeight(height - 10) // Account for header, stats, help
}

func (m *TriageModel) updateStats() {
	m.pendingCount = 0
	m.allowedCount = 0
	m.deniedCount = 0
	m.scrutinyCount = 0

	for _, f := range m.flows {
		switch f.State {
		case "pending":
			m.pendingCount++
		case "allowed":
			m.allowedCount++
			if f.Scrutiny {
				m.scrutinyCount++
			}
		case "denied":
			m.deniedCount++
		}
	}
}

func (m *TriageModel) updateRows() {
	var rows []table.Row
	for _, f := range m.flows {
		// Format state with color indicator
		stateDisplay := f.State
		switch f.State {
		case "allowed":
			if f.Scrutiny {
				stateDisplay = "⚠ scrutiny"
			} else {
				stateDisplay = "✓ allowed"
			}
		case "denied":
			stateDisplay = "✗ denied"
		case "pending":
			stateDisplay = "? pending"
		}

		hostname := f.SrcHostname
		if hostname == "" {
			hostname = f.SrcMAC
		}

		domainHint := f.DomainHint
		if domainHint == "" {
			domainHint = "-"
		}

		rows = append(rows, table.Row{
			hostname,
			f.SrcMAC,
			f.Protocol,
			fmt.Sprintf("%d", f.DstPort),
			domainHint,
			f.LastSeen,
			fmt.Sprintf("%d", f.Occurrences),
			stateDisplay,
		})
	}
	m.table.SetRows(rows)
}

// SelectedFlow returns the currently selected flow
func (m TriageModel) SelectedFlow() *FlowData {
	idx := m.table.Cursor()
	if idx >= 0 && idx < len(m.flows) {
		return &m.flows[idx]
	}
	return nil
}

// Init implements tea.Model
func (m TriageModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model
func (m TriageModel) Update(msg tea.Msg) (TriageModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keyMap.Allow):
			if flow := m.SelectedFlow(); flow != nil && m.OnAllow != nil {
				if err := m.OnAllow(flow.ID); err == nil {
					flow.State = "allowed"
					flow.Scrutiny = false
					m.updateStats()
					m.updateRows()
				}
			}

		case key.Matches(msg, m.keyMap.Deny):
			if flow := m.SelectedFlow(); flow != nil && m.OnDeny != nil {
				if err := m.OnDeny(flow.ID); err == nil {
					flow.State = "denied"
					m.updateStats()
					m.updateRows()
				}
			}

		case key.Matches(msg, m.keyMap.Scrutiny):
			if flow := m.SelectedFlow(); flow != nil && m.OnScrutiny != nil {
				if err := m.OnScrutiny(flow.ID); err == nil {
					flow.State = "allowed"
					flow.Scrutiny = true
					m.updateStats()
					m.updateRows()
				}
			}

		case key.Matches(msg, m.keyMap.Refresh):
			if m.OnRefresh != nil {
				if flows, err := m.OnRefresh(); err == nil {
					m.SetFlows(flows)
				}
			}

		case key.Matches(msg, m.keyMap.AllowAll):
			// Allow all pending flows
			if m.OnAllow != nil {
				for i := range m.flows {
					if m.flows[i].State == "pending" {
						if err := m.OnAllow(m.flows[i].ID); err == nil {
							m.flows[i].State = "allowed"
						}
					}
				}
				m.updateStats()
				m.updateRows()
			}

		case key.Matches(msg, m.keyMap.Quit):
			return m, tea.Quit
		}
	}

	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// View implements tea.Model
func (m TriageModel) View() string {
	var b strings.Builder

	// Header
	header := styleHeader.Render(brand.Name + " Flow Triage")
	b.WriteString(header)
	b.WriteString("\n\n")

	// Learning mode indicator
	if m.learningMode {
		learningBadge := lipgloss.NewStyle().
			Background(lipgloss.Color("#4169E1")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1).
			Render("LEARNING MODE ACTIVE")
		b.WriteString(learningBadge)
		b.WriteString(" New flows are automatically allowed\n\n")
	}

	// Stats bar
	scrutinyInfo := ""
	if m.scrutinyCount > 0 {
		scrutinyInfo = fmt.Sprintf("  │  %s %d", styleScrutiny.Render("⚠ Scrutiny:"), m.scrutinyCount)
	}
	stats := styleStats.Render(fmt.Sprintf(
		"%s %d  │  %s %d  │  %s %d%s  │  Total: %d",
		stylePending.Render("? Pending:"), m.pendingCount,
		styleAllowed.Render("✓ Allowed:"), m.allowedCount,
		styleDenied.Render("✗ Denied:"), m.deniedCount,
		scrutinyInfo,
		len(m.flows),
	))
	b.WriteString(stats)
	b.WriteString("\n")

	// Table
	tableStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62"))
	b.WriteString(tableStyle.Render(m.table.View()))
	b.WriteString("\n")

	// Help
	help := styleHelp.Render(
		"[a] Allow  [d] Deny  [s] Allow+Scrutiny  [r] Refresh  [A] Allow All  [q] Quit",
	)
	b.WriteString(help)

	return lipgloss.NewStyle().Margin(1, 2).Render(b.String())
}

// ViewCompact returns a compact view for embedding
func (m TriageModel) ViewCompact() string {
	var b strings.Builder

	// Stats bar only
	stats := fmt.Sprintf(
		"%s %d  │  %s %d  │  %s %d",
		stylePending.Render("Pending:"), m.pendingCount,
		styleAllowed.Render("Allowed:"), m.allowedCount,
		styleDenied.Render("Denied:"), m.deniedCount,
	)
	b.WriteString(stats)
	b.WriteString("\n")

	// Table
	b.WriteString(m.table.View())

	return b.String()
}
