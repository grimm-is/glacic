package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"grimm.is/glacic/internal/clock"
	"os"
	"path/filepath"
	"sync"

	"grimm.is/glacic/internal/config"
)

// FileAPIKeyStore persists API keys to a JSON file.
type FileAPIKeyStore struct {
	path   string
	keys   map[string]*APIKey // by ID
	byHash map[string]*APIKey // by KeyHash
	mu     sync.RWMutex
}

// NewFileAPIKeyStore creates a new file-based API key store.
func NewFileAPIKeyStore(path string) (*FileAPIKeyStore, error) {
	store := &FileAPIKeyStore{
		path:   path,
		keys:   make(map[string]*APIKey),
		byHash: make(map[string]*APIKey),
	}

	// Load existing keys if file exists
	if _, err := os.Stat(path); err == nil {
		if err := store.load(); err != nil {
			return nil, fmt.Errorf("failed to load API keys: %w", err)
		}
	}

	return store, nil
}

func (s *FileAPIKeyStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}

	var keys []*APIKey
	if err := json.Unmarshal(data, &keys); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, key := range keys {
		s.keys[key.ID] = key
		s.byHash[key.KeyHash] = key
	}

	return nil
}

// save persists keys to disk (takes RLock internally)
func (s *FileAPIKeyStore) save() error {
	s.mu.RLock()
	keys := make([]*APIKey, 0, len(s.keys))
	for _, key := range s.keys {
		keys = append(keys, key)
	}
	s.mu.RUnlock()

	return s.saveKeys(keys)
}

// saveUnlocked persists keys to disk (caller must hold lock)
func (s *FileAPIKeyStore) saveUnlocked() error {
	keys := make([]*APIKey, 0, len(s.keys))
	for _, key := range s.keys {
		keys = append(keys, key)
	}
	return s.saveKeys(keys)
}

// saveKeys writes the key slice to disk
func (s *FileAPIKeyStore) saveKeys(keys []*APIKey) error {
	data, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// Write atomically
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}

	return os.Rename(tmpPath, s.path)
}

func (s *FileAPIKeyStore) Get(id string) (*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key, ok := s.keys[id]
	if !ok {
		return nil, fmt.Errorf("API key not found: %s", id)
	}
	return key, nil
}

func (s *FileAPIKeyStore) GetByHash(hash string) (*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key, ok := s.byHash[hash]
	if !ok {
		return nil, fmt.Errorf("API key not found")
	}
	return key, nil
}

func (s *FileAPIKeyStore) List() ([]*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make([]*APIKey, 0, len(s.keys))
	for _, key := range s.keys {
		keys = append(keys, key)
	}
	return keys, nil
}

func (s *FileAPIKeyStore) Create(key *APIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.keys[key.ID]; exists {
		return fmt.Errorf("API key already exists: %s", key.ID)
	}

	s.keys[key.ID] = key
	s.byHash[key.KeyHash] = key

	return s.saveUnlocked()
}

func (s *FileAPIKeyStore) Update(key *APIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	old, exists := s.keys[key.ID]
	if !exists {
		return fmt.Errorf("API key not found: %s", key.ID)
	}

	// Remove old hash mapping if changed
	if old.KeyHash != key.KeyHash {
		delete(s.byHash, old.KeyHash)
		s.byHash[key.KeyHash] = key
	}

	s.keys[key.ID] = key

	return s.saveUnlocked()
}

func (s *FileAPIKeyStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, exists := s.keys[id]
	if !exists {
		return fmt.Errorf("API key not found: %s", id)
	}

	delete(s.byHash, key.KeyHash)
	delete(s.keys, id)

	return s.saveUnlocked()
}

func (s *FileAPIKeyStore) UpdateLastUsed(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, exists := s.keys[id]
	if !exists {
		return nil // Silently ignore
	}

	now := clock.Now()
	key.LastUsedAt = &now
	key.UsageCount++

	// Don't save on every request - too expensive
	// Could batch these updates
	return nil
}

// LoadKeysFromConfig loads API keys from the config file.
// These are merged with file-stored keys.
func (s *FileAPIKeyStore) LoadKeysFromConfig(cfg *config.APIConfig) error {
	if cfg == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, keyCfg := range cfg.Keys {
		// Hash the key
		hash := sha256.Sum256([]byte(keyCfg.Key))
		keyHash := hex.EncodeToString(hash[:])

		// Check if already exists
		if _, exists := s.byHash[keyHash]; exists {
			continue // Skip duplicates
		}

		// Convert permissions
		perms := make([]Permission, len(keyCfg.Permissions))
		for i, p := range keyCfg.Permissions {
			perms[i] = Permission(p)
		}

		// Generate ID from hash prefix
		id := keyHash[:16]

		key := &APIKey{
			ID:           id,
			Name:         keyCfg.Name,
			KeyHash:      keyHash,
			KeyPrefix:    keyCfg.Key[:min(12, len(keyCfg.Key))],
			Permissions:  perms,
			AllowedIPs:   keyCfg.AllowedIPs,
			AllowedPaths: keyCfg.AllowedPaths,
			RateLimit:    keyCfg.RateLimit,
			CreatedAt:    clock.Now(),
			Enabled:      keyCfg.Enabled,
			Description:  keyCfg.Description,
		}

		s.keys[id] = key
		s.byHash[keyHash] = key
	}

	// Also handle bootstrap key
	if cfg.BootstrapKey != "" {
		hash := sha256.Sum256([]byte(cfg.BootstrapKey))
		keyHash := hex.EncodeToString(hash[:])

		if _, exists := s.byHash[keyHash]; !exists {
			id := "bootstrap"
			key := &APIKey{
				ID:          id,
				Name:        "Bootstrap Key",
				KeyHash:     keyHash,
				KeyPrefix:   cfg.BootstrapKey[:min(12, len(cfg.BootstrapKey))],
				Permissions: []Permission{PermAll},
				CreatedAt:   clock.Now(),
				Enabled:     true,
				Description: "Initial bootstrap key - should be replaced with proper keys",
			}
			s.keys[id] = key
			s.byHash[keyHash] = key
		}
	}

	return nil
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// CombinedAPIKeyStore combines multiple stores (e.g., file + config).
type CombinedAPIKeyStore struct {
	primary   APIKeyStore // File store (read/write)
	secondary APIKeyStore // Config store (read-only)
}

// NewCombinedAPIKeyStore creates a store that checks multiple sources.
func NewCombinedAPIKeyStore(primary, secondary APIKeyStore) *CombinedAPIKeyStore {
	return &CombinedAPIKeyStore{
		primary:   primary,
		secondary: secondary,
	}
}

func (s *CombinedAPIKeyStore) Get(id string) (*APIKey, error) {
	if key, err := s.primary.Get(id); err == nil {
		return key, nil
	}
	return s.secondary.Get(id)
}

func (s *CombinedAPIKeyStore) GetByHash(hash string) (*APIKey, error) {
	if key, err := s.primary.GetByHash(hash); err == nil {
		return key, nil
	}
	return s.secondary.GetByHash(hash)
}

func (s *CombinedAPIKeyStore) List() ([]*APIKey, error) {
	keys := make(map[string]*APIKey)

	if primaryKeys, err := s.primary.List(); err == nil {
		for _, k := range primaryKeys {
			keys[k.ID] = k
		}
	}

	if secondaryKeys, err := s.secondary.List(); err == nil {
		for _, k := range secondaryKeys {
			if _, exists := keys[k.ID]; !exists {
				keys[k.ID] = k
			}
		}
	}

	result := make([]*APIKey, 0, len(keys))
	for _, k := range keys {
		result = append(result, k)
	}
	return result, nil
}

func (s *CombinedAPIKeyStore) Create(key *APIKey) error {
	return s.primary.Create(key)
}

func (s *CombinedAPIKeyStore) Update(key *APIKey) error {
	return s.primary.Update(key)
}

func (s *CombinedAPIKeyStore) Delete(id string) error {
	return s.primary.Delete(id)
}

func (s *CombinedAPIKeyStore) UpdateLastUsed(id string) error {
	// Try primary first
	if err := s.primary.UpdateLastUsed(id); err == nil {
		return nil
	}
	return s.secondary.UpdateLastUsed(id)
}
