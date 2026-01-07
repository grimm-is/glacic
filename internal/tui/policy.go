package tui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type PolicyModel struct {
	Backend Backend
	List    list.Model
	Width   int
	Height  int
}

type item struct {
	title string
	desc  string
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

func NewPolicyModel(backend Backend) PolicyModel {
	// Initial items (placeholder until Init)
	items := []list.Item{
		item{title: "Loading...", desc: "Fetching policies"},
	}

	defaultDelegate := list.NewDefaultDelegate()
	defaultDelegate.Styles.SelectedTitle = defaultDelegate.Styles.SelectedTitle.
		Foreground(ColorIce).
		BorderLeft(false).
		BorderLeftForeground(ColorIce)
	defaultDelegate.Styles.SelectedDesc = defaultDelegate.Styles.SelectedDesc.
		Foreground(ColorDeep)

	l := list.New(items, defaultDelegate, 0, 0)
	l.Title = "Firewall Zones"
	l.SetShowHelp(false)
	l.Styles.Title = StyleTitle

	return PolicyModel{
		Backend: backend,
		List:    l,
	}
}

func (m PolicyModel) Init() tea.Cmd {
	return func() tea.Msg {
		// Mock fetching zones. In real app, call m.Backend.GetConfig().Zones
		return []item{
			{title: "WAN", desc: "External Interface (eth0) - Default: DROP"},
			{title: "LAN", desc: "Internal Trusted (eth1) - Default: ACCEPT"},
			{title: "DMZ", desc: "Semi-trusted (eth2) - Default: DROP"},
		}
	}
}

func (m PolicyModel) Update(msg tea.Msg) (PolicyModel, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case []item:
		items := make([]list.Item, len(msg))
		for i, it := range msg {
			items[i] = it
		}
		cmd = m.List.SetItems(items)
		return m, cmd

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.List.SetSize(msg.Width-4, msg.Height-4)
	}

	m.List, cmd = m.List.Update(msg)
	return m, cmd
}

func (m PolicyModel) View() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		StyleHeader.Render("POLICY INSPECTOR"),
		StyleCard.Render(m.List.View()),
	)
}
