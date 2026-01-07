package storage

import (
	"fmt"
	"grimm.is/glacic/internal/clock"
	"sync"
)

// MemoryAPIKeyStore is an in-memory implementation of APIKeyStore.
type MemoryAPIKeyStore struct {
	keys   map[string]*APIKey // by ID
	byHash map[string]*APIKey // by KeyHash
	mu     sync.RWMutex
}

// NewMemoryAPIKeyStore creates a new in-memory API key store.
func NewMemoryAPIKeyStore() *MemoryAPIKeyStore {
	return &MemoryAPIKeyStore{
		keys:   make(map[string]*APIKey),
		byHash: make(map[string]*APIKey),
	}
}

func (s *MemoryAPIKeyStore) Get(id string) (*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key, ok := s.keys[id]
	if !ok {
		return nil, fmt.Errorf("API key not found: %s", id)
	}
	return key, nil
}

func (s *MemoryAPIKeyStore) GetByHash(hash string) (*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key, ok := s.byHash[hash]
	if !ok {
		return nil, fmt.Errorf("API key not found")
	}
	return key, nil
}

func (s *MemoryAPIKeyStore) List() ([]*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make([]*APIKey, 0, len(s.keys))
	for _, key := range s.keys {
		keys = append(keys, key)
	}
	return keys, nil
}

func (s *MemoryAPIKeyStore) Create(key *APIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.keys[key.ID]; exists {
		return fmt.Errorf("API key already exists: %s", key.ID)
	}

	s.keys[key.ID] = key
	s.byHash[key.KeyHash] = key
	return nil
}

func (s *MemoryAPIKeyStore) Update(key *APIKey) error {
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
	return nil
}

func (s *MemoryAPIKeyStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, exists := s.keys[id]
	if !exists {
		return fmt.Errorf("API key not found: %s", id)
	}

	delete(s.byHash, key.KeyHash)
	delete(s.keys, id)
	return nil
}

func (s *MemoryAPIKeyStore) UpdateLastUsed(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, exists := s.keys[id]
	if !exists {
		return nil // Silently ignore
	}

	now := clock.Now()
	key.LastUsedAt = &now
	key.UsageCount++
	return nil
}
