package components

import (
	"time"

	"grimm.is/glacic/internal/ctlplane"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Toast styles
var (
	toastSuccessStyle = lipgloss.NewStyle().
				Padding(0, 1).
				Bold(true).
				Foreground(lipgloss.Color("#000")).
				Background(lipgloss.Color("#25A065"))

	toastErrorStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true).
			Foreground(lipgloss.Color("#FFF")).
			Background(lipgloss.Color("#E74C3C"))

	toastWarningStyle = lipgloss.NewStyle().
				Padding(0, 1).
				Bold(true).
				Foreground(lipgloss.Color("#000")).
				Background(lipgloss.Color("#F39C12"))

	toastInfoStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true).
			Foreground(lipgloss.Color("#FFF")).
			Background(lipgloss.Color("#3498DB"))
)

// Toast represents a single notification toast
type Toast struct {
	ID        int64
	Type      ctlplane.NotificationType
	Title     string
	Message   string
	ExpiresAt time.Time
}

// ToastModel manages toast notifications for the TUI
type ToastModel struct {
	toasts     []Toast
	nextID     int64
	maxToasts  int
	expireTime time.Duration
}

// NewToastModel creates a new toast manager
func NewToastModel() ToastModel {
	return ToastModel{
		toasts:     make([]Toast, 0),
		maxToasts:  5,
		expireTime: 5 * time.Second,
		nextID:     1,
	}
}

// Add adds a new toast notification
func (m *ToastModel) Add(ntype ctlplane.NotificationType, title, message string) {
	t := Toast{
		ID:        m.nextID,
		Type:      ntype,
		Title:     title,
		Message:   message,
		ExpiresAt: time.Now().Add(m.expireTime),
	}
	m.nextID++

	// Remove oldest if at capacity
	if len(m.toasts) >= m.maxToasts {
		m.toasts = m.toasts[1:]
	}
	m.toasts = append(m.toasts, t)
}

// AddFromNotification adds a notification from the control plane
func (m *ToastModel) AddFromNotification(n ctlplane.Notification) {
	m.Add(n.Type, n.Title, n.Message)
}

// Prune removes expired toasts
func (m *ToastModel) Prune() {
	now := time.Now()
	var remaining []Toast
	for _, t := range m.toasts {
		if t.ExpiresAt.After(now) {
			remaining = append(remaining, t)
		}
	}
	m.toasts = remaining
}

// Init satisfies tea.Model interface
func (m ToastModel) Init() tea.Cmd {
	return nil
}

// Update satisfies tea.Model interface
func (m ToastModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Prune expired toasts on each update
	m.Prune()
	return m, nil
}

// View renders the toasts
func (m ToastModel) View() string {
	if len(m.toasts) == 0 {
		return ""
	}

	var lines []string
	for _, t := range m.toasts {
		style := toastInfoStyle
		switch t.Type {
		case ctlplane.NotifySuccess:
			style = toastSuccessStyle
		case ctlplane.NotifyError:
			style = toastErrorStyle
		case ctlplane.NotifyWarning:
			style = toastWarningStyle
		case ctlplane.NotifyInfo:
			style = toastInfoStyle
		}

		text := t.Title
		if t.Message != "" {
			text += ": " + t.Message
		}
		lines = append(lines, style.Render(text))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// HasToasts returns true if there are any active toasts
func (m ToastModel) HasToasts() bool {
	return len(m.toasts) > 0
}
