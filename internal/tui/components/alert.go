package components

import (
	"grimm.is/glacic/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	alertBaseStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Margin(0, 0, 1, 0).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))

	alertInfoStyle = alertBaseStyle.Copy().
			BorderForeground(lipgloss.Color("33")) // Blue

	alertSuccessStyle = alertBaseStyle.Copy().
				BorderForeground(lipgloss.Color("42")) // Green

	alertWarningStyle = alertBaseStyle.Copy().
				BorderForeground(lipgloss.Color("220")) // Yellow

	alertErrorStyle = alertBaseStyle.Copy().
			BorderForeground(lipgloss.Color("196")) // Red

	alertTitleStyle = lipgloss.NewStyle().Bold(true)
)

type AlertModel struct {
	def ui.Alert
}

func NewAlertModel(def ui.Alert) AlertModel {
	return AlertModel{def: def}
}

func (m AlertModel) Init() tea.Cmd {
	return nil
}

func (m AlertModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m AlertModel) View() string {
	style := alertInfoStyle
	titleColor := lipgloss.Color("33")

	switch m.def.Severity {
	case "success":
		style = alertSuccessStyle
		titleColor = lipgloss.Color("42")
	case "warning":
		style = alertWarningStyle
		titleColor = lipgloss.Color("220")
	case "error":
		style = alertErrorStyle
		titleColor = lipgloss.Color("196")
	}

	title := alertTitleStyle.Copy().Foreground(titleColor).Render(m.def.Title)
	msg := lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Render(m.def.Message)

	return style.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			title,
			msg,
		),
	)
}
