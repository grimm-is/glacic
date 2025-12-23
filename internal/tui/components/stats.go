package components

import (
	"fmt"
	"reflect"
	"strings"

	"grimm.is/glacic/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	statsStyle = lipgloss.NewStyle().
			Padding(1).
			MarginRight(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62"))

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Bold(true)

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")).
			Bold(true).
			MarginTop(1)
)

type StatsModel struct {
	def  ui.Stats
	data interface{}
}

func NewStatsModel(def ui.Stats) StatsModel {
	return StatsModel{def: def}
}

func (m *StatsModel) SetData(data map[string]interface{}) {
	if m.def.DataSource == "" {
		return
	}
	if val, ok := data[m.def.DataSource]; ok {
		m.data = val
	}
}

func (m StatsModel) View() string {
	var cards []string

	for _, req := range m.def.Values {
		val := m.extractValue(req.Key)

		card := statsStyle.Render(
			lipgloss.JoinVertical(lipgloss.Left,
				labelStyle.Render(req.Label),
				valueStyle.Render(val),
			),
		)
		cards = append(cards, card)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, cards...)
}

func (m *StatsModel) extractValue(key string) string {
	if m.data == nil {
		return "..."
	}

	val := reflect.ValueOf(m.data)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return "?"
	}

	// Try Direct Field Name
	fieldName := strings.Title(key)
	f := val.FieldByName(fieldName)
	if f.IsValid() {
		return m.formatValue(f.Interface())
	}

	// Try JSON tag
	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		if strings.Split(tag, ",")[0] == key {
			return m.formatValue(val.Field(i).Interface())
		}
	}

	return "-"
}

func (m *StatsModel) formatValue(v interface{}) string {
	// Add specific formatting here (bytes, duration, etc)
	return fmt.Sprintf("%v", v)
}

// Init satisfies the tea.Model interface
func (m StatsModel) Init() tea.Cmd {
	return nil
}

// Update satisfies the tea.Model interface
func (m StatsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}
