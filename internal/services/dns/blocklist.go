package dns

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultBlocklistTimeout is the HTTP timeout for blocklist downloads
	DefaultBlocklistTimeout = 30 * time.Second
	// MaxBlocklistSize is the maximum size we'll download (10MB)
	MaxBlocklistSize = 10 * 1024 * 1024
)

// DownloadBlocklist fetches a blocklist from URL and parses it
// Supports both hosts-file format (0.0.0.0 domain) and plain domain lists
func DownloadBlocklist(url string) ([]string, error) {
	return DownloadBlocklistWithTimeout(url, int(DefaultBlocklistTimeout.Milliseconds()))
}

// DownloadBlocklistWithTimeout fetches a blocklist with a custom timeout in milliseconds
func DownloadBlocklistWithTimeout(url string, timeoutMs int) ([]string, error) {
	client := &http.Client{
		Timeout: time.Duration(timeoutMs) * time.Millisecond,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch blocklist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("blocklist server returned status %d", resp.StatusCode)
	}

	// Limit body size to prevent memory exhaustion
	// Mitigation: OWASP A05:2021-Security Misconfiguration
	limitedReader := io.LimitReader(resp.Body, MaxBlocklistSize)

	return parseBlocklist(limitedReader)
}

// DownloadBlocklistWithCache downloads a blocklist, falling back to cache on failure
func DownloadBlocklistWithCache(url, cachePath string) ([]string, error) {
	// Try to download
	domains, err := DownloadBlocklist(url)
	if err == nil {
		// Cache the successful download
		if cacheErr := CacheBlocklist(cachePath, url, domains); cacheErr != nil {
			log.Printf("[DNS] Warning: failed to cache blocklist: %v", cacheErr)
		}
		return domains, nil
	}

	log.Printf("[DNS] Failed to download blocklist, trying cache: %v", err)

	// Fallback to cache
	cached, cacheErr := LoadCachedBlocklist(cachePath, url)
	if cacheErr != nil {
		return nil, fmt.Errorf("download failed (%v) and no cache available (%v)", err, cacheErr)
	}

	log.Printf("[DNS] Loaded %d domains from cache for %s", len(cached), url)
	return cached, nil
}

// parseBlocklist parses both hosts-file format and plain domain lists
func parseBlocklist(r io.Reader) ([]string, error) {
	var domains []string
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Handle inline comments
		if idx := strings.Index(line, "#"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		if len(parts) >= 2 {
			// Hosts file format: IP domain [domain2...]
			// First field is IP (0.0.0.0, 127.0.0.1, etc.)
			// Skip localhost entries
			for _, domain := range parts[1:] {
				domain = strings.ToLower(domain)
				if domain != "localhost" && domain != "localhost.localdomain" {
					domains = append(domains, domain)
				}
			}
		} else {
			// Plain domain format
			domain := strings.ToLower(parts[0])
			if domain != "localhost" && domain != "localhost.localdomain" {
				domains = append(domains, domain)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading blocklist: %w", err)
	}

	return domains, nil
}

// urlToFilename converts a URL to a safe cache filename
func urlToFilename(url string) string {
	hash := sha256.Sum256([]byte(url))
	return hex.EncodeToString(hash[:8]) + ".txt"
}

// CacheBlocklist saves a blocklist to disk
func CacheBlocklist(cachePath, url string, domains []string) error {
	if err := os.MkdirAll(cachePath, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	filename := filepath.Join(cachePath, urlToFilename(url))
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create cache file: %w", err)
	}
	defer f.Close()

	for _, domain := range domains {
		if _, err := fmt.Fprintln(f, domain); err != nil {
			return fmt.Errorf("failed to write cache: %w", err)
		}
	}

	return nil
}

// LoadCachedBlocklist loads a blocklist from cache
func LoadCachedBlocklist(cachePath, url string) ([]string, error) {
	filename := filepath.Join(cachePath, urlToFilename(url))
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("cache not found: %w", err)
	}
	defer f.Close()

	return parseBlocklist(f)
}
