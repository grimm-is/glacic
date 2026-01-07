package tui

import (
	"fmt"
	"strings"

	"grimm.is/glacic/internal/ui"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TableModel wraps bubbles/table with our schema.
type TableModel struct {
	Schema      ui.Table
	Data        []map[string]interface{}
	table       table.Model
	width       int
	height      int
	focused     bool
	selected    []int
	searchQuery string
}

// NewTableModel creates a table model from a schema.
func NewTableModel(schema ui.Table) TableModel {
	// Build columns from schema
	var columns []table.Column
	for _, col := range schema.Columns {
		if !col.Hidden {
			columns = append(columns, table.Column{
				Title: col.Label,
				Width: col.Width,
			})
		}
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	// Style the table
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	return TableModel{
		Schema: schema,
		table:  t,
	}
}

// SetData updates the table data.
func (m *TableModel) SetData(data []map[string]interface{}) {
	m.Data = data
	m.updateRows()
}

// SetSize updates the table dimensions.
func (m *TableModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.table.SetHeight(height - 4) // Account for header and border
}

// SetFocused sets the focus state.
func (m *TableModel) SetFocused(focused bool) {
	m.focused = focused
	m.table.Focus()
}

func (m *TableModel) updateRows() {
	var rows []table.Row
	for _, item := range m.Data {
		var row []string
		for _, col := range m.Schema.Columns {
			if !col.Hidden {
				val := fmt.Sprintf("%v", item[col.Key])
				if val == "<nil>" {
					val = "-"
				}
				// Format value
				val = formatValue(val, col.Format)
				// Truncate if needed
				if col.Truncate && len(val) > col.Width {
					val = val[:col.Width-3] + "..."
				}
				row = append(row, val)
			}
		}
		rows = append(rows, row)
	}
	m.table.SetRows(rows)
}

// Init implements tea.Model.
func (m TableModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m TableModel) Update(msg tea.Msg) (TableModel, tea.Cmd) {
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m TableModel) View() string {
	var b strings.Builder

	// Title
	if m.Schema.Title != "" {
		b.WriteString(lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("252")).
			MarginBottom(1).
			Render(m.Schema.Title))
		b.WriteString("\n")
	}

	// Table
	b.WriteString(m.table.View())

	// Empty state
	if len(m.Data) == 0 {
		emptyText := m.Schema.EmptyText
		if emptyText == "" {
			emptyText = "No data"
		}
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true).
			Render(emptyText))
	}

	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Render(b.String())
}

// SelectedRow returns the currently selected row data.
func (m TableModel) SelectedRow() map[string]interface{} {
	idx := m.table.Cursor()
	if idx >= 0 && idx < len(m.Data) {
		return m.Data[idx]
	}
	return nil
}

// SelectedIndex returns the currently selected row index.
func (m TableModel) SelectedIndex() int {
	return m.table.Cursor()
}

func formatValue(val, format string) string {
	switch format {
	case "bytes":
		// Would need actual byte parsing
		return val
	case "duration":
		return val
	case "percent":
		return val + "%"
	case "date":
		// Would need date parsing
		return val
	default:
		return val
	}
}
