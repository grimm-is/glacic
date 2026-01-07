// Package tui provides TUI rendering for the unified UI schema.
package tui

import (
	"fmt"
	"strings"

	"grimm.is/glacic/internal/ui"

	"github.com/charmbracelet/lipgloss"
)

// Styles for TUI rendering
var (
	// Colors
	primaryColor   = lipgloss.Color("#25A065")
	secondaryColor = lipgloss.Color("#6C757D")
	dangerColor    = lipgloss.Color("#DC3545")
	warningColor   = lipgloss.Color("#FFC107")
	infoColor      = lipgloss.Color("#17A2B8")
	mutedColor     = lipgloss.Color("240")

	// Base styles
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFDF5")).
			Background(primaryColor).
			Padding(0, 1).
			Bold(true)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			MarginLeft(1)

	sectionStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1).
			MarginTop(1)

	menuItemStyle = lipgloss.NewStyle().
			Padding(0, 2)

	menuActiveStyle = lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true).
			Padding(0, 2)

	menuHeaderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Bold(true).
			MarginTop(1).
			MarginBottom(0)

	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("252")).
				BorderStyle(lipgloss.NormalBorder()).
				BorderBottom(true).
				BorderForeground(mutedColor)

	tableCellStyle = lipgloss.NewStyle().
			Padding(0, 1)

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Width(20)

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255"))

	helpStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Italic(true)

	badgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")).
			Background(primaryColor).
			Padding(0, 1)

	errorBadgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")).
			Background(dangerColor).
			Padding(0, 1)
)

// Renderer implements ui.Renderer for TUI output.
type Renderer struct {
	width  int
	height int
}

// NewRenderer creates a new TUI renderer.
func NewRenderer(width, height int) *Renderer {
	return &Renderer{width: width, height: height}
}

// SetSize updates the terminal size.
func (r *Renderer) SetSize(width, height int) {
	r.width = width
	r.height = height
}

// RenderMenu renders the navigation menu for TUI.
func (r *Renderer) RenderMenu(items []ui.MenuItem, activeID ui.MenuID) string {
	var b strings.Builder

	for _, item := range items {
		r.renderMenuItem(&b, item, activeID, 0)
	}

	return b.String()
}

func (r *Renderer) renderMenuItem(b *strings.Builder, item ui.MenuItem, activeID ui.MenuID, depth int) {
	indent := strings.Repeat("  ", depth)
	icon := r.iconToEmoji(item.Icon)

	style := menuItemStyle
	if item.ID == activeID {
		style = menuActiveStyle
	}

	// Render the item
	label := fmt.Sprintf("%s%s %s", indent, icon, item.Label)
	if item.Badge != "" {
		label += " " + badgeStyle.Render(item.Badge)
	}
	b.WriteString(style.Render(label))
	b.WriteString("\n")

	// Render children
	for _, child := range item.Children {
		r.renderMenuItem(b, child, activeID, depth+1)
	}
}

// RenderBreadcrumb renders breadcrumb navigation.
func (r *Renderer) RenderBreadcrumb(items []ui.MenuItem) string {
	var parts []string
	for _, item := range items {
		parts = append(parts, item.Label)
	}
	return subtitleStyle.Render(strings.Join(parts, " > "))
}

// RenderPage renders a complete page.
func (r *Renderer) RenderPage(page *ui.Page, data map[string]interface{}) string {
	var b strings.Builder

	// Title
	icon := r.iconToEmoji(page.Icon)
	b.WriteString(titleStyle.Render(fmt.Sprintf("%s %s", icon, page.Title)))
	b.WriteString("\n")

	if page.Description != "" {
		b.WriteString(subtitleStyle.Render(page.Description))
		b.WriteString("\n")
	}

	// Actions (as hints)
	if len(page.Actions) > 0 {
		var actions []string
		for _, a := range page.Actions {
			key := strings.ToLower(string(a.Label[0]))
			actions = append(actions, fmt.Sprintf("[%s] %s", key, a.Label))
		}
		b.WriteString(helpStyle.Render(strings.Join(actions, "  ")))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Components
	for _, comp := range page.Components {
		compData := data[comp.ID()]
		rendered := r.RenderComponent(comp, compData)
		b.WriteString(rendered)
		b.WriteString("\n")
	}

	return b.String()
}

// RenderComponent renders a single component.
func (r *Renderer) RenderComponent(component ui.Component, data interface{}) string {
	switch c := component.(type) {
	case ui.Table:
		return r.renderTable(c, data)
	case ui.Form:
		return r.renderForm(c, data)
	case ui.Stats:
		return r.renderStats(c, data)
	case ui.Card:
		return r.renderCard(c, data)
	case ui.Alert:
		return r.renderAlert(c)
	case ui.Tabs:
		return r.renderTabs(c, data)
	default:
		return fmt.Sprintf("[Unknown component type: %s]", component.Type())
	}
}

func (r *Renderer) renderTable(t ui.Table, data interface{}) string {
	var b strings.Builder

	if t.Title != "" {
		b.WriteString(menuHeaderStyle.Render(t.Title))
		b.WriteString("\n")
	}

	// Calculate column widths
	totalWidth := 0
	for _, col := range t.Columns {
		if !col.Hidden {
			totalWidth += col.Width + 2 // padding
		}
	}

	// Header
	var headerCells []string
	for _, col := range t.Columns {
		if !col.Hidden {
			cell := tableHeaderStyle.Width(col.Width).Render(col.Label)
			headerCells = append(headerCells, cell)
		}
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, headerCells...))
	b.WriteString("\n")

	// Rows
	if rows, ok := data.([]map[string]interface{}); ok {
		for _, row := range rows {
			var cells []string
			for _, col := range t.Columns {
				if !col.Hidden {
					val := fmt.Sprintf("%v", row[col.Key])
					val = r.formatValue(val, col.Format)
					if col.Truncate && len(val) > col.Width {
						val = val[:col.Width-3] + "..."
					}
					cell := tableCellStyle.Width(col.Width).Render(val)
					cells = append(cells, cell)
				}
			}
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cells...))
			b.WriteString("\n")
		}
	} else if data == nil {
		b.WriteString(helpStyle.Render(t.EmptyText))
		b.WriteString("\n")
	}

	return sectionStyle.Render(b.String())
}

func (r *Renderer) renderForm(f ui.Form, data interface{}) string {
	var b strings.Builder

	if f.Title != "" {
		b.WriteString(menuHeaderStyle.Render(f.Title))
		b.WriteString("\n\n")
	}

	formData, _ := data.(map[string]interface{})

	for _, section := range f.Sections {
		if section.Title != "" {
			b.WriteString(lipgloss.NewStyle().Bold(true).Render(section.Title))
			b.WriteString("\n")
		}

		for _, field := range section.Fields {
			// Get current value
			var value string
			if formData != nil {
				if v, ok := formData[field.Key]; ok {
					value = fmt.Sprintf("%v", v)
				}
			}
			if value == "" && field.DefaultValue != nil {
				value = fmt.Sprintf("%v", field.DefaultValue)
			}

			// Render field
			label := labelStyle.Render(field.Label + ":")
			val := valueStyle.Render(value)
			if value == "" {
				val = helpStyle.Render(field.Placeholder)
			}

			b.WriteString(fmt.Sprintf("%s %s\n", label, val))

			if field.HelpText != "" {
				b.WriteString(helpStyle.Render("  " + field.HelpText))
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}

	return sectionStyle.Render(b.String())
}

func (r *Renderer) renderStats(s ui.Stats, data interface{}) string {
	var b strings.Builder

	if s.Title != "" {
		b.WriteString(menuHeaderStyle.Render(s.Title))
		b.WriteString("\n\n")
	}

	statsData, _ := data.(map[string]interface{})

	// Render stats in columns
	var statStrings []string
	for _, stat := range s.Values {
		var value string
		if statsData != nil {
			if v, ok := statsData[stat.Key]; ok {
				value = r.formatValue(fmt.Sprintf("%v", v), stat.Format)
			}
		}
		if value == "" {
			value = "-"
		}

		icon := r.iconToEmoji(stat.Icon)
		statStr := fmt.Sprintf("%s %s: %s", icon, stat.Label, valueStyle.Render(value))
		statStrings = append(statStrings, statStr)
	}

	// Join stats with spacing
	b.WriteString(strings.Join(statStrings, "  │  "))

	return sectionStyle.Render(b.String())
}

func (r *Renderer) renderCard(c ui.Card, data interface{}) string {
	var b strings.Builder

	icon := r.iconToEmoji(c.Icon)
	b.WriteString(menuHeaderStyle.Render(fmt.Sprintf("%s %s", icon, c.Title)))
	if c.Subtitle != "" {
		b.WriteString(subtitleStyle.Render(" - " + c.Subtitle))
	}
	b.WriteString("\n")

	// Render nested components
	for _, comp := range c.Content {
		b.WriteString(r.RenderComponent(comp, data))
	}

	return sectionStyle.Render(b.String())
}

func (r *Renderer) renderAlert(a ui.Alert) string {
	var style lipgloss.Style
	var icon string

	switch a.Severity {
	case "error":
		style = lipgloss.NewStyle().Foreground(dangerColor)
		icon = "✗"
	case "warning":
		style = lipgloss.NewStyle().Foreground(warningColor)
		icon = "⚠"
	case "success":
		style = lipgloss.NewStyle().Foreground(primaryColor)
		icon = "✓"
	default:
		style = lipgloss.NewStyle().Foreground(infoColor)
		icon = "ℹ"
	}

	content := fmt.Sprintf("%s %s: %s", icon, a.Title, a.Message)
	return style.Render(content)
}

func (r *Renderer) renderTabs(t ui.Tabs, data interface{}) string {
	var b strings.Builder

	// Render tab bar
	var tabLabels []string
	for i, tab := range t.Tabs {
		style := menuItemStyle
		if (t.DefaultTab != "" && tab.ID == t.DefaultTab) || (t.DefaultTab == "" && i == 0) {
			style = menuActiveStyle
		}
		label := tab.Label
		if tab.Badge != "" {
			label += " " + badgeStyle.Render(tab.Badge)
		}
		tabLabels = append(tabLabels, style.Render(label))
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, tabLabels...))
	b.WriteString("\n\n")

	// Render active tab content (first tab or default)
	activeTab := t.Tabs[0]
	for _, tab := range t.Tabs {
		if tab.ID == t.DefaultTab {
			activeTab = tab
			break
		}
	}

	for _, comp := range activeTab.Content {
		b.WriteString(r.RenderComponent(comp, data))
	}

	return b.String()
}

// formatValue formats a value based on its format hint.
func (r *Renderer) formatValue(value, format string) string {
	switch format {
	case "bytes":
		// Simple byte formatting (would need actual parsing in real impl)
		return value
	case "duration":
		return value
	case "percent":
		return value + "%"
	case "date":
		return value
	default:
		return value
	}
}

// iconToEmoji converts icon names to terminal-friendly representations.
func (r *Renderer) iconToEmoji(icon string) string {
	icons := map[string]string{
		"home":         "🏠",
		"network":      "🌐",
		"layers":       "📚",
		"shield":       "🛡️",
		"shield-check": "✅",
		"list":         "📋",
		"shuffle":      "🔀",
		"database":     "💾",
		"gauge":        "📊",
		"server":       "🖥️",
		"wifi":         "📶",
		"globe":        "🌍",
		"git-branch":   "🔀",
		"map":          "🗺️",
		"git-merge":    "🔗",
		"settings":     "⚙️",
		"users":        "👥",
		"archive":      "📦",
		"file-text":    "📄",
		"sliders":      "🎚️",
		"plus":         "+",
		"edit":         "✏️",
		"trash":        "🗑️",
		"play":         "▶",
		"pause":        "⏸",
		"refresh-cw":   "🔄",
		"eye":          "👁",
		"download":     "⬇️",
		"upload":       "⬆️",
		"pin":          "📌",
		"key":          "🔑",
		"clock":        "🕐",
		"cpu":          "💻",
		"hard-drive":   "💿",
		"activity":     "📈",
	}

	if emoji, ok := icons[icon]; ok {
		return emoji
	}
	return "•"
}
