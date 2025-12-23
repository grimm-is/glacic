package components

import (
	"fmt"
	"reflect"
	"strings"

	"grimm.is/glacic/internal/ui"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	baseTableStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240"))
)

type TableModel struct {
	table     table.Model
	def       ui.Table
	data      interface{}
	IsFocused bool
}

func NewTableModel(def ui.Table) TableModel {
	columns := make([]table.Column, 0)
	for _, c := range def.Columns {
		if !c.Hidden {
			columns = append(columns, table.Column{
				Title: c.Label,
				Width: c.Width,
			})
		}
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

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
		table: t,
		def:   def,
	}
}

func (m TableModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m TableModel) View() string {
	return baseTableStyle.Render(m.table.View())
}

func (m *TableModel) SetData(data map[string]interface{}) {
	if m.def.DataSource == "" {
		return
	}
	if val, ok := data[m.def.DataSource]; ok {
		m.data = val
		rows := m.generateRows(val)
		m.table.SetRows(rows)
	}
}

func (m *TableModel) generateRows(data interface{}) []table.Row {
	val := reflect.ValueOf(data)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Slice {
		return nil
	}

	var rows []table.Row
	for i := 0; i < val.Len(); i++ {
		item := val.Index(i)
		var row table.Row

		for _, col := range m.def.Columns {
			if col.Hidden {
				continue
			}

			// Extract field value securely using reflection
			fieldVal := m.extractField(item, col.Key)
			row = append(row, fieldVal)
		}
		rows = append(rows, row)
	}
	return rows
}

func (m *TableModel) extractField(item reflect.Value, key string) string {
	// Handle struct fields (case-insensitive match likely needed in real world, but strict for now)
	// We'll try to match JSON tag or Field Name

	if item.Kind() == reflect.Ptr {
		item = item.Elem()
	}
	if item.Kind() != reflect.Struct {
		return ""
	}

	// 1. Try direct struct field by name (Requires exact casing usually, but let's try PascalCase)
	// key "name" -> Field "Name"
	fieldName := strings.Title(key)
	f := item.FieldByName(fieldName)
	if f.IsValid() {
		return fmt.Sprintf("%v", f.Interface())
	}

	// 2. Scan tags if direct match fails (slower but robust)
	typ := item.Type()
	for i := 0; i < item.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		// handle "name,omitempty"
		if strings.Split(tag, ",")[0] == key {
			return fmt.Sprintf("%v", item.Field(i).Interface())
		}
	}

	return "-"
}

func (m *TableModel) SetHeight(h int) {
	m.table.SetHeight(h)
}

// Init satisfies the tea.Model interface
func (m TableModel) Init() tea.Cmd {
	return nil
}
