package network

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestOUIBuilder_FetchAndParse(t *testing.T) {
	// Mock IEEE server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve fake OUI data
		fmt.Fprintln(w, "OUI/MA-L")
		fmt.Fprintln(w, "00-00-01   (hex)		XEROX CORPORATION")
		fmt.Fprintln(w, "000001     (base 16)		XEROX CORPORATION")
		fmt.Fprintln(w, "				M/S 105-50C")
		fmt.Fprintln(w, "				800 PHILLIPS ROAD")
		fmt.Fprintln(w, "				WEBSTER NY 14580")
		fmt.Fprintln(w, "				UNITED STATES")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "00-0A-95   (hex)		Apple Computer, Inc.")
		fmt.Fprintln(w, "000A95     (base 16)		Apple Computer, Inc.")
		fmt.Fprintln(w, "				20650 Valley Green Dr.")
		fmt.Fprintln(w, "				Cupertino CA 95014")
		fmt.Fprintln(w, "				UNITED STATES")
	}))
	defer server.Close()

	// Refactor BuildOUIDB to accept URLs?
	// Or we just test fetchAndParse directly if we export it or make it testable?
	// It is private: fetchAndParse.
	// But BuildOUIDB uses constants.
	// Let's modify the test to access the internal logic if possible, or just refactor oui_builder slightly to be testable?
	// Actually, Go tests in the same package can access private functions.

	db := &OUIDB{
		Entries: make(map[string]OUIEntry),
	}

	if err := fetchAndParse(server.URL, db); err != nil {
		t.Fatalf("fetchAndParse failed: %v", err)
	}

	// Verify entries
	tests := []struct {
		prefix string
		want   string
	}{
		{"000001", "XEROX CORPORATION"},
		{"000A95", "Apple Computer, Inc."},
	}

	for _, tt := range tests {
		entry, ok := db.Entries[tt.prefix]
		if !ok {
			t.Errorf("Prefix %s not found", tt.prefix)
			continue
		}
		if entry.Manufacturer != tt.want {
			t.Errorf("Prefix %s: want %q, got %q", tt.prefix, tt.want, entry.Manufacturer)
		}
	}
}

func TestOUIBuilder_RoundTrip(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "oui_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db := &OUIDB{
		Entries: map[string]OUIEntry{
			"AABBCC": {Manufacturer: "Test Corp", Country: "XX"},
		},
	}

	fpath := filepath.Join(tmpDir, "test.db.gz")

	// Save
	if err := db.Save(fpath); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load
	f, err := os.Open(fpath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	loadedDB, err := LoadCompactDB(f)
	if err != nil {
		t.Fatalf("LoadCompactDB failed: %v", err)
	}

	// Verify
	entry, ok := loadedDB.Entries["AABBCC"]
	if !ok {
		t.Fatal("Entry lost in round trip")
	}
	if entry.Manufacturer != "Test Corp" {
		t.Errorf("Mismatch: want 'Test Corp', got %q", entry.Manufacturer)
	}
}
