package dns

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// TestBlocklistDownload_HostsFormat tests downloading and parsing hosts-file format
func TestBlocklistDownload_HostsFormat(t *testing.T) {
	// Create mock HTTP server with hosts-file format blocklist
	hostsContent := `# Comment line
0.0.0.0 ads.example.com
0.0.0.0 tracker.example.com
# Another comment
127.0.0.1 malware.example.com
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, hostsContent)
	}))
	defer server.Close()

	// Download and parse
	domains, err := DownloadBlocklist(server.URL)
	if err != nil {
		t.Fatalf("DownloadBlocklist failed: %v", err)
	}

	// Verify expected domains
	expected := []string{"ads.example.com", "tracker.example.com", "malware.example.com"}
	if len(domains) != len(expected) {
		t.Errorf("Expected %d domains, got %d", len(expected), len(domains))
	}

	for _, exp := range expected {
		found := false
		for _, d := range domains {
			if d == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected domain %s not found in result", exp)
		}
	}
}

// TestBlocklistDownload_DomainListFormat tests downloading plain domain list format
func TestBlocklistDownload_DomainListFormat(t *testing.T) {
	domainContent := `# Adblock list
ads.example.com
tracker.example.com
analytics.example.com
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, domainContent)
	}))
	defer server.Close()

	domains, err := DownloadBlocklist(server.URL)
	if err != nil {
		t.Fatalf("DownloadBlocklist failed: %v", err)
	}

	if len(domains) != 3 {
		t.Errorf("Expected 3 domains, got %d: %v", len(domains), domains)
	}
}

// TestBlocklistCache_SaveAndLoad tests caching blocklists to disk
func TestBlocklistCache_SaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	cachePath := filepath.Join(tempDir, "blocklist_cache")

	domains := []string{"blocked1.com", "blocked2.com", "blocked3.com"}
	url := "http://example.com/blocklist.txt"

	// Save to cache
	err := CacheBlocklist(cachePath, url, domains)
	if err != nil {
		t.Fatalf("CacheBlocklist failed: %v", err)
	}

	// Load from cache
	loaded, err := LoadCachedBlocklist(cachePath, url)
	if err != nil {
		t.Fatalf("LoadCachedBlocklist failed: %v", err)
	}

	if len(loaded) != len(domains) {
		t.Errorf("Expected %d domains, got %d", len(domains), len(loaded))
	}

	for i, d := range domains {
		if loaded[i] != d {
			t.Errorf("Domain mismatch at %d: expected %s, got %s", i, d, loaded[i])
		}
	}
}

// TestBlocklistDownload_FallbackToCache tests using cache when server is down
func TestBlocklistDownload_FallbackToCache(t *testing.T) {
	tempDir := t.TempDir()
	cachePath := filepath.Join(tempDir, "blocklist_cache")
	url := "http://definitely-not-a-real-server.invalid/blocklist.txt"

	// Pre-populate cache
	domains := []string{"cached1.com", "cached2.com"}
	err := CacheBlocklist(cachePath, url, domains)
	if err != nil {
		t.Fatalf("Failed to setup cache: %v", err)
	}

	// Try to download (should fail) and fallback to cache
	loaded, err := DownloadBlocklistWithCache(url, cachePath)
	if err != nil {
		t.Fatalf("DownloadBlocklistWithCache failed: %v", err)
	}

	if len(loaded) != len(domains) {
		t.Errorf("Expected %d cached domains, got %d", len(domains), len(loaded))
	}
}

// TestBlocklistDownload_HTTPError tests handling of HTTP errors
func TestBlocklistDownload_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := DownloadBlocklist(server.URL)
	if err == nil {
		t.Error("Expected error for HTTP 500, got nil")
	}
}

// TestBlocklistDownload_Timeout tests timeout handling
func TestBlocklistDownload_Timeout(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Response slowly, but check for test completion
		select {
		case <-done:
			return
		case <-r.Context().Done():
			return
		}
	}))
	defer func() {
		close(done)
		server.Close()
	}()

	// This should timeout relatively quickly (100ms)
	_, err := DownloadBlocklistWithTimeout(server.URL, 100)
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
}

// TestBlocklistDownload_SizeLimit tests size limiting
func TestBlocklistDownload_SizeLimit(t *testing.T) {
	// Create a large response
	largeContent := strings.Repeat("0.0.0.0 big"+strings.Repeat("x", 100)+".com\n", 10000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, largeContent)
	}))
	defer server.Close()

	// Should handle large lists gracefully
	domains, err := DownloadBlocklist(server.URL)
	if err != nil {
		t.Fatalf("DownloadBlocklist failed on large list: %v", err)
	}

	// Just verify we got some domains
	if len(domains) == 0 {
		t.Error("Expected some domains from large list")
	}
}
