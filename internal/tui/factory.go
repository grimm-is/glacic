package tui

import (
	"grimm.is/glacic/internal/tui/components"
	"grimm.is/glacic/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

// NewComponent creates a TUI component model from a UI definition.
// It returns the model and the data source string (if any).
func NewComponent(def ui.Component) (tea.Model, string) {
	switch c := def.(type) {
	case ui.Table:
		return components.NewTableModel(c), c.DataSource

	case ui.Stats:
		return components.NewStatsModel(c), c.DataSource

	case ui.Alert:
		return components.NewAlertModel(c), ""

	case ui.Card:
		// Cards can contain other components, so we need to instantiate them recursively.
		// However, to avoid circular dependencies (factory -> card -> factory),
		// we'll pass a factory function to the card model if needed, or simply
		// assume the CardModel handles its children via a passed-in slice.
		// For simplicity here, we'll instantiate children first.
		var children []components.GenericComponent
		for _, childDef := range c.Content {
			model, src := NewComponent(childDef)
			if model != nil {
				children = append(children, components.GenericComponent{
					Model:      model,
					Def:        childDef,
					DataSource: src,
				})
			}
		}
		return components.NewCardModel(c, children), ""

	case ui.Form:
		return components.NewFormModel(c), c.DataSource

	case ui.Tabs:
		// Tabs also contain children per tab
		var tabsData []components.TabData
		for _, tab := range c.Tabs {
			var tabChildren []components.GenericComponent
			for _, childDef := range tab.Content {
				model, src := NewComponent(childDef)
				if model != nil {
					tabChildren = append(tabChildren, components.GenericComponent{
						Model:      model,
						Def:        childDef,
						DataSource: src,
					})
				}
			}
			tabsData = append(tabsData, components.TabData{
				ID:       tab.ID,
				Label:    tab.Label,
				Badge:    tab.Badge,
				Children: tabChildren,
			})
		}
		return components.NewTabsModel(c, tabsData), ""
	}

	return nil, ""
}
