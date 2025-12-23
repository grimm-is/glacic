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
// We use the shared definition from components package now
type GenericComponent = components.GenericComponent

type Model struct {
	backend Backend

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

func NewModel(backend Backend) Model {
	menu := ui.FlattenMenu(ui.MainMenu())
	
	m := Model{
		backend:   backend,
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

	// Instantiate renderers for each component using the factory
	for _, comp := range page.Components {
		model, dataSource := NewComponent(comp)
		
		if model != nil {
			m.components = append(m.components, components.GenericComponent{
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
		// Pass the Full Results Map to all components
		// Each component will look up its own DataSource(s)
		for i := range m.components {
			updateComponentData(&m.components[i], msg.results)
		}

		// Process notifications and add to toasts
		for _, n := range msg.notifications {
			m.toasts.AddFromNotification(n)
		}
		if msg.lastNotifyID > m.lastNotifyID {
			m.lastNotifyID = msg.lastNotifyID
		}

	case components.FormSubmitMsg:
		// Handle form submission
		if m.backend != nil {
			err := m.backend.Submit(msg.Endpoint, msg.Data)
			if err != nil {
				m.toasts.Add("Error", err.Error(), "error")
			} else {
				m.toasts.Add("Success", "Configuration applied", "success")
			}
		}
		return m, m.fetchData() // Refresh data after submit
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
	results       map[string]interface{}
	notifications []ctlplane.Notification
	lastNotifyID  int64
}

func (m Model) fetchData() tea.Cmd {
	return func() tea.Msg {
		// 1. Get System Status (Global)
		status, _ := m.backend.GetStatus()

		// 2. Fetch data for each active component recursively
		results := make(map[string]interface{})
		
		// Collect all needed data sources from the CURRENT PAGE definition
		// We use the definition because m.components only has top-level models
		if m.currentPage != nil {
			sources := CollectDataSources(m.currentPage.Components)
			for _, src := range sources {
				if _, exists := results[src]; !exists {
					res, err := m.backend.Fetch(src)
					if err == nil {
						results[src] = res
					}
				}
			}
		}

		// 3. Fetch notifications from control plane
		notifs, lastID, _ := m.backend.GetNotifications(m.lastNotifyID)

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

// updateComponentData recursively updates data on a component model
func updateComponentData(comp *components.GenericComponent, data map[string]interface{}) {
	type DataSetter interface {
		SetData(map[string]interface{})
	}

	if setter, ok := comp.Model.(DataSetter); ok {
		setter.SetData(data)
	}
}

// Helper to collect all data sources from a component tree
func CollectDataSources(comps []ui.Component) []string {
	var sources []string
	for _, c := range comps {
		sources = append(sources, collectFromComponent(c)...)
	}
	return sources
}

func collectFromComponent(c ui.Component) []string {
	var sources []string

	switch t := c.(type) {
	case ui.Table:
		if t.DataSource != "" {
			sources = append(sources, t.DataSource)
		}
	case ui.Stats:
		if t.DataSource != "" {
			sources = append(sources, t.DataSource)
		}
	case ui.Form:
		if t.DataSource != "" {
			sources = append(sources, t.DataSource)
		}
	case ui.Card:
		for _, child := range t.Content {
			sources = append(sources, collectFromComponent(child)...)
		}
	case ui.Tabs:
		for _, tab := range t.Tabs {
			for _, child := range tab.Content {
				sources = append(sources, collectFromComponent(child)...)
			}
		}
	}
	return sources
}
