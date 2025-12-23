package components

import (
	"fmt"

	"grimm.is/glacic/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	tabsBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      "─",
		Left:        "│",
		Right:       "│",
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "┴",
		BottomRight: "┴",
	}

	tabStyle = lipgloss.NewStyle().
			Border(tabsBorder, true).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	activeTabStyle = tabStyle.Copy().
			BorderForeground(lipgloss.Color("63")). // Purple
			Foreground(lipgloss.Color("63")).
			Bold(true)

	tabContentStyle = lipgloss.NewStyle().
			Padding(1)
)

type TabData struct {
	ID       string
	Label    string
	Badge    string
	Children []GenericComponent
}

type TabsModel struct {
	def       ui.Tabs
	tabs      []TabData
	activeIdx int
	IsFocused bool
}

func NewTabsModel(def ui.Tabs, tabs []TabData) TabsModel {
	m := TabsModel{
		def:       def,
		tabs:      tabs,
		activeIdx: 0,
	}
	
	// Set default tab if specified
	if def.DefaultTab != "" {
		for i, t := range tabs {
			if t.ID == def.DefaultTab {
				m.activeIdx = i
				break
			}
		}
	}
	return m
}

func (m TabsModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	// Init current tab children
	if m.activeIdx < len(m.tabs) {
		for _, child := range m.tabs[m.activeIdx].Children {
			if cmd := child.Model.Init(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	return tea.Batch(cmds...)
}

func (m TabsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "right", "l", "n":
			m.activeIdx = min(m.activeIdx+1, len(m.tabs)-1)
			return m, nil
		case "left", "h", "p":
			m.activeIdx = max(m.activeIdx-1, 0)
			return m, nil
		}
	}

	// Propagate updates to children of Active Tab ONLY
	if m.activeIdx < len(m.tabs) {
		tab := m.tabs[m.activeIdx]
		for i := range tab.Children {
			// Careful: updating slice element in struct copy
			// We need to update the model in the slice
			var newModel tea.Model
			newModel, cmd = tab.Children[i].Model.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			// This update on 'tab' copy doesn't affect 'm.tabs'
			// We must assign back
			m.tabs[m.activeIdx].Children[i].Model = newModel
		}
	}

	return m, tea.Batch(cmds...)
}

func (m TabsModel) View() string {
	// Render Tabs Header
	var tabViews []string
	for i, t := range m.tabs {
		style := tabStyle
		if i == m.activeIdx {
			style = activeTabStyle
		}
		label := t.Label
		if t.Badge != "" {
			label += fmt.Sprintf(" (%s)", t.Badge)
		}
		tabViews = append(tabViews, style.Render(label))
	}
	
	header := lipgloss.JoinHorizontal(lipgloss.Top, tabViews...)
	gap := tabStyle.Copy().
		BorderTop(false).
		BorderLeft(false).
		BorderRight(false).
		BorderBottom(true).
		Width(max(0, 80-lipgloss.Width(header))).
		Render("")
	
	headerWithGap := lipgloss.JoinHorizontal(lipgloss.Bottom, header, gap)

	// Render Active Content
	var contentViews []string
	if m.activeIdx < len(m.tabs) {
		for _, child := range m.tabs[m.activeIdx].Children {
			contentViews = append(contentViews, child.Model.View())
		}
	}
	
	content := tabContentStyle.Render(lipgloss.JoinVertical(lipgloss.Left, contentViews...))

	return lipgloss.JoinVertical(lipgloss.Left, headerWithGap, content)
}

// Helpers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m *TabsModel) SetData(data map[string]interface{}) {
	for i := range m.tabs {
		for j := range m.tabs[i].Children {
			type DataSetter interface {
				SetData(map[string]interface{})
			}
			if setter, ok := m.tabs[i].Children[j].Model.(DataSetter); ok {
				setter.SetData(data)
			}
		}
	}
}

// Children returns children of all tabs (flattened) or structured access?
// We need to update data for ALL tabs, not just active one (background updates).
// But for typical TUI we only render active. 
// Data updates usually push down to models.
func (m *TabsModel) AllChildren() []*GenericComponent {
	var children []*GenericComponent
	for i := range m.tabs {
		for j := range m.tabs[i].Children {
			children = append(children, &m.tabs[i].Children[j])
		}
	}
	return children
}
