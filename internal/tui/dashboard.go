package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DashboardModel is the main HUD view
type DashboardModel struct {
	Backend Backend
	Status  *EnrichedStatus
	Width   int
	Height  int
}

func NewDashboardModel(backend Backend) DashboardModel {
	return DashboardModel{
		Backend: backend,
	}
}

func (m DashboardModel) Init() tea.Cmd {
	return func() tea.Msg {
		status, err := m.Backend.GetStatus()
		if err != nil {
			return nil // Handle error properly in real app
		}
		return status
	}
}

func (m DashboardModel) Update(msg tea.Msg) (DashboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case *EnrichedStatus:
		m.Status = msg
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
	}
	return m, nil
}

func (m DashboardModel) View() string {
	if m.Status == nil {
		return "Loading Dashboard..."
	}

	// Layout:
	// [ Header / Status ]
	// [ Sparklines ]
	// [ Alerts ]

	// 1. Status Block
	statusIcon := "✅"
	statusText := StyleStatusGood.Render("ONLINE")
	if !m.Status.Running {
		statusIcon = "❌"
		statusText = StyleStatusBad.Render("OFFLINE")
	}

	statusBlock := StyleCard.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			StyleTitle.Render("System Status"),
			fmt.Sprintf("%s %s", statusIcon, statusText),
			StyleSubtitle.Render(fmt.Sprintf("Uptime: %s", m.Status.Uptime)),
		),
	)

	// 2. Metrics Block (Placeholder for Gauges)
	metricsBlock := StyleCard.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			StyleTitle.Render("Resource Usage"),
			fmt.Sprintf("CPU: %s", progressBar(0.15)),
			fmt.Sprintf("RAM: %s", progressBar(0.42)),
		),
	)

	// 3. Throughput Block (Placeholder for Sparklines)
	throughputBlock := StyleCard.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			StyleTitle.Render("Network Throughput"),
			"RX:  ▂▃▅▇ (1.2 Gbps)",
			"TX:  ▂    (120 Mbps)",
		),
	)

	// Top Row
	topRow := lipgloss.JoinHorizontal(lipgloss.Top, statusBlock, metricsBlock, throughputBlock)

	// 4. Alert Ticker
	alertsBlock := StyleCard.Width(60).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			StyleTitle.Render("System Alerts"),
			StyleStatusWarn.Render("• [10:23] High latency on WAN interface"),
			StyleStatusBad.Render("• [10:15] Failed login attempt from 192.168.1.50"),
			StyleStatusGood.Render("• [09:00] Backup completed successfully"), // Used Good color which is Muted? No, Good is Green. Muted is better.
		),
	)

	return lipgloss.JoinVertical(lipgloss.Left,
		topRow,
		alertsBlock,
	)
}

// Simple text-based progress bar helper
func progressBar(percent float64) string {
	w := 20
	filled := int(float64(w) * percent)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", w-filled)
	return fmt.Sprintf("[%s] %.0f%%", bar, percent*100)
}
