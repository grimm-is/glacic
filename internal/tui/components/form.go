package components

import (
	"fmt"
	"reflect"
	"strings"

	"grimm.is/glacic/internal/ui"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	formStyle = lipgloss.NewStyle().
			Padding(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")) // Purple-ish

	sectionTitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("63")).
				Bold(true).
				MarginBottom(1)

	fieldLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Width(25)

	fieldValueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255"))

	focusedLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("63")).
				Bold(true).
				Width(25)

	modifiedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")) // Orange

	toggleOnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")). // Green
			SetString("[x] Enabled")

	toggleOffStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")). // Grey
			SetString("[ ] Disabled")

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			MarginTop(1)
)

// FormSubmitMsg is sent when form is submitted
type FormSubmitMsg struct {
	Endpoint string
	Data     map[string]interface{}
}

type FormModel struct {
	def          ui.Form
	data         interface{}
	editedValues map[string]string // Edited values (key -> string value)
	cursor       int               // Current field index
	editing      bool              // Currently editing a field
	textInput    textinput.Model   // Text input for editing
	allFields    []ui.FormField    // Flattened list of all fields
}

func NewFormModel(def ui.Form) FormModel {
	// Flatten all fields for navigation
	var allFields []ui.FormField
	for _, section := range def.Sections {
		allFields = append(allFields, section.Fields...)
	}

	ti := textinput.New()
	ti.Placeholder = "Enter value..."
	ti.CharLimit = 256

	return FormModel{
		def:          def,
		editedValues: make(map[string]string),
		allFields:    allFields,
		textInput:    ti,
	}
}

func (m *FormModel) SetData(data map[string]interface{}) {
	if m.def.DataSource == "" {
		return
	}
	if val, ok := data[m.def.DataSource]; ok {
		m.data = val
	}
}

func (m FormModel) Init() tea.Cmd {
	return nil
}

func (m FormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.editing {
			switch msg.String() {
			case "enter":
				// Save edit
				if m.cursor < len(m.allFields) {
					m.editedValues[m.allFields[m.cursor].Key] = m.textInput.Value()
				}
				m.editing = false
				m.textInput.Blur()
				return m, nil
			case "esc":
				// Cancel edit
				m.editing = false
				m.textInput.Blur()
				return m, nil
			default:
				m.textInput, cmd = m.textInput.Update(msg)
				return m, cmd
			}
		}

		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.allFields)-1 {
				m.cursor++
			}
		case "enter", "e":
			// Start editing
			if m.cursor < len(m.allFields) {
				field := m.allFields[m.cursor]
				if !field.ReadOnly && !field.Disabled {
					m.editing = true
					// Pre-populate with current value
					currentVal := m.getFieldValue(field.Key)
					m.textInput.SetValue(currentVal)
					m.textInput.Focus()
					return m, textinput.Blink
				}
			}
		case "ctrl+s", "a":
			// Apply/Submit
			if m.def.SubmitAction != "" && len(m.editedValues) > 0 {
				submitData := make(map[string]interface{})
				for k, v := range m.editedValues {
					submitData[k] = v
				}
				return m, func() tea.Msg {
					return FormSubmitMsg{Endpoint: m.def.SubmitAction, Data: submitData}
				}
			}
		case " ":
			// Toggle for checkbox/toggle fields
			if m.cursor < len(m.allFields) {
				field := m.allFields[m.cursor]
				if field.Type == ui.FieldToggle || field.Type == ui.FieldCheckbox {
					currentVal := m.getFieldValue(field.Key)
					newVal := "true"
					if currentVal == "true" {
						newVal = "false"
					}
					m.editedValues[field.Key] = newVal
				}
			}
		}
	}

	return m, nil
}

func (m FormModel) View() string {
	var sections []string

	// Form Title
	if m.def.Title != "" {
		sections = append(sections, sectionTitleStyle.Render(m.def.Title))
	}

	fieldIndex := 0
	for _, section := range m.def.Sections {
		var fields []string

		if section.Title != "" {
			fields = append(fields, lipgloss.NewStyle().Bold(true).Render(section.Title))
		}

		for _, field := range section.Fields {
			isFocused := fieldIndex == m.cursor
			isModified := m.editedValues[field.Key] != ""

			// Get value (prefer edited, then original)
			val := m.getFieldValue(field.Key)

			// Render based on field type
			var valStr string
			if field.Type == ui.FieldToggle || field.Type == ui.FieldCheckbox {
				boolVal := val == "true"
				if boolVal {
					valStr = toggleOnStyle.String()
				} else {
					valStr = toggleOffStyle.String()
				}
			} else if m.editing && isFocused {
				valStr = m.textInput.View()
			} else {
				valStr = val
				if valStr == "" && field.Placeholder != "" {
					valStr = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(field.Placeholder)
				}
			}

			// Apply styles
			labelStyle := fieldLabelStyle
			if isFocused {
				labelStyle = focusedLabelStyle
			}

			valueStyle := fieldValueStyle
			if isModified {
				valueStyle = modifiedStyle
			}

			row := lipgloss.JoinHorizontal(lipgloss.Top,
				labelStyle.Render(field.Label),
				valueStyle.Render(valStr),
			)

			// Add focus indicator
			if isFocused {
				row = "→ " + row
			} else {
				row = "  " + row
			}

			fields = append(fields, row)
			fieldIndex++
		}

		sections = append(sections, lipgloss.JoinVertical(lipgloss.Left, fields...))
		sections = append(sections, " ") // Spacer
	}

	// Help text
	help := "↑/↓: navigate • enter: edit • space: toggle • ctrl+s: apply"
	if len(m.editedValues) > 0 {
		help = fmt.Sprintf("(%d changes) • ", len(m.editedValues)) + help
	}
	sections = append(sections, helpStyle.Render(help))

	return formStyle.Render(lipgloss.JoinVertical(lipgloss.Left, sections...))
}

func (m *FormModel) getFieldValue(key string) string {
	// Check edited first
	if val, ok := m.editedValues[key]; ok {
		return val
	}
	// Fall back to original data
	return fmt.Sprintf("%v", m.extractValue(key))
}

func (m *FormModel) extractValue(key string) interface{} {
	if m.data == nil {
		return ""
	}

	val := reflect.ValueOf(m.data)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	// Handle Map
	if val.Kind() == reflect.Map {
		mapVal := val.MapIndex(reflect.ValueOf(key))
		if mapVal.IsValid() {
			return mapVal.Interface()
		}
		return ""
	}

	if val.Kind() != reflect.Struct {
		return ""
	}

	// Try Direct Field Name
	fieldName := strings.Title(key)
	// Handle snake_case to PascalCase for struct fields (e.g. anti_spoofing -> AntiSpoofing)
	if strings.Contains(key, "_") {
		parts := strings.Split(key, "_")
		fieldName = ""
		for _, p := range parts {
			if p == "id" {
				fieldName += "ID"
			} else if p == "ssl" || p == "tls" || p == "api" || p == "url" {
				fieldName += strings.ToUpper(p)
			} else {
				fieldName += strings.Title(p)
			}
		}
	}

	f := val.FieldByName(fieldName)
	if f.IsValid() {
		return f.Interface()
	}

	// Try JSON tag
	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		if strings.Split(tag, ",")[0] == key {
			return val.Field(i).Interface()
		}
	}

	return ""
}
