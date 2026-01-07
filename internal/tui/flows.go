package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type FlowsModel struct {
	Backend Backend
	Table   table.Model
	Flows   []Flow
	Width   int
	Height  int
}

func NewFlowsModel(backend Backend) FlowsModel {
	columns := []table.Column{
		{Title: "Proto", Width: 6},
		{Title: "Source", Width: 20},
		{Title: "Destination", Width: 20},
		{Title: "State", Width: 12},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorDeep).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(ColorIce).
		Background(ColorDeep).
		Bold(false)
	t.SetStyles(s)

	return FlowsModel{
		Backend: backend,
		Table:   t,
	}
}

func (m FlowsModel) Init() tea.Cmd {
	return func() tea.Msg {
		flows, err := m.Backend.GetFlows("")
		if err != nil {
			return nil
		}
		return flows
	}
}

func (m FlowsModel) Update(msg tea.Msg) (FlowsModel, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case []Flow:
		m.Flows = msg
		rows := make([]table.Row, len(msg))
		for i, f := range msg {
			rows[i] = table.Row{
				strings.ToUpper(f.Proto),
				f.Src,
				f.Dst,
				f.State,
			}
		}
		m.Table.SetRows(rows)

	case tea.KeyMsg:
		switch msg.String() {
		case "r":
			// Refresh
			return m, m.Init()
		}

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.Table.SetHeight(msg.Height - 5) // Reserve space for header/footer
		// Adjust column widths if needed
	}

	m.Table, cmd = m.Table.Update(msg)
	return m, cmd
}

func (m FlowsModel) View() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		StyleHeader.Render("FLOW MONITOR (r: refresh)"),
		StyleCard.Render(m.Table.View()),
		StyleSubtitle.Render(fmt.Sprintf("%d active flows", len(m.Flows))),
	)
}
