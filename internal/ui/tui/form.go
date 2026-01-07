package tui

import (
	"fmt"
	"strings"

	"grimm.is/glacic/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FormModel represents a form in the TUI.
// This is a simplified implementation that displays form fields.
// For full editing, the bubbles/textinput package would be needed.
type FormModel struct {
	Schema     ui.Form
	Data       map[string]interface{}
	fieldKeys  []string
	focusIndex int
	width      int
	height     int
	submitted  bool
	cancelled  bool
	errors     map[string]string
}

// NewFormModel creates a form model from a schema.
func NewFormModel(schema ui.Form) FormModel {
	var fieldKeys []string

	// Collect field keys
	for _, section := range schema.Sections {
		for _, field := range section.Fields {
			fieldKeys = append(fieldKeys, field.Key)
		}
	}

	return FormModel{
		Schema:    schema,
		Data:      make(map[string]interface{}),
		fieldKeys: fieldKeys,
		errors:    make(map[string]string),
	}
}

// SetData populates the form with existing data.
func (m *FormModel) SetData(data map[string]interface{}) {
	m.Data = data
}

// GetData returns the current form data.
func (m FormModel) GetData() map[string]interface{} {
	return m.Data
}

// Init implements tea.Model.
func (m FormModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m FormModel) Update(msg tea.Msg) (FormModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "down", "j":
			m.focusIndex++
			if m.focusIndex >= len(m.fieldKeys) {
				m.focusIndex = 0
			}

		case "shift+tab", "up", "k":
			m.focusIndex--
			if m.focusIndex < 0 {
				m.focusIndex = len(m.fieldKeys) - 1
			}

		case "enter":
			m.submitted = true
			return m, nil

		case "esc":
			m.cancelled = true
			return m, nil
		}
	}

	return m, nil
}

// View implements tea.Model.
func (m FormModel) View() string {
	var b strings.Builder

	// Title
	if m.Schema.Title != "" {
		b.WriteString(lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("252")).
			Render(m.Schema.Title))
		b.WriteString("\n")
		if m.Schema.Description != "" {
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Render(m.Schema.Description))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Fields
	fieldIdx := 0
	for _, section := range m.Schema.Sections {
		// Section title
		if section.Title != "" {
			b.WriteString(lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("252")).
				MarginTop(1).
				Render(section.Title))
			b.WriteString("\n")
		}

		for _, field := range section.Fields {
			// Label
			labelStyle := lipgloss.NewStyle().
				Width(20).
				Foreground(lipgloss.Color("252"))

			label := field.Label
			if field.Validation.Required {
				label += " *"
			}

			// Highlight focused field
			if fieldIdx == m.focusIndex {
				labelStyle = labelStyle.Foreground(lipgloss.Color("#25A065")).Bold(true)
			}

			b.WriteString(labelStyle.Render(label + ":"))
			b.WriteString(" ")

			// Value
			val := "-"
			if v, ok := m.Data[field.Key]; ok && v != nil {
				val = fmt.Sprintf("%v", v)
			}

			valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
			if fieldIdx == m.focusIndex {
				valueStyle = valueStyle.
					Background(lipgloss.Color("57")).
					Padding(0, 1)
			}

			// Handle different field types
			switch field.Type {
			case ui.FieldToggle, ui.FieldCheckbox:
				checked := val == "true" || val == "1" || val == "yes"
				if checked {
					b.WriteString(valueStyle.Render("[x] Enabled"))
				} else {
					b.WriteString(valueStyle.Render("[ ] Disabled"))
				}
			case ui.FieldPassword:
				if val != "-" && val != "" {
					b.WriteString(valueStyle.Render("••••••••"))
				} else {
					b.WriteString(valueStyle.Render(val))
				}
			default:
				b.WriteString(valueStyle.Render(val))
			}

			// Error
			if err, ok := m.errors[field.Key]; ok && err != "" {
				b.WriteString("\n")
				b.WriteString(lipgloss.NewStyle().
					Foreground(lipgloss.Color("#DC3545")).
					Render("  " + err))
			}

			// Help text for focused field
			if field.HelpText != "" && fieldIdx == m.focusIndex {
				b.WriteString("\n")
				b.WriteString(lipgloss.NewStyle().
					Foreground(lipgloss.Color("240")).
					Italic(true).
					Render("  " + field.HelpText))
			}

			b.WriteString("\n")
			fieldIdx++
		}
		b.WriteString("\n")
	}

	// Footer with actions
	b.WriteString("\n")
	submitLabel := m.Schema.SubmitLabel
	if submitLabel == "" {
		submitLabel = "Save"
	}
	cancelLabel := m.Schema.CancelLabel
	if cancelLabel == "" {
		cancelLabel = "Cancel"
	}

	b.WriteString(lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render(fmt.Sprintf("[Enter] %s  [Esc] %s  [↑/↓] Navigate", submitLabel, cancelLabel)))

	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Render(b.String())
}

// IsSubmitted returns true if the form was submitted.
func (m FormModel) IsSubmitted() bool {
	return m.submitted
}

// IsCancelled returns true if the form was cancelled.
func (m FormModel) IsCancelled() bool {
	return m.cancelled
}

// SetError sets an error message for a field.
func (m *FormModel) SetError(key, message string) {
	m.errors[key] = message
}

// ClearErrors clears all error messages.
func (m *FormModel) ClearErrors() {
	m.errors = make(map[string]string)
}
