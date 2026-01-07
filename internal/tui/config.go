package tui

import (
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"grimm.is/glacic/internal/config"
)

type ConfigModel struct {
	Backend Backend
	List    list.Model
	Form    *huh.Form

	// State
	Editing       bool
	ActiveSection string
	Config        *config.Config
	LastError     error

	Width  int
	Height int
}

type sectionItem struct {
	title string
	desc  string
	field string // Name of the field in config.Config
}

func (i sectionItem) Title() string       { return i.title }
func (i sectionItem) Description() string { return i.desc }
func (i sectionItem) FilterValue() string { return i.title }

func NewConfigModel(backend Backend) ConfigModel {
	items := []list.Item{
		sectionItem{title: "API Settings", desc: "Manage HTTP/HTTPS API configuration", field: "API"},
		sectionItem{title: "Features", desc: "Enable/Disable core features", field: "Features"},
		sectionItem{title: "System", desc: "System identity and behavior", field: "System"}, // Assuming System exists or similar
	}

	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Configuration Sections"
	l.Styles.Title = StyleTitle

	return ConfigModel{
		Backend: backend,
		List:    l,
	}
}

type ConfigLoadError struct {
	Err error
}

func (m ConfigModel) Init() tea.Cmd {
	return func() tea.Msg {
		cfg, err := m.Backend.GetConfig()
		if err != nil {
			DebugLog("Config Init Failed: %v", err)
			return ConfigLoadError{Err: err}
		}
		return cfg
	}
}

func (m ConfigModel) Update(msg tea.Msg) (ConfigModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case *config.Config:
		m.Config = msg
		m.LastError = nil
		return m, nil

	case ConfigLoadError:
		m.LastError = msg.Err
		return m, nil

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.List.SetSize(msg.Width-4, msg.Height-4)
		return m, nil

	case tea.KeyMsg:
		if m.Editing {
			if msg.Type == tea.KeyEsc {
				m.Editing = false
				m.Form = nil
				return m, nil
			}

			// Update form
			var formCmd tea.Cmd
			form, formCmd := m.Form.Update(msg)
			if f, ok := form.(*huh.Form); ok {
				m.Form = f
			}

			if m.Form.State == huh.StateCompleted {
				m.Editing = false
				m.Form = nil
				// TODO: Save config back to backend
				// m.Backend.SaveConfig(m.Config)
			}

			return m, formCmd
		}

		switch msg.String() {
		case "enter":
			f, _ := os.OpenFile("tui_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			fmt.Fprintf(f, "%s: Enter pressed. Config exists: %v. Editing: %v\n", time.Now(), m.Config != nil, m.Editing)

			if m.Config == nil {
				f.Close()
				return m, nil
			}

			// Enter critical editing mode
			item := m.List.SelectedItem()
			fmt.Fprintf(f, "Selected item type: %T, Value: %+v\n", item, item)

			selected, ok := item.(sectionItem)
			if ok {
				fmt.Fprintf(f, "Selected section: %s\n", selected.field)
				m.ActiveSection = selected.field
				m.Editing = true

				// Reflection magic to get the field
				val := reflect.ValueOf(m.Config).Elem()
				fieldVal := val.FieldByName(selected.field)

				if !fieldVal.IsValid() || fieldVal.IsNil() {
					fmt.Fprintf(f, "Field %s is invalid or nil\n", selected.field)
					// Initialize if nil? Or show error?
					m.Editing = false
					f.Close()
					return m, nil
				}

				fmt.Fprintf(f, "Creating AutoForm for field %s\n", selected.field)
				// AutoForm expects a pointer to a struct
				// fieldVal is likely a pointer (e.g. *APIConfig)
				m.Form = AutoForm(fieldVal.Interface())
				m.Form.Init()
			} else {
				fmt.Fprintf(f, "Failed to cast item to sectionItem\n")
			}
			f.Close()
			return m, nil
		}
	}

	if !m.Editing {
		m.List, cmd = m.List.Update(msg)
	}

	return m, cmd
}

func (m ConfigModel) View() string {
	if m.Editing && m.Form != nil {
		return lipgloss.JoinVertical(lipgloss.Left,
			StyleHeader.Render("EDITING: "+m.ActiveSection),
			StyleCard.Render(m.Form.View()),
			StyleSubtitle.Render("Esc to Cancel, Enter to Save"),
		)
	}

	if m.Config == nil {
		if m.LastError != nil {
			return lipgloss.JoinVertical(lipgloss.Left,
				StyleHeader.Render("CONFIG EXPLORER"),
				StyleStatusBad.Render("Failed to load configuration:"),
				StyleCard.Render(m.LastError.Error()),
				StyleSubtitle.Render("Check connectivity or try again."),
			)
		}
		return lipgloss.JoinVertical(lipgloss.Left,
			StyleHeader.Render("CONFIG EXPLORER"),
			StyleSubtitle.Render("Loading configuration..."),
		)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		StyleHeader.Render("CONFIG EXPLORER"),
		StyleSubtitle.Render("Select a section to edit"),
		StyleCard.Render(m.List.View()),
	)
}
