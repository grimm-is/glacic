package firewall

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"grimm.is/glacic/internal/clock"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/state"
)

// IPSetService manages IPSet lifecycle, auto-updates, and metadata.
type IPSetService struct {
	tableName      string
	cacheDir       string
	fireholManager *FireHOLManager
	ipsetManager   *IPSetManager
	stateStore     state.Store
	logger         *logging.Logger
	mu             sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
}

// IPSetMetadata tracks cache information for IPSet lists.
type IPSetMetadata struct {
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	Source       string    `json:"source"` // "manual", "firehol", "url"
	SourceURL    string    `json:"source_url,omitempty"`
	LastUpdate   time.Time `json:"last_update"`
	NextUpdate   time.Time `json:"next_update"`
	EntriesCount int       `json:"entries_count"`
	ETag         string    `json:"etag,omitempty"`
	Checksum     string    `json:"checksum,omitempty"`
}

// NewIPSetService creates a new IPSet service.
func NewIPSetService(tableName, cacheDir string, stateStore state.Store, logger *logging.Logger) *IPSetService {
	return &IPSetService{
		ipsetManager:   NewIPSetManager(tableName),
		fireholManager: NewFireHOLManager(cacheDir, logger),
		stateStore:     stateStore,
		logger:         logger,
	}
}

// Start begins the IPSet service and background update routines.
func (s *IPSetService) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.logger.Info("IPSet service started")
	return nil
}

// Stop stops the IPSet service and background routines.
func (s *IPSetService) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}

	s.logger.Info("IPSet service stopped")
	return nil
}

// ApplyIPSets applies all IPSet configurations.
func (s *IPSetService) ApplyIPSets(cfg *config.Config) error {

	// Apply each IPSet configuration
	for _, ipset := range cfg.IPSets {
		if err := s.applyIPSet(&ipset); err != nil {
			return fmt.Errorf("failed to apply ipset %s: %w", ipset.Name, err)
		}
	}

	return nil
}

// applyIPSet applies a single IPSet configuration.
func (s *IPSetService) applyIPSet(ipset *config.IPSet) error {
	s.logger.Info("Applying IPSet", "name", ipset.Name, "type", ipset.Type)

	// Validate configuration
	if err := s.validateIPSet(ipset); err != nil {
		return err
	}

	// Create the nftables set
	setType := SetType(ipset.Type)
	if err := s.ipsetManager.CreateSet(ipset.Name, setType); err != nil {
		return fmt.Errorf("failed to create set: %w", err)
	}

	// Populate the set based on source
	var entries []string
	var source string
	var sourceURL string

	switch {
	case len(ipset.Entries) > 0:
		entries = ipset.Entries
		source = "manual"
	case ipset.FireHOLList != "":
		listEntries, err := s.fireholManager.DownloadList(ipset.FireHOLList)
		if err != nil {
			return fmt.Errorf("failed to download firehol list %s: %w", ipset.FireHOLList, err)
		}
		entries = listEntries
		source = "firehol"
		sourceURL = s.getFireHOLURL(ipset.FireHOLList)
	case ipset.URL != "":
		listEntries, err := s.fireholManager.DownloadFromURL(ipset.URL)
		if err != nil {
			return fmt.Errorf("failed to download from URL %s: %w", ipset.URL, err)
		}
		entries = listEntries
		source = "url"
		sourceURL = ipset.URL
	default:
		return fmt.Errorf("ipset %s has no entries, firehol_list, or url", ipset.Name)
	}

	// Add entries to the set
	if len(entries) > 0 {
		if err := s.ipsetManager.AddElements(ipset.Name, entries); err != nil {
			return fmt.Errorf("failed to add elements: %w", err)
		}
	}

	// Save metadata
	metadata := IPSetMetadata{
		Name:         ipset.Name,
		Type:         ipset.Type,
		Source:       source,
		SourceURL:    sourceURL,
		LastUpdate:   clock.Now(),
		EntriesCount: len(entries),
	}

	// Calculate next update time if auto-update is enabled (handled by external scheduler)
	if ipset.AutoUpdate && ipset.RefreshHours > 0 {
		metadata.NextUpdate = clock.Now().Add(time.Duration(ipset.RefreshHours) * time.Hour)
	}

	if err := s.saveMetadata(ipset.Name, metadata); err != nil {
		s.logger.Warn("Failed to save IPSet metadata", "ipset", ipset.Name, "error", err)
	}

	s.logger.Info("IPSet applied successfully",
		"name", ipset.Name,
		"entries", len(entries),
		"source", source,
		"auto_update", ipset.AutoUpdate)

	return nil
}

// updateIPSet updates an IPSet with fresh data from its source.
// This method handles:
// 1. Checking metadata to determine source (FireHOL vs URL)
// 2. Downloading fresh data (using FireHOLManager which handles caching/ETags)
// 3. Flushing the existing nftables set
// 4. Repopulating the set with new entries
// 5. Updating metadata with new stats and timestamp
func (s *IPSetService) updateIPSet(name string) error {
	// Get current metadata
	metadata, err := s.getMetadata(name)
	if err != nil {
		return fmt.Errorf("failed to get metadata: %w", err)
	}

	// Download fresh entries based on source
	var entries []string
	switch metadata.Source {
	case "firehol":
		entries, err = s.fireholManager.DownloadList(metadata.SourceURL)
	case "url":
		entries, err = s.fireholManager.DownloadFromURL(metadata.SourceURL)
	default:
		return fmt.Errorf("cannot auto-update manual ipset %s", name)
	}

	if err != nil {
		return fmt.Errorf("failed to download fresh entries: %w", err)
	}

	// Update the set atomically
	if err := s.ipsetManager.ReloadSet(name, entries); err != nil {
		return fmt.Errorf("failed to reload set atomically: %w", err)
	}

	// Update metadata
	metadata.LastUpdate = clock.Now()
	metadata.EntriesCount = len(entries)
	if err := s.saveMetadata(name, metadata); err != nil {
		s.logger.Warn("Failed to save updated metadata", "ipset", name, "error", err)
	}

	return nil
}

// validateIPSet validates IPSet configuration before applying.
func (s *IPSetService) validateIPSet(ipset *config.IPSet) error {
	if ipset.Name == "" {
		return fmt.Errorf("ipset name cannot be empty")
	}

	if ipset.Type == "" {
		return fmt.Errorf("ipset type cannot be empty")
	}

	// Validate set type
	validTypes := map[string]bool{
		"ipv4_addr": true, "ipv6_addr": true, "inet_service": true,
	}
	if !validTypes[ipset.Type] {
		return fmt.Errorf("invalid ipset type %s", ipset.Type)
	}

	// Validate source configuration
	sources := 0
	if len(ipset.Entries) > 0 {
		sources++
	}
	if ipset.FireHOLList != "" {
		sources++
		// Validate FireHOL list name
		if _, exists := WellKnownLists[ipset.FireHOLList]; !exists {
			return fmt.Errorf("unknown firehol list: %s", ipset.FireHOLList)
		}
	}
	if ipset.URL != "" {
		sources++
		// Basic URL validation
		if !isValidURL(ipset.URL) {
			return fmt.Errorf("invalid URL: %s", ipset.URL)
		}
	}

	if sources == 0 {
		return fmt.Errorf("ipset %s must have entries, firehol_list, or url", ipset.Name)
	}
	if sources > 1 {
		return fmt.Errorf("ipset %s cannot have multiple sources", ipset.Name)
	}

	// Validate auto-update configuration
	if ipset.AutoUpdate && ipset.RefreshHours <= 0 {
		return fmt.Errorf("auto_update requires refresh_hours > 0")
	}

	// Test network connectivity for remote sources
	if ipset.FireHOLList != "" || ipset.URL != "" {
		if err := s.testConnectivity(ipset.FireHOLList, ipset.URL); err != nil {
			return fmt.Errorf("network connectivity test failed: %w", err)
		}
	}

	return nil
}

// testConnectivity tests network connectivity to FireHOL or custom URL.
func (s *IPSetService) testConnectivity(fireholList, url string) error {
	var testURL string
	if fireholList != "" {
		testURL = s.getFireHOLURL(fireholList)
	} else if url != "" {
		testURL = url
	} else {
		return nil
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("HEAD", testURL, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return nil
}

// getFireHOLURL returns the URL for a FireHOL list name.
func (s *IPSetService) getFireHOLURL(listName string) string {
	if listInfo, exists := WellKnownLists[listName]; exists {
		return listInfo.URL
	}
	return ""
}

// saveMetadata saves IPSet metadata to the state store.
func (s *IPSetService) saveMetadata(name string, metadata IPSetMetadata) error {
	bucket := "ipset_metadata"
	key := name
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return s.stateStore.Set(bucket, key, data)
}

// getMetadata retrieves IPSet metadata from the state store.
func (s *IPSetService) getMetadata(name string) (IPSetMetadata, error) {
	var metadata IPSetMetadata
	bucket := "ipset_metadata"
	key := name
	data, err := s.stateStore.Get(bucket, key)
	if err != nil {
		return metadata, err
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return metadata, err
	}
	return metadata, nil
}

// ListIPSets returns all IPSet metadata.
func (s *IPSetService) ListIPSets() ([]IPSetMetadata, error) {
	// Get all keys with ipset_metadata_ prefix
	keys, err := s.stateStore.ListKeys("ipset_metadata")
	if err != nil {
		return nil, err
	}

	var metadatas []IPSetMetadata
	for _, key := range keys {
		data, err := s.stateStore.Get("ipset_metadata", key)
		if err != nil {
			s.logger.Warn("Failed to get IPSet metadata", "key", key, "error", err)
			continue
		}
		var metadata IPSetMetadata
		if err := json.Unmarshal(data, &metadata); err != nil {
			s.logger.Warn("Failed to unmarshal IPSet metadata", "key", key, "error", err)
			continue
		}
		metadatas = append(metadatas, metadata)
	}

	return metadatas, nil
}

// ForceUpdate forces an immediate update of an IPSet.
func (s *IPSetService) ForceUpdate(name string) error {
	return s.updateIPSet(name)
}

// GetMetadata retrieves IPSet metadata from the state store.
func (s *IPSetService) GetMetadata(name string) (IPSetMetadata, error) {
	return s.getMetadata(name)
}

// ClearCache clears the FireHOL cache.
func (s *IPSetService) ClearCache() error {
	return s.fireholManager.ClearCache()
}

// GetCacheInfo returns cache information.
func (s *IPSetService) GetCacheInfo() (map[string]interface{}, error) {
	return s.fireholManager.GetCacheInfo()
}

// AddEntry adds a single entry to an IPSet.
func (s *IPSetService) AddEntry(setName, entry string) error {
	return s.ipsetManager.AddElements(setName, []string{entry})
}

// RemoveEntry removes a single entry from an IPSet.
func (s *IPSetService) RemoveEntry(setName, entry string) error {
	return s.ipsetManager.RemoveElements(setName, []string{entry})
}

// CheckEntry checks if an entry exists in an IPSet using O(1) nft get element.
func (s *IPSetService) CheckEntry(setName, entry string) (bool, error) {
	return s.ipsetManager.CheckElement(setName, entry)
}

// GetSetElements returns all elements in an IPSet.
func (s *IPSetService) GetSetElements(setName string) ([]string, error) {
	return s.ipsetManager.GetSetElements(setName)
}

// GetIPSetManager returns the underlying IPSetManager.
// Useful for advanced usage or testing.
func (s *IPSetService) GetIPSetManager() *IPSetManager {
	return s.ipsetManager
}

// isValidURL performs basic URL validation.
func isValidURL(url string) bool {
	return len(url) > 7 && (url[:7] == "http://" || url[:8] == "https://")
}
