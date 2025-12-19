package tui

import (
	"strings"

	"grimm.is/glacic/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PageModel represents a complete page in the TUI.
type PageModel struct {
	Schema     *ui.Page
	Data       map[string]interface{}
	tables     map[string]TableModel
	focusedIdx int
	width      int
	height     int
}

// NewPageModel creates a page model from a schema.
func NewPageModel(schema *ui.Page) PageModel {
	m := PageModel{
		Schema: schema,
		Data:   make(map[string]interface{}),
		tables: make(map[string]TableModel),
	}

	// Initialize component models
	if schema != nil {
		for _, comp := range schema.Components {
			switch c := comp.(type) {
			case ui.Table:
				m.tables[c.ComponentID] = NewTableModel(c)
			}
		}
	}

	return m
}

// SetData updates the page data.
func (m *PageModel) SetData(data map[string]interface{}) {
	m.Data = data

	// Update component data
	for id, tableData := range data {
		if table, ok := m.tables[id]; ok {
			if arr, ok := tableData.([]map[string]interface{}); ok {
				table.SetData(arr)
				m.tables[id] = table
			}
		}
	}
}

// SetSize updates the page dimensions.
func (m *PageModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Update component sizes
	for id, table := range m.tables {
		table.SetSize(width-4, 12)
		m.tables[id] = table
	}
}

// Init implements tea.Model.
func (m PageModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m PageModel) Update(msg tea.Msg) (PageModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			// Cycle through focusable components
			m.focusedIdx++
			if m.focusedIdx >= len(m.tables) {
				m.focusedIdx = 0
			}
		}
	}

	// Update focused table
	idx := 0
	for id, table := range m.tables {
		if idx == m.focusedIdx {
			var cmd tea.Cmd
			table, cmd = table.Update(msg)
			m.tables[id] = table
			cmds = append(cmds, cmd)
		}
		idx++
	}

	return m, tea.Batch(cmds...)
}

// View implements tea.Model.
func (m PageModel) View() string {
	if m.Schema == nil {
		return "No page loaded"
	}

	var b strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFDF5")).
		Background(lipgloss.Color("#25A065")).
		Padding(0, 1)

	b.WriteString(titleStyle.Render(m.Schema.Title))
	b.WriteString("\n")

	// Description
	if m.Schema.Description != "" {
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render(m.Schema.Description))
		b.WriteString("\n")
	}

	// Actions hint
	if len(m.Schema.Actions) > 0 {
		var actions []string
		for _, a := range m.Schema.Actions {
			key := strings.ToLower(string(a.Label[0]))
			actions = append(actions, "["+key+"] "+a.Label)
		}
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true).
			Render(strings.Join(actions, "  ")))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Components
	for _, comp := range m.Schema.Components {
		switch c := comp.(type) {
		case ui.Table:
			if table, ok := m.tables[c.ComponentID]; ok {
				b.WriteString(table.View())
				b.WriteString("\n\n")
			}
		case ui.Stats:
			b.WriteString(m.renderStats(c))
			b.WriteString("\n\n")
		case ui.Alert:
			b.WriteString(m.renderAlert(c))
			b.WriteString("\n\n")
		}
	}

	return b.String()
}

func (m PageModel) renderStats(s ui.Stats) string {
	var b strings.Builder

	if s.Title != "" {
		b.WriteString(lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("252")).
			Render(s.Title))
		b.WriteString("\n\n")
	}

	// Render stats in a row
	var stats []string
	for _, stat := range s.Values {
		val := "-"
		if m.Data != nil {
			if statsData, ok := m.Data[s.ComponentID].(map[string]interface{}); ok {
				if v, ok := statsData[stat.Key]; ok {
					val = formatValue(v.(string), stat.Format)
				}
			}
		}

		icon := iconToEmoji(stat.Icon)
		statStr := lipgloss.NewStyle().
			Padding(0, 2).
			Render(icon + " " + stat.Label + ": " + val)
		stats = append(stats, statStr)
	}

	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, stats...))

	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Render(b.String())
}

func (m PageModel) renderAlert(a ui.Alert) string {
	var style lipgloss.Style
	var icon string

	switch a.Severity {
	case "error":
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("#DC3545"))
		icon = "✗"
	case "warning":
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFC107"))
		icon = "⚠"
	case "success":
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("#25A065"))
		icon = "✓"
	default:
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("#17A2B8"))
		icon = "ℹ"
	}

	return style.Render(icon + " " + a.Title + ": " + a.Message)
}

func iconToEmoji(icon string) string {
	icons := map[string]string{
		"clock":      "🕐",
		"cpu":        "💻",
		"hard-drive": "💿",
		"activity":   "📈",
		"wifi":       "📶",
		"globe":      "🌍",
		"shield":     "🛡️",
		"server":     "🖥️",
		"users":      "👥",
		"database":   "💾",
		"gauge":      "📊",
		"file-text":  "📄",
	}

	if emoji, ok := icons[icon]; ok {
		return emoji
	}
	return "•"
}
