package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"grimm.is/glacic/internal/ui"
)

func TestHandler_Menu(t *testing.T) {
	h := NewHandler()
	req := httptest.NewRequest("GET", "/api/ui/menu", nil)
	w := httptest.NewRecorder()

	h.handleMenu(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
	}

	var menu []ui.MenuItem
	if err := json.NewDecoder(resp.Body).Decode(&menu); err != nil {
		t.Fatalf("Failed to decode menu: %v", err)
	}
	if len(menu) == 0 {
		t.Error("Menu is empty")
	}
}

func TestHandler_Pages(t *testing.T) {
	h := NewHandler()
	req := httptest.NewRequest("GET", "/api/ui/pages", nil)
	w := httptest.NewRecorder()

	h.handlePages(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
	}

	var pages map[string]interface{} // Generic map as ui.Page might vary
	if err := json.NewDecoder(resp.Body).Decode(&pages); err != nil {
		t.Fatalf("Failed to decode pages: %v", err)
	}
	// Note: PageRegistry might be empty in tests if not initialized,
	// but standard init usually fills it.
}

func TestHandler_Page_NotFound(t *testing.T) {
	h := NewHandler()
	req := httptest.NewRequest("GET", "/api/ui/page/non_existent", nil)
	w := httptest.NewRecorder()

	h.handlePage(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 NotFound, got %d", resp.StatusCode)
	}
}

func TestHandler_HandlePageWithContext(t *testing.T) {
	// Register a dummy page for testing if registry is empty
	// Or rely on standard ones.
	// ui.PageRegistry is a global var, we can't easily modify it safely in parallel tests.
	// But ui.MainMenu is fixed.

	h := NewHandler()
	// MenuDashboard should exist
	req := httptest.NewRequest("GET", "/api/ui/page/dashboard", nil)
	w := httptest.NewRecorder()

	// We call internal method directly or via handler if exposed?
	// HandlePageWithContext is exported.

	h.HandlePageWithContext(w, req, ui.MenuDashboard)

	resp := w.Result()
	// resp is not needed if we rely on recorder, but good to check status
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
	}
	// It might return 404 if "dashboard" page logic isn't registered in `ui.PageRegistry` during test execution
	// ui package initialization does it.

	// Let's assume it might fail if init didn't run.
	// If 404, it means GetPage returned nil.
}
