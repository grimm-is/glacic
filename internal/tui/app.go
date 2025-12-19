package tui

import (
	"time"

	"grimm.is/glacic/internal/ctlplane"
	"grimm.is/glacic/internal/tui/components"
	"grimm.is/glacic/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Refresh interval for data
const refreshInterval = 2 * time.Second

// Styles
var (
	appStyle = lipgloss.NewStyle().Margin(1, 1)

	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFDF5")).
			Background(lipgloss.Color("#25A065")).
			Padding(0, 1).
			Bold(true)

	// Tab styles
	tabStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color("240"))

	activeTabStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color("#25A065")).
			Border(lipgloss.RoundedBorder(), false, false, true, false).
			BorderForeground(lipgloss.Color("#25A065")).
			Bold(true)
)

type tickMsg time.Time

// GenericComponent is a wrapper to handle different component types
type GenericComponent struct {
	Model      tea.Model
	Def        ui.Component
	DataSource string
}

type Model struct {
	client  *ctlplane.Client
	adapter *DataAdapter

	width  int
	height int
	status *ctlplane.Status

	// Navigation
	menuItems   []ui.MenuItem
	activeTab   int // Index of active top-level menu item
	currentPage *ui.Page

	// Active Page Components
	components []GenericComponent

	// Toast notifications
	toasts       components.ToastModel
	lastNotifyID int64

	loading bool
	err     error
}

func NewModel(client *ctlplane.Client) Model {
	menu := ui.MainMenu()

	m := Model{
		client:    client,
		adapter:   NewDataAdapter(client),
		menuItems: menu,
		activeTab: 0,
		loading:   true,
		toasts:    components.NewToastModel(),
	}

	// Load initial page
	if len(menu) > 0 {
		m.loadPage(menu[0].ID)
	}

	return m
}

func (m *Model) loadPage(id ui.MenuID) {
	page := ui.GetPage(id)
	m.currentPage = page
	m.components = nil // Clear existing

	if page == nil {
		return
	}

	// Instantiate renderers for each component
	for _, comp := range page.Components {
		var model tea.Model
		var dataSource string

		switch comp.Type() {
		case ui.ComponentTable:
			if tbl, ok := comp.(ui.Table); ok {
				model = components.NewTableModel(tbl)
				dataSource = tbl.DataSource
			}
		case ui.ComponentStats:
			if stats, ok := comp.(ui.Stats); ok {
				model = components.NewStatsModel(stats)
				dataSource = stats.DataSource
			}
			// Add additional component types (Form, Card, etc.) here as needed
		}

		if model != nil {
			m.components = append(m.components, GenericComponent{
				Model:      model,
				Def:        comp,
				DataSource: dataSource,
			})
		}
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.fetchData(),
		tickCmd(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Propagate size? Maybe later

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab":
			m.activeTab = (m.activeTab + 1) % len(m.menuItems)
			m.loadPage(m.menuItems[m.activeTab].ID)
			return m, m.fetchDataWait() // Instant fetch on tab switch
		case "shift+tab":
			m.activeTab = (m.activeTab - 1 + len(m.menuItems)) % len(m.menuItems)
			m.loadPage(m.menuItems[m.activeTab].ID)
			return m, m.fetchDataWait()
		}

	case tickMsg:
		return m, tea.Batch(m.fetchData(), tickCmd())

	case dataMsg:
		m.loading = false
		m.status = msg.status

		// Update components with new data
		for i, result := range msg.results {
			if i < len(m.components) {
				comp := &m.components[i]

				switch c := comp.Model.(type) {
				case components.TableModel:
					c.SetData(result)
					comp.Model = c
				case components.StatsModel:
					c.SetData(result)
					comp.Model = c
				}
			}
		}

		// Process notifications and add to toasts
		for _, n := range msg.notifications {
			m.toasts.AddFromNotification(n)
		}
		if msg.lastNotifyID > m.lastNotifyID {
			m.lastNotifyID = msg.lastNotifyID
		}
	}

	// Update active components
	for i := range m.components {
		m.components[i].Model, cmd = m.components[i].Model.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if m.loading && m.status == nil {
		return "Connecting to control plane..."
	}

	// Header
	headerText := "FIREWALL CONSOLE"
	header := titleStyle.Render(headerText)

	// Tabs
	var tabViews []string
	for i, item := range m.menuItems {
		style := tabStyle
		if i == m.activeTab {
			style = activeTabStyle
		}
		tabViews = append(tabViews, style.Render(item.Label))
	}
	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, tabViews...)

	// Page Content
	var contentViews []string
	if m.currentPage != nil {
		for _, comp := range m.components {
			contentViews = append(contentViews, comp.Model.View())
		}
	} else {
		contentViews = append(contentViews, "Page not found")
	}

	content := lipgloss.JoinVertical(lipgloss.Left, contentViews...)

	// Toast notifications overlay
	toastView := m.toasts.View()
	if toastView != "" {
		content = lipgloss.JoinVertical(lipgloss.Left, content, "", toastView)
	}

	return appStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, tabBar, content))
}

// Data fetching
type dataMsg struct {
	status        *ctlplane.Status
	results       []interface{} // Matched to m.components order
	notifications []ctlplane.Notification
	lastNotifyID  int64
}

func (m Model) fetchData() tea.Cmd {
	return func() tea.Msg {
		// 1. Get System Status (Global)
		status, _ := m.client.GetStatus()

		// 2. Fetch data for each active component
		var results []interface{}
		for _, comp := range m.components {
			if comp.DataSource != "" {
				res, err := m.adapter.Fetch(comp.DataSource)
				if err != nil {
					// In a real app we'd handle error states in UI
					results = append(results, nil)
				} else {
					results = append(results, res)
				}
			} else {
				results = append(results, nil)
			}
		}

		// 3. Fetch notifications from control plane
		notifs, lastID, _ := m.client.GetNotifications(m.lastNotifyID)

		return dataMsg{status: status, results: results, notifications: notifs, lastNotifyID: lastID}
	}
}

// fetchDataWait is same as fetchData but can be used where we want to signify a wait?
// Actually tea.Cmd is same.
func (m Model) fetchDataWait() tea.Cmd {
	return m.fetchData() // just alias for now
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
