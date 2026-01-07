package storage

import (
	"encoding/json"
	"fmt"
	"grimm.is/glacic/internal/clock"

	"grimm.is/glacic/internal/state"
)

const (
	bucketKeys    = "api_keys"
	bucketKeyHash = "api_keys_hash"
)

// StateAPIKeyStore persists API keys using the state package.
type StateAPIKeyStore struct {
	store state.Store
}

// NewStateAPIKeyStore creates a new state-backed API key store.
func NewStateAPIKeyStore(store state.Store) (*StateAPIKeyStore, error) {
	// Ensure buckets exist
	if err := store.CreateBucket(bucketKeys); err != nil && err != state.ErrBucketExists {
		return nil, fmt.Errorf("failed to create keys bucket: %w", err)
	}
	if err := store.CreateBucket(bucketKeyHash); err != nil && err != state.ErrBucketExists {
		return nil, fmt.Errorf("failed to create key hash bucket: %w", err)
	}

	return &StateAPIKeyStore{store: store}, nil
}

func (s *StateAPIKeyStore) Create(key *APIKey) error {
	// Check if ID exists
	if _, err := s.store.Get(bucketKeys, key.ID); err == nil {
		return fmt.Errorf("API key already exists: %s", key.ID)
	}

	// Check if Hash exists
	if _, err := s.store.Get(bucketKeyHash, key.KeyHash); err == nil {
		return fmt.Errorf("API key hash already exists")
	}

	// Store Key
	if err := s.store.SetJSON(bucketKeys, key.ID, key); err != nil {
		return fmt.Errorf("failed to store key: %w", err)
	}

	// Store Hash Index
	if err := s.store.Set(bucketKeyHash, key.KeyHash, []byte(key.ID)); err != nil {
		// Rollback key storage? Ideally transaction, but state store usage implies eventual consistency or simple KV
		s.store.Delete(bucketKeys, key.ID)
		return fmt.Errorf("failed to store hash index: %w", err)
	}

	return nil
}

func (s *StateAPIKeyStore) Get(id string) (*APIKey, error) {
	var key APIKey
	if err := s.store.GetJSON(bucketKeys, id, &key); err != nil {
		if err == state.ErrNotFound {
			return nil, fmt.Errorf("API key not found: %s", id)
		}
		return nil, err
	}
	return &key, nil
}

func (s *StateAPIKeyStore) GetByHash(hash string) (*APIKey, error) {
	idBytes, err := s.store.Get(bucketKeyHash, hash)
	if err != nil {
		if err == state.ErrNotFound {
			return nil, fmt.Errorf("API key not found")
		}
		return nil, err
	}

	return s.Get(string(idBytes))
}

func (s *StateAPIKeyStore) Update(key *APIKey) error {
	// Get existing to check for hash change
	oldKey, err := s.Get(key.ID)
	if err != nil {
		return err
	}

	// If hash changed, update index
	if oldKey.KeyHash != key.KeyHash {
		// Remove old hash
		if err := s.store.Delete(bucketKeyHash, oldKey.KeyHash); err != nil {
			return fmt.Errorf("failed to delete old hash index: %w", err)
		}
		// Set new hash
		if err := s.store.Set(bucketKeyHash, key.KeyHash, []byte(key.ID)); err != nil {
			return fmt.Errorf("failed to set new hash index: %w", err)
		}
	}

	// Update key
	if err := s.store.SetJSON(bucketKeys, key.ID, key); err != nil {
		return fmt.Errorf("failed to update key: %w", err)
	}

	return nil
}

func (s *StateAPIKeyStore) Delete(id string) error {
	key, err := s.Get(id)
	if err != nil {
		return err
	}

	// Delete from index
	if err := s.store.Delete(bucketKeyHash, key.KeyHash); err != nil {
		// Continue to delete key anyway?
	}

	// Delete from store
	if err := s.store.Delete(bucketKeys, id); err != nil {
		return err
	}

	return nil
}

func (s *StateAPIKeyStore) List() ([]*APIKey, error) {
	entries, err := s.store.List(bucketKeys)
	if err != nil {
		return nil, err
	}

	keys := make([]*APIKey, 0, len(entries))
	for _, val := range entries {
		var key APIKey
		if err := json.Unmarshal(val, &key); err != nil {
			continue // Skip corrupted entries?
		}
		keys = append(keys, &key)
	}

	return keys, nil
}

func (s *StateAPIKeyStore) UpdateLastUsed(id string) error {
	// Fetch, update, save
	// This is expensive for a high-traffic API if done synchronously on every request using basic KV.
	// Optimally, we'd have lighter weight update or batching.
	// For now, implement as read-modify-write.
	// NOTE: This introduces race condition on concurrent updates, but LastUsedAt being slightly off is usually acceptable.

	key, err := s.Get(id)
	if err != nil {
		return err
	}

	now := clock.Now()
	key.LastUsedAt = &now
	key.UsageCount++

	return s.store.SetJSON(bucketKeys, key.ID, key)
}
