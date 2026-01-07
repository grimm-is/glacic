package tui

import "github.com/charmbracelet/lipgloss"

// Glacic Color Palette
var (
	ColorIce   = lipgloss.Color("#A8D8EA") // Cyan/Blueish for accents
	ColorDeep  = lipgloss.Color("#596E79") // Muted Blue/Grey for secondary text
	ColorDark  = lipgloss.Color("#2C3E50") // Dark background elements
	ColorText  = lipgloss.Color("#E0E0E0") // Primary text
	ColorAlert = lipgloss.Color("#FF6B6B") // Red for errors/drops
	ColorGood  = lipgloss.Color("#4ECDC4") // Green for success/accepts
	ColorWarn  = lipgloss.Color("#FFE66D") // Yellow for warnings
	ColorMuted = lipgloss.Color("#6c757d") // Muted text
)

// Styles
var (
	// Base styles
	StyleBase = lipgloss.NewStyle().Foreground(ColorText)

	// Headers and Titles
	StyleHeader = lipgloss.NewStyle().
			Foreground(ColorIce).
			Bold(true).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(ColorDeep).
			Padding(0, 1)

	StyleTitle = lipgloss.NewStyle().
			Foreground(ColorIce).
			Bold(true)

	StyleSubtitle = lipgloss.NewStyle().
			Foreground(ColorDeep).
			Italic(true)

	// Status Indicators
	StyleStatusGood = lipgloss.NewStyle().Foreground(ColorGood).Bold(true)
	StyleStatusBad  = lipgloss.NewStyle().Foreground(ColorAlert).Bold(true)
	StyleStatusWarn = lipgloss.NewStyle().Foreground(ColorWarn).Bold(true)

	// Panel/Card Styles
	StyleCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorDeep).
			Padding(0, 1).
			Margin(0, 1)

	StyleActiveCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorIce).
			Padding(0, 1).
			Margin(0, 1)

	// Table Styles
	StyleTableHeader = lipgloss.NewStyle().
				Foreground(ColorDeep).
				Bold(true).
				Padding(0, 1)

	StyleTableRow = lipgloss.NewStyle().
			Padding(0, 1)

	StyleTableRowSelected = lipgloss.NewStyle().
				Foreground(ColorIce).
				Background(ColorDeep).
				Bold(true).
				Padding(0, 1)

	// Form/Input Styles
	StyleInputPrompt      = lipgloss.NewStyle().Foreground(ColorIce)
	StyleInputText        = lipgloss.NewStyle().Foreground(ColorText)
	StyleInputPlaceholder = lipgloss.NewStyle().Foreground(ColorMuted)
	StyleInputCursor      = lipgloss.NewStyle().Foreground(ColorAlert)

	// App container
	StyleApp = lipgloss.NewStyle().Margin(1, 2)

	// Top Bar / Menu Styles
	StyleTopBar = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(ColorDeep).
			Padding(0, 1).
			MarginBottom(1)

	StyleMenuItem = lipgloss.NewStyle().
			Foreground(ColorDeep).
			Padding(0, 1)

	StyleMenuItemActive = lipgloss.NewStyle().
				Foreground(ColorDark).
				Background(ColorIce).
				Bold(true).
				Padding(0, 1)

	StyleMenuKey = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Faint(true)
)
