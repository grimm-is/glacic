package tui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type HistoryModel struct {
	Backend Backend
	List    list.Model
	Width   int
	Height  int
}

type checkpointItem struct {
	title string
	desc  string
}

func (i checkpointItem) Title() string       { return i.title }
func (i checkpointItem) Description() string { return i.desc }
func (i checkpointItem) FilterValue() string { return i.title }

func NewHistoryModel(backend Backend) HistoryModel {
	items := []list.Item{
		checkpointItem{title: "Loading...", desc: "Fetching history"},
	}

	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Configuration History"
	l.Styles.Title = StyleTitle

	return HistoryModel{
		Backend: backend,
		List:    l,
	}
}

func (m HistoryModel) Init() tea.Cmd {
	return func() tea.Msg {
		// Mock history data
		return []checkpointItem{
			{title: "v45 (Current)", desc: "2023-10-27 10:00:00 - admin - Enable DHCP on LAN"},
			{title: "v44", desc: "2023-10-26 14:30:00 - admin - Add DMZ zone"},
			{title: "v43", desc: "2023-10-25 09:15:00 - system - Automatic Backup"},
		}
	}
}

func (m HistoryModel) Update(msg tea.Msg) (HistoryModel, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case []checkpointItem:
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

func (m HistoryModel) View() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		StyleHeader.Render("TIME MACHINE"),
		StyleSubtitle.Render("Select a checkpoint to view diff or rollback"),
		StyleCard.Render(m.List.View()),
	)
}
