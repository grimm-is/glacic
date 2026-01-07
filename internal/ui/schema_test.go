package ui

import (
	"encoding/json"
	"testing"
)

func TestMainMenu(t *testing.T) {
	menu := MainMenu()
	if len(menu) == 0 {
		t.Fatal("MainMenu returned empty menu")
	}

	// Check expected top-level items
	expectedIDs := []MenuID{MenuDashboard, MenuTopology, MenuClients, MenuConsole}
	for _, id := range expectedIDs {
		found := false
		for _, item := range menu {
			if item.ID == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Missing menu item: %s", id)
		}
	}
}

func TestFlattenMenu(t *testing.T) {
	menu := MainMenu()
	flat := FlattenMenu(menu)

	// Should have more items than top-level (due to children)
	if len(flat) <= len(menu) {
		t.Errorf("FlattenMenu should include children, got %d items", len(flat))
	}

	// Check that child items are included
	found := false
	for _, item := range flat {
		if item.ID == MenuPolicies {
			found = true
			break
		}
	}
	if !found {
		t.Error("FlattenMenu should include child item MenuPolicies")
	}
}

func TestFindMenuItem(t *testing.T) {
	menu := MainMenu()

	// Find top-level item
	item := FindMenuItem(menu, MenuDashboard)
	if item == nil {
		t.Error("FindMenuItem failed to find Dashboard")
	}
	if item != nil && item.Label != "Dashboard" {
		t.Errorf("Expected label 'Dashboard', got %q", item.Label)
	}

	// Find nested item
	item = FindMenuItem(menu, MenuPolicies)
	if item == nil {
		t.Error("FindMenuItem failed to find nested Policies item")
	}

	// Find non-existent item
	item = FindMenuItem(menu, "nonexistent")
	if item != nil {
		t.Error("FindMenuItem should return nil for non-existent item")
	}
}

func TestGetBreadcrumb(t *testing.T) {
	// Top-level item
	bc := GetBreadcrumb(MenuDashboard)
	if len(bc) != 1 {
		t.Errorf("Dashboard breadcrumb should have 1 item, got %d", len(bc))
	}

	// Nested item
	bc = GetBreadcrumb(MenuPolicies)
	if len(bc) != 3 {
		t.Errorf("Policies breadcrumb should have 3 items, got %d", len(bc))
	}
	if len(bc) >= 3 {
		if bc[0].ID != MenuConsole {
			t.Errorf("First breadcrumb should be Console, got %s", bc[0].ID)
		}
		if bc[1].ID != MenuGroupShield {
			t.Errorf("Second breadcrumb should be GroupShield, got %s", bc[1].ID)
		}
		if bc[2].ID != MenuPolicies {
			t.Errorf("Third breadcrumb should be Policies, got %s", bc[2].ID)
		}
	}
}

func TestMenuJSON(t *testing.T) {
	data, err := MenuJSON()
	if err != nil {
		t.Fatalf("MenuJSON failed: %v", err)
	}

	var menu []MenuItem
	if err := json.Unmarshal(data, &menu); err != nil {
		t.Fatalf("Failed to unmarshal menu JSON: %v", err)
	}

	if len(menu) == 0 {
		t.Error("Unmarshaled menu is empty")
	}
}

func TestGetPage(t *testing.T) {
	// Existing page
	page := GetPage(MenuDashboard)
	if page == nil {
		t.Error("GetPage failed to find Dashboard page")
	}
	if page != nil && page.Title != "Dashboard" {
		t.Errorf("Expected title 'Dashboard', got %q", page.Title)
	}

	// Non-existent page
	page = GetPage("nonexistent")
	if page != nil {
		t.Error("GetPage should return nil for non-existent page")
	}
}

func TestDashboardPage(t *testing.T) {
	page := DashboardPage()

	if page.ID != MenuDashboard {
		t.Errorf("Expected ID %s, got %s", MenuDashboard, page.ID)
	}

	if len(page.Components) == 0 {
		t.Error("Dashboard should have components")
	}

	// Check for expected component types
	hasStats := false
	hasTable := false
	for _, comp := range page.Components {
		switch comp.Type() {
		case ComponentStats:
			hasStats = true
		case ComponentTable:
			hasTable = true
		}
	}

	if !hasStats {
		t.Error("Dashboard should have a Stats component")
	}
	if !hasTable {
		t.Error("Dashboard should have a Table component")
	}
}

func TestInterfacesPage(t *testing.T) {
	page := InterfacesPage()

	if page.ID != MenuInterfaces {
		t.Errorf("Expected ID %s, got %s", MenuInterfaces, page.ID)
	}

	// Should have actions
	if len(page.Actions) == 0 {
		t.Error("Interfaces page should have actions")
	}

	// Should have a table
	hasTable := false
	for _, comp := range page.Components {
		if comp.Type() == ComponentTable {
			hasTable = true
			table := comp.(Table)
			if len(table.Columns) == 0 {
				t.Error("Interfaces table should have columns")
			}
			if len(table.Actions) == 0 {
				t.Error("Interfaces table should have row actions")
			}
		}
	}
	if !hasTable {
		t.Error("Interfaces page should have a table")
	}
}

func TestInterfaceEditForm(t *testing.T) {
	form := InterfaceEditForm()

	if form.ComponentID == "" {
		t.Error("Form should have an ID")
	}

	if len(form.Sections) == 0 {
		t.Error("Form should have sections")
	}

	// Check for expected fields
	fieldKeys := make(map[string]bool)
	for _, section := range form.Sections {
		for _, field := range section.Fields {
			fieldKeys[field.Key] = true
		}
	}

	expectedFields := []string{"name", "zone", "enabled", "mtu", "ipv4_method"}
	for _, key := range expectedFields {
		if !fieldKeys[key] {
			t.Errorf("Form missing expected field: %s", key)
		}
	}
}

func TestProtectionPage(t *testing.T) {
	page := ProtectionPage()

	// Should have a form component
	hasForm := false
	for _, comp := range page.Components {
		if comp.Type() == ComponentForm {
			hasForm = true
			form := comp.(Form)

			// Check for protection-related fields
			fieldKeys := make(map[string]bool)
			for _, section := range form.Sections {
				for _, field := range section.Fields {
					fieldKeys[field.Key] = true
				}
			}

			expectedFields := []string{"anti_spoofing", "bogon_filtering", "syn_flood_protection"}
			for _, key := range expectedFields {
				if !fieldKeys[key] {
					t.Errorf("Protection form missing field: %s", key)
				}
			}
		}
	}

	if !hasForm {
		t.Error("Protection page should have a form")
	}
}

func TestAllPagesJSON(t *testing.T) {
	data, err := AllPagesJSON()
	if err != nil {
		t.Fatalf("AllPagesJSON failed: %v", err)
	}

	// Just verify it's valid JSON with expected structure
	var pages map[string]interface{}
	if err := json.Unmarshal(data, &pages); err != nil {
		t.Fatalf("Failed to unmarshal pages JSON: %v", err)
	}

	if len(pages) == 0 {
		t.Error("No pages returned")
	}

	// Check that key pages exist
	expectedPages := []string{"dashboard", "interfaces", "firewall.policies", "services.dhcp", "services.dns"}
	for _, id := range expectedPages {
		if _, ok := pages[id]; !ok {
			t.Errorf("Missing page: %s", id)
		}
	}
}

func TestComponentTypes(t *testing.T) {
	// Test that components return correct types
	table := Table{ComponentID: "test"}
	if table.Type() != ComponentTable {
		t.Errorf("Table.Type() = %s, want %s", table.Type(), ComponentTable)
	}

	form := Form{ComponentID: "test"}
	if form.Type() != ComponentForm {
		t.Errorf("Form.Type() = %s, want %s", form.Type(), ComponentForm)
	}

	stats := Stats{ComponentID: "test"}
	if stats.Type() != ComponentStats {
		t.Errorf("Stats.Type() = %s, want %s", stats.Type(), ComponentStats)
	}

	card := Card{ComponentID: "test"}
	if card.Type() != ComponentCard {
		t.Errorf("Card.Type() = %s, want %s", card.Type(), ComponentCard)
	}

	alert := Alert{ComponentID: "test"}
	if alert.Type() != ComponentAlert {
		t.Errorf("Alert.Type() = %s, want %s", alert.Type(), ComponentAlert)
	}

	tabs := Tabs{ComponentID: "test"}
	if tabs.Type() != ComponentTabs {
		t.Errorf("Tabs.Type() = %s, want %s", tabs.Type(), ComponentTabs)
	}
}
