package firewall

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"grimm.is/glacic/internal/clock"
	"grimm.is/glacic/internal/logging"
)

// FireHOLManager handles downloading and managing FireHOL IP lists.
type FireHOLManager struct {
	cacheDir string
	logger   *logging.Logger
}

// FireHOLListInfo contains metadata about a FireHOL list.
type FireHOLListInfo struct {
	Name        string
	URL         string
	Description string
	Category    string
	Entries     int
}

// Well-known FireHOL lists from https://iplists.firehol.org/
var WellKnownLists = map[string]FireHOLListInfo{
	// Level 1: Safe for production use
	"firehol_level1": {
		Name:        "firehol_level1",
		URL:         "https://iplists.firehol.org/files/firehol_level1.netset",
		Description: "Attacks, malware, during the last 48 hours",
		Category:    "attacks",
	},
	// Level 2: More aggressive blocking
	"firehol_level2": {
		Name:        "firehol_level2",
		URL:         "https://iplists.firehol.org/files/firehol_level2.netset",
		Description: "Attacks, malware, spyware, during the last 48 hours",
		Category:    "attacks",
	},
	// Level 3: Even more aggressive
	"firehol_level3": {
		Name:        "firehol_level3",
		URL:         "https://iplists.firehol.org/files/firehol_level3.netset",
		Description: "Attacks, during the last 30 days",
		Category:    "attacks",
	},
	// Spamhaus DROP
	"spamhaus_drop": {
		Name:        "spamhaus_drop",
		URL:         "https://iplists.firehol.org/files/spamhaus_drop.netset",
		Description: "Spamhaus Don't Route Or Peer list",
		Category:    "spam",
	},
	// Spamhaus EDROP
	"spamhaus_edrop": {
		Name:        "spamhaus_edrop",
		URL:         "https://iplists.firehol.org/files/spamhaus_edrop.netset",
		Description: "Spamhaus Extended DROP list",
		Category:    "spam",
	},
	// DShield
	"dshield": {
		Name:        "dshield",
		URL:         "https://iplists.firehol.org/files/dshield.netset",
		Description: "DShield top attacking IPs",
		Category:    "attacks",
	},
	// Blocklist.de
	"blocklist_de": {
		Name:        "blocklist_de",
		URL:         "https://iplists.firehol.org/files/blocklist_de.ipset",
		Description: "Blocklist.de all attacks",
		Category:    "attacks",
	},
	// Emerging Threats
	"et_compromised": {
		Name:        "et_compromised",
		URL:         "https://iplists.firehol.org/files/et_compromised.ipset",
		Description: "Emerging Threats compromised IPs",
		Category:    "compromised",
	},
	// Feodo Tracker (banking trojans)
	"feodo": {
		Name:        "feodo",
		URL:         "https://iplists.firehol.org/files/feodo.ipset",
		Description: "Feodo Tracker botnet C&C servers",
		Category:    "botnet",
	},
	// Binary Defense
	"binarydefense": {
		Name:        "binarydefense",
		URL:         "https://iplists.firehol.org/files/binarydefense.ipset",
		Description: "Binary Defense malicious IPs",
		Category:    "attacks",
	},
	// Abuse.ch SSL Blacklist
	"sslbl": {
		Name:        "sslbl",
		URL:         "https://iplists.firehol.org/files/sslbl.ipset",
		Description: "Abuse.ch SSL Blacklist",
		Category:    "malware",
	},
	// TOR exit nodes (for blocking TOR access)
	"tor_exits": {
		Name:        "tor_exits",
		URL:         "https://iplists.firehol.org/files/tor_exits.ipset",
		Description: "TOR exit nodes",
		Category:    "anonymizers",
	},
	// Full bogons (unallocated IP space)
	"fullbogons": {
		Name:        "fullbogons",
		URL:         "https://iplists.firehol.org/files/fullbogons.netset",
		Description: "Full bogons - unallocated IPv4 space",
		Category:    "bogons",
	},
}

// NewFireHOLManager creates a new FireHOL manager.
func NewFireHOLManager(cacheDir string, logger *logging.Logger) *FireHOLManager {
	if logger == nil {
		logger = logging.New(logging.DefaultConfig())
	}
	return &FireHOLManager{
		cacheDir: cacheDir,
		logger:   logger,
	}
}

// GetListURL returns the URL for a FireHOL list by name.
func GetListURL(listName string) (string, error) {
	if info, ok := WellKnownLists[listName]; ok {
		return info.URL, nil
	}
	// Try constructing URL for unknown lists
	return fmt.Sprintf("https://iplists.firehol.org/files/%s.netset", listName), nil
}

// DownloadList downloads a FireHOL list and returns the IPs.
// Uses caching to avoid repeated downloads.
func (m *FireHOLManager) DownloadList(listName string) ([]string, error) {
	url, err := GetListURL(listName)
	if err != nil {
		return nil, err
	}
	return m.DownloadFromURL(url)
}

// DownloadFromURL downloads an IP list from any URL with caching support.
func (m *FireHOLManager) DownloadFromURL(url string) ([]string, error) {
	// Generate cache key from URL
	cacheKey := m.generateCacheKey(url)

	// Try to load from cache first
	if ips, err := m.loadFromCache(cacheKey); err == nil {
		return ips, nil
	}

	// Download fresh data
	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download %s: status %d", url, resp.StatusCode)
	}

	var reader io.Reader = resp.Body

	// Handle gzip-compressed responses
	if strings.HasSuffix(url, ".gz") || resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzReader.Close()
		reader = gzReader
	}

	// Limit reader to 10MB to prevent memory exhaustion (DoS)
	limitReader := io.LimitReader(reader, 10*1024*1024)

	// Read into memory (up to limit) so we can parse AND cache it
	data, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse the list
	ips, err := ParseIPList(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to parse IP list: %w", err)
	}

	// Save to cache
	if err := m.saveToCache(cacheKey, data, resp.Header.Get("ETag")); err != nil {
		// Log warning but don't fail the operation
		m.logger.Warn("Failed to cache list", "url", url, "error", err)
	}

	return ips, nil
}

// generateCacheKey creates a SHA256 hash from URL for cache filename.
func (m *FireHOLManager) generateCacheKey(url string) string {
	hash := sha256.Sum256([]byte(url))
	return hex.EncodeToString(hash[:])
}

// saveToCache saves downloaded data to cache with metadata.
func (m *FireHOLManager) saveToCache(cacheKey string, data []byte, etag string) error {
	if m.cacheDir == "" {
		m.cacheDir = "/var/cache/firewall/iplists"
	}

	// Create cache directory
	if err := os.MkdirAll(m.cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache dir: %w", err)
	}

	// Save the data file
	dataPath := filepath.Join(m.cacheDir, cacheKey+".txt")
	if err := os.WriteFile(dataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write data cache: %w", err)
	}

	// Save metadata file
	metadata := map[string]interface{}{
		"cached_at": clock.Now().Unix(),
		"etag":      etag,
		"size":      len(data),
		"checksum":  m.calculateChecksum(data),
	}

	metadataData, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	metadataPath := filepath.Join(m.cacheDir, cacheKey+".meta")
	if err := os.WriteFile(metadataPath, metadataData, 0644); err != nil {
		return fmt.Errorf("failed to write metadata cache: %w", err)
	}

	return nil
}

// loadFromCache loads data from cache if valid.
func (m *FireHOLManager) loadFromCache(cacheKey string) ([]string, error) {
	if m.cacheDir == "" {
		m.cacheDir = "/var/cache/firewall/iplists"
	}

	dataPath := filepath.Join(m.cacheDir, cacheKey+".txt")
	metadataPath := filepath.Join(m.cacheDir, cacheKey+".meta")

	// Check if cache files exist
	if _, err := os.Stat(dataPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("cache miss")
	}
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("cache metadata miss")
	}

	// Load and validate metadata
	metadataData, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal(metadataData, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	// Check cache age (max 24 hours by default)
	cachedAt, ok := metadata["cached_at"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid cache timestamp")
	}

	cacheAge := time.Since(time.Unix(int64(cachedAt), 0))
	if cacheAge > 24*time.Hour {
		return nil, fmt.Errorf("cache expired")
	}

	// Load data
	data, err := os.ReadFile(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read data: %w", err)
	}

	// Verify checksum
	if expectedChecksum, ok := metadata["checksum"].(string); ok {
		actualChecksum := m.calculateChecksum(data)
		if actualChecksum != expectedChecksum {
			return nil, fmt.Errorf("cache checksum mismatch")
		}
	}

	ips, err := ParseIPList(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to parse cached list: %w", err)
	}
	return ips, nil
}

// calculateChecksum calculates SHA256 checksum of data.
func (m *FireHOLManager) calculateChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// ClearCache removes all cached files.
func (m *FireHOLManager) ClearCache() error {
	if m.cacheDir == "" {
		return nil
	}

	return os.RemoveAll(m.cacheDir)
}

// GetCacheInfo returns information about cached lists.
func (m *FireHOLManager) GetCacheInfo() (map[string]interface{}, error) {
	if m.cacheDir == "" {
		return map[string]interface{}{"cached_lists": 0}, nil
	}

	files, err := os.ReadDir(m.cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]interface{}{"cached_lists": 0}, nil
		}
		return nil, err
	}

	cachedLists := 0
	totalSize := int64(0)

	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".txt") {
			cachedLists++
			info, err := file.Info()
			if err == nil {
				totalSize += info.Size()
			}
		}
	}

	return map[string]interface{}{
		"cached_lists": cachedLists,
		"total_size":   totalSize,
		"cache_dir":    m.cacheDir,
	}, nil
}

// CacheList downloads and caches a list locally.
func (m *FireHOLManager) CacheList(listName string) (string, error) {
	if m.cacheDir == "" {
		m.cacheDir = "/var/cache/firewall/iplists"
	}

	// Create cache directory
	if err := os.MkdirAll(m.cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache dir: %w", err)
	}

	cachePath := filepath.Join(m.cacheDir, listName+".txt")

	ips, err := m.DownloadList(listName)
	if err != nil {
		return "", err
	}

	// Write to cache file
	if err := os.WriteFile(cachePath, []byte(strings.Join(ips, "\n")), 0644); err != nil {
		return "", fmt.Errorf("failed to write cache: %w", err)
	}

	return cachePath, nil
}

// LoadCachedList loads a list from the local cache.
func (m *FireHOLManager) LoadCachedList(listName string) ([]string, time.Time, error) {
	if m.cacheDir == "" {
		m.cacheDir = "/var/cache/firewall/iplists"
	}

	cachePath := filepath.Join(m.cacheDir, listName+".txt")

	info, err := os.Stat(cachePath)
	if err != nil {
		return nil, time.Time{}, err
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, time.Time{}, err
	}

	ips, err := ParseIPList(bytes.NewReader(data))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to parse cached list: %w", err)
	}
	return ips, info.ModTime(), nil
}

// NeedsRefresh checks if a cached list needs to be refreshed.
func (m *FireHOLManager) NeedsRefresh(listName string, maxAge time.Duration) bool {
	_, modTime, err := m.LoadCachedList(listName)
	if err != nil {
		return true // Cache miss, needs refresh
	}
	return time.Since(modTime) > maxAge
}

// UpdateIPSet downloads a list and returns the IPs.
// Note: This method only downloads and caches the data. Set operations should be handled by IPSetManager.
func (m *FireHOLManager) UpdateIPSet(setName, listName string) (int, error) {
	ips, err := m.DownloadList(listName)
	if err != nil {
		return 0, err
	}

	return len(ips), nil
}

// UpdateIPSetFromList downloads from a FireHOL list and returns the IPs.
// Note: This method only downloads and caches the data. Set operations should be handled by IPSetManager.
func (m *FireHOLManager) UpdateIPSetFromList(setName, listName string) (int, error) {
	ips, err := m.DownloadList(listName)
	if err != nil {
		return 0, err
	}

	return len(ips), nil
}

// UpdateIPSetFromURL downloads from a custom URL and returns the IPs.
// Note: This method only downloads and caches the data. Set operations should be handled by IPSetManager.
func (m *FireHOLManager) UpdateIPSetFromURL(setName, url string) (int, error) {
	ips, err := m.DownloadFromURL(url)
	if err != nil {
		return 0, err
	}

	return len(ips), nil
}

// ListAvailable returns information about all well-known FireHOL lists.
func ListAvailable() []FireHOLListInfo {
	var lists []FireHOLListInfo
	for _, info := range WellKnownLists {
		lists = append(lists, info)
	}
	return lists
}

// GetListInfo returns information about a specific list.
func GetListInfo(listName string) (FireHOLListInfo, bool) {
	info, ok := WellKnownLists[listName]
	return info, ok
}
