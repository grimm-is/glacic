package ui

import "encoding/json"

// Renderer defines the interface for rendering UI components.
type Renderer interface {
	// RenderPage renders a complete page
	RenderPage(page *Page, data map[string]interface{}) (string, error)

	// RenderComponent renders a single component
	RenderComponent(component Component, data interface{}) (string, error)

	// RenderMenu renders the navigation menu
	RenderMenu(items []MenuItem, activeID MenuID) (string, error)
}

// DataProvider fetches data for components.
type DataProvider interface {
	// Fetch retrieves data from an endpoint
	Fetch(endpoint string) (interface{}, error)

	// Submit sends data to an endpoint
	Submit(endpoint string, data interface{}) error
}

// PageData contains all data needed to render a page.
type PageData struct {
	Page       *Page                  `json:"page"`
	Menu       []MenuItem             `json:"menu"`
	ActiveMenu MenuID                 `json:"activeMenu"`
	Breadcrumb []MenuItem             `json:"breadcrumb"`
	Data       map[string]interface{} `json:"data"` // Component data keyed by component ID
	User       *UserInfo              `json:"user,omitempty"`
}

// UserInfo contains current user information for the UI.
type UserInfo struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

// NewPageData creates a PageData for a given menu ID.
func NewPageData(menuID MenuID, user *UserInfo) *PageData {
	page := GetPage(menuID)
	if page == nil {
		return nil
	}

	return &PageData{
		Page:       page,
		Menu:       MainMenu(),
		ActiveMenu: menuID,
		Breadcrumb: GetBreadcrumb(menuID),
		Data:       make(map[string]interface{}),
		User:       user,
	}
}

// ToJSON serializes the page data to JSON for the web UI.
func (pd *PageData) ToJSON() ([]byte, error) {
	return json.Marshal(pd)
}

// ComponentJSON serializes a component definition to JSON.
func ComponentJSON(c Component) ([]byte, error) {
	// Wrap with type information
	wrapper := struct {
		Type      ComponentType `json:"type"`
		Component interface{}   `json:"component"`
	}{
		Type:      c.Type(),
		Component: c,
	}
	return json.Marshal(wrapper)
}

// MenuJSON serializes the menu to JSON.
func MenuJSON() ([]byte, error) {
	return json.Marshal(MainMenu())
}

// AllPagesJSON returns all page definitions as JSON (for web app initialization).
func AllPagesJSON() ([]byte, error) {
	pages := make(map[MenuID]Page)
	for id, fn := range PageRegistry {
		pages[id] = fn()
	}
	return json.Marshal(pages)
}
