package tui

import (
	"grimm.is/glacic/internal/config"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// View represents the currently active screen
type View int

const (
	ViewDashboard View = iota
	ViewFlows
	ViewPolicy
	ViewHistory
	ViewConfigTree // Full config graph
)

// Backend defines the interface for data retrieval and actions.
type Backend interface {
	GetStatus() (*EnrichedStatus, error)
	GetFlows(filter string) ([]Flow, error)
	GetConfig() (*config.Config, error)
}

// Model is the main application state
type Model struct {
	Backend Backend

	// State
	ActiveView View
	Width      int
	Height     int

	// Views
	Dashboard DashboardModel
	Flows     FlowsModel
	Policy    PolicyModel
	History   HistoryModel
	Config    ConfigModel
}

// NewModel creates a new initial model
func NewModel(backend Backend) Model {
	return Model{
		Backend:    backend,
		ActiveView: ViewDashboard,
		Dashboard:  NewDashboardModel(backend),
		Flows:      NewFlowsModel(backend),
		Policy:     NewPolicyModel(backend),
		History:    NewHistoryModel(backend),
		Config:     NewConfigModel(backend),
	}
}

// Init initializes the application
func (m Model) Init() tea.Cmd {
	// Init all views that need initial data
	return tea.Batch(
		m.Dashboard.Init(),
		m.Flows.Init(),
		m.Policy.Init(),
		m.History.Init(),
		m.Config.Init(),
	)
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// If editing form in config view, don't trap global keys if focusing?
		// But let's keep global quit for now unless editing needs q/ctrl+c
		if m.ActiveView != ViewConfigTree {
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			}
		} else {
			// In config view, check if editing
			if m.ActiveView == ViewConfigTree && m.Config.Editing {
				// Don't intercept keys
			} else {
				switch msg.String() {
				case "q", "ctrl+c":
					return m, tea.Quit
				}
			}
		}

		if msg.String() == "tab" {
			// Cycle views
			// If editing config, maybe don't cycle?
			if m.ActiveView == ViewConfigTree && m.Config.Editing {
				// consume
			} else {
				m.ActiveView = (m.ActiveView + 1) % 5
				return m, nil
			}
		}

		// Shortcuts for Top Bar
		if !(m.ActiveView == ViewConfigTree && m.Config.Editing) {
			switch msg.String() {
			case "1":
				m.ActiveView = ViewDashboard
				return m, nil
			case "2":
				m.ActiveView = ViewFlows
				return m, nil
			case "3":
				m.ActiveView = ViewPolicy
				return m, nil
			case "4":
				m.ActiveView = ViewHistory
				return m, nil
			case "5":
				m.ActiveView = ViewConfigTree
				return m, nil
			}
		}

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height

		// Propagate resize to all views
		var cmd tea.Cmd
		m.Dashboard, cmd = m.Dashboard.Update(msg)
		cmds = append(cmds, cmd)

		m.Flows, cmd = m.Flows.Update(msg)
		cmds = append(cmds, cmd)

		m.Policy, cmd = m.Policy.Update(msg)
		cmds = append(cmds, cmd)

		m.History, cmd = m.History.Update(msg)
		cmds = append(cmds, cmd)

		m.Config, cmd = m.Config.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Delegate to active view
	var cmd tea.Cmd
	switch m.ActiveView {
	case ViewDashboard:
		m.Dashboard, cmd = m.Dashboard.Update(msg)
	case ViewFlows:
		m.Flows, cmd = m.Flows.Update(msg)
	case ViewPolicy:
		m.Policy, cmd = m.Policy.Update(msg)
	case ViewHistory:
		m.History, cmd = m.History.Update(msg)
	case ViewConfigTree:
		m.Config, cmd = m.Config.Update(msg)
	}
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// View renders the application
func (m Model) View() string {
	doc := m.ViewTopBar() + "\n"
	// doc := StyleHeader.Render("GLACIC FIREWALL HUD     [Tab] Next View") + "\n\n"

	switch m.ActiveView {
	case ViewDashboard:
		doc += m.Dashboard.View()
	case ViewFlows:
		doc += m.Flows.View()
	case ViewPolicy:
		doc += m.Policy.View()
	case ViewHistory:
		doc += m.History.View()
	case ViewConfigTree:
		doc += m.Config.View()
	}

	return StyleApp.Render(doc)
}

// ViewTopBar renders the top navigation menu
func (m Model) ViewTopBar() string {
	var items []string

	menus := []struct {
		View  View
		Label string
		Key   string
	}{
		{ViewDashboard, "Dashboard", "1"},
		{ViewFlows, "Flows", "2"},
		{ViewPolicy, "Policy", "3"},
		{ViewHistory, "History", "4"},
		{ViewConfigTree, "Config", "5"},
	}

	for _, menu := range menus {
		key := StyleMenuKey.Render("[" + menu.Key + "]")
		label := menu.Label

		if m.ActiveView == menu.View {
			items = append(items, StyleMenuItemActive.Render(key+" "+label))
		} else {
			items = append(items, StyleMenuItem.Render(key+" "+label))
		}
	}

	// Join horizontally
	// Add branding
	brand := StyleTitle.Render("GLACIC ")

	bar := lipgloss.JoinHorizontal(lipgloss.Top, append([]string{brand}, items...)...)
	return StyleTopBar.Render(bar)
}

// Shared Types
type EnrichedStatus struct {
	Running bool
	Uptime  string
}

type Flow struct {
	Proto string
	Src   string
	Dst   string
	State string
}
