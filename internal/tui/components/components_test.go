package components

import (
	"testing"

	"grimm.is/glacic/internal/ui"
)

func TestStatsModel(t *testing.T) {
	def := ui.Stats{
		Values: []ui.StatValue{
			{Label: "Uptime", Key: "uptime"},
			{Label: "CPU", Key: "cpu"},
		},
	}
	m := NewStatsModel(def)

	data := struct {
		Uptime string `json:"uptime"`
		CPU    int    `json:"cpu"`
	}{
		Uptime: "1h",
		CPU:    50,
	}

	m.SetData(&data)

	// Since extractValue is unexported, we test via public methods.
	// But View() output is styled string, hard to assert exact content without strip ansi.
	// However, extractValue logic is critical.
	// We can use reflection to test internal methods if we were in same package, which we are (package components).

	val := m.extractValue("uptime")
	if val != "1h" {
		t.Errorf("Expected 1h, got %s", val)
	}

	valCPU := m.extractValue("cpu")
	if valCPU != "50" {
		t.Errorf("Expected 50, got %s", valCPU)
	}
}

func TestTableModel(t *testing.T) {
	def := ui.Table{
		Columns: []ui.TableColumn{
			{Label: "Name", Key: "name", Width: 10},
			{Label: "Value", Key: "value", Width: 10},
		},
	}
	m := NewTableModel(def)

	data := []struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}{
		{"A", 1},
		{"B", 2},
	}

	m.SetData(data) // Should not panic

	// Test internal helper
	rows := m.generateRows(data)
	if len(rows) != 2 {
		t.Errorf("Expected 2 rows, got %d", len(rows))
	}
	if len(rows[0]) != 2 {
		t.Errorf("Expected 2 columns, got %d", len(rows[0]))
	}

	// Check content
	if rows[0][0] != "A" {
		t.Errorf("Expected A, got %s", rows[0][0])
	}
	if rows[0][1] != "1" {
		t.Errorf("Expected 1, got %s", rows[0][1])
	}
}
