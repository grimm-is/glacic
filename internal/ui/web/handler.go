// Package web provides HTTP handlers for serving the unified UI schema to web clients.
package web

import (
	"encoding/json"
	"net/http"

	"grimm.is/glacic/internal/ui"
)

// Handler provides HTTP handlers for UI schema endpoints.
type Handler struct{}

// NewHandler creates a new web UI handler.
func NewHandler() *Handler {
	return &Handler{}
}

// RegisterRoutes registers UI schema routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/ui/menu", h.handleMenu)
	mux.HandleFunc("/api/ui/pages", h.handlePages)
	mux.HandleFunc("/api/ui/page/", h.handlePage)
}

// handleMenu returns the navigation menu structure.
func (h *Handler) handleMenu(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ui.MainMenu())
}

// handlePages returns all page definitions.
func (h *Handler) handlePages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pages := make(map[ui.MenuID]ui.Page)
	for id, fn := range ui.PageRegistry {
		pages[id] = fn()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pages)
}

// handlePage returns a single page definition.
func (h *Handler) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract page ID from path: /api/ui/page/{id}
	pageID := ui.MenuID(r.URL.Path[len("/api/ui/page/"):])
	if pageID == "" {
		http.Error(w, "Page ID required", http.StatusBadRequest)
		return
	}

	page := ui.GetPage(pageID)
	if page == nil {
		http.Error(w, "Page not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(page)
}

// PageResponse wraps a page with additional context for the web UI.
type PageResponse struct {
	Page       *ui.Page      `json:"page"`
	Menu       []ui.MenuItem `json:"menu"`
	Breadcrumb []ui.MenuItem `json:"breadcrumb"`
	ActiveMenu ui.MenuID     `json:"activeMenu"`
}

// handlePageWithContext returns a page with full navigation context.
func (h *Handler) HandlePageWithContext(w http.ResponseWriter, r *http.Request, pageID ui.MenuID) {
	page := ui.GetPage(pageID)
	if page == nil {
		http.Error(w, "Page not found", http.StatusNotFound)
		return
	}

	response := PageResponse{
		Page:       page,
		Menu:       ui.MainMenu(),
		Breadcrumb: ui.GetBreadcrumb(pageID),
		ActiveMenu: pageID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
