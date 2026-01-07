package firewall

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseIPList(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  []string
		expectErr bool
	}{
		{
			name: "Basic IPs",
			input: `192.168.1.1
10.0.0.1`,
			expected: []string{"192.168.1.1", "10.0.0.1"},
		},
		{
			name: "Comments and Empty Lines",
			input: `
# This is a comment
192.168.1.1 # Inline comment
   10.0.0.1
; Semicolon comment
`,
			expected: []string{"192.168.1.1", "10.0.0.1"},
		},
		{
			name: "CIDRs",
			input: `192.168.0.0/24
2001:db8::/32`,
			expected: []string{"192.168.0.0/24", "2001:db8::/32"},
		},
		{
			name:      "Invalid Lines",
			input:     `invalid-ip`,
			expected:  nil,
			expectErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ips, err := ParseIPList(strings.NewReader(tc.input))
			if (err != nil) != tc.expectErr {
				t.Fatalf("ParseIPList() error = %v, expectErr %v", err, tc.expectErr)
			}
			if tc.expectErr {
				return // If an error was expected and occurred, we're done.
			}

			// If no error was expected, proceed with checking the parsed IPs.
			if len(ips) != len(tc.expected) {
				t.Errorf("Expected %d IPs, got %d", len(tc.expected), len(ips))
			}
			for i, ip := range ips {
				if ip != tc.expected[i] {
					t.Errorf("Expected IP %s, got %s", tc.expected[i], ip)
				}
			}
		})
	}
}

func TestFireHOLManager_DownloadList(t *testing.T) {
	// Mock Server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "1.2.3.4")
		fmt.Fprintln(w, "5.6.7.8")
	}))
	defer ts.Close()

	tmpDir, err := ioutil.TempDir("", "firehol-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewFireHOLManager(tmpDir, nil)

	// Test DownloadFromURL
	ips, err := mgr.DownloadFromURL(ts.URL)
	if err != nil {
		t.Fatalf("DownloadFromURL failed: %v", err)
	}
	if len(ips) != 2 {
		t.Errorf("Expected 2 IPs, got %d", len(ips))
	}
	if ips[0] != "1.2.3.4" {
		t.Errorf("Unexpected IP: %s", ips[0])
	}

	// Verify Cache
	cacheFiles, _ := filepath.Glob(filepath.Join(tmpDir, "*.txt"))
	if len(cacheFiles) != 1 {
		t.Error("Cache file not created")
	}

	// Test UpdateIPSetFromURL (wrapper)
	count, err := mgr.UpdateIPSetFromURL("test-set", ts.URL)
	if err != nil {
		t.Fatalf("UpdateIPSetFromURL failed: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected count 2, got %d", count)
	}
}

func TestFireHOLManager_CacheLogic(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "firehol-cache-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewFireHOLManager(tmpDir, nil)

	// Let's rely on public API: UpdateIPSetFromURL with mock server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "8.8.8.8")
	}))
	defer ts.Close()

	_, err = mgr.DownloadFromURL(ts.URL)
	if err != nil {
		t.Fatal(err)
	}

	// Add test entry to WellKnownLists
	orig := WellKnownLists
	// We need to restore it
	defer func() { WellKnownLists = orig }()

	// Copy map to avoid modifying global state for other tests?
	// The variable is global, so we must be careful.
	// But since we are replacing the whole map var with `orig` at end, we can modify it.
	// However, we need to make sure we don't race if parallel tests.
	// These tests are not parallel marked.

	// Use a new map to be safeish if we replace it entirely?
	// WellKnownLists = map[string]FireHOLListInfo{...}
	// But it's initialized.

	WellKnownLists["test_mock"] = FireHOLListInfo{URL: ts.URL}

	// Use CacheList to populate cache by name
	_, err = mgr.CacheList("test_mock")
	if err != nil {
		t.Fatal(err)
	}

	if mgr.NeedsRefresh("test_mock", time.Hour) {
		t.Error("Should not need refresh immediately")
	}
}

func TestListAvailable(t *testing.T) {
	lists := ListAvailable()
	if len(lists) == 0 {
		t.Error("No lists available")
	}

	info, ok := GetListInfo("firehol_level1")
	if !ok {
		t.Error("firehol_level1 not found")
	}
	if info.Category != "attacks" {
		t.Error("Unexpected category")
	}
}
