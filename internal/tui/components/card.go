package components

import (
	"fmt"

	"grimm.is/glacic/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	cardStyle = lipgloss.NewStyle().
			Padding(1).
			Margin(0, 0, 1, 0).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")) // Purple-ish

	cardTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")). // Pink
			Bold(true).
			MarginBottom(1)

	cardSubtitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")). // Grey
				MarginBottom(1)
)

// GenericComponent wrapper needed for children
// This duplicates app.go's definition but that one is private to the package
// and we can't import main package here.
// Ideally this should be in a shared package, but for now we define what we need.
type GenericComponent struct {
	Model      tea.Model
	Def        ui.Component
	DataSource string
}

type CardModel struct {
	def      ui.Card
	children []GenericComponent
}

func NewCardModel(def ui.Card, children []GenericComponent) CardModel {
	return CardModel{
		def:      def,
		children: children,
	}
}

func (m CardModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, child := range m.children {
		if cmd := child.Model.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func (m CardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	for i := range m.children {
		m.children[i].Model, cmd = m.children[i].Model.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m CardModel) View() string {
	var content []string

	// Header
	header := cardTitleStyle.Render(m.def.Title)
	if m.def.Icon != "" {
		header = fmt.Sprintf("%s %s", m.def.Icon, header) // Simple icon prefix
	}
	content = append(content, header)

	if m.def.Subtitle != "" {
		content = append(content, cardSubtitleStyle.Render(m.def.Subtitle))
	}

	// Children
	for _, child := range m.children {
		content = append(content, child.Model.View())
	}

	return cardStyle.Render(lipgloss.JoinVertical(lipgloss.Left, content...))
}

func (m *CardModel) SetData(data map[string]interface{}) {
	for i := range m.children {
		// Propagate data to children
		// We need to type assert to an interface that has SetData
		// Since we control all components, we know they support it (or should)
		// We define a local interface for checking
		type DataSetter interface {
			SetData(map[string]interface{})
		}

		if setter, ok := m.children[i].Model.(DataSetter); ok {
			setter.SetData(data)
		}
	}
}

// Helper methods to update children data
// This requires type assertion in the parent app
func (m *CardModel) Children() []GenericComponent {
	return m.children
}
