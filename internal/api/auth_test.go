package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"grimm.is/glacic/internal/api/storage"
)

func TestNewMemoryAPIKeyStore(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()
	if store == nil {
		t.Fatal("expected non-nil store")
	}

	// Should start empty
	keys, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys, got %d", len(keys))
	}
}

func TestMemoryAPIKeyStore_CreateAndGet(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()

	key := &storage.APIKey{
		ID:          "test-id",
		Name:        "test-key",
		KeyHash:     "hash123",
		Permissions: []storage.Permission{storage.PermReadConfig},
		CreatedAt:   time.Now(),
	}

	err := store.Create(key)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Get by ID
	retrieved, err := store.Get("test-id")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if retrieved.Name != "test-key" {
		t.Errorf("expected name 'test-key', got %s", retrieved.Name)
	}

	// Get by hash
	retrieved, err = store.GetByHash("hash123")
	if err != nil {
		t.Fatalf("GetByHash failed: %v", err)
	}
	if retrieved.ID != "test-id" {
		t.Errorf("expected ID 'test-id', got %s", retrieved.ID)
	}

	// Get non-existent
	_, err = store.Get("non-existent")
	if err == nil {
		t.Error("expected error for non-existent key")
	}
}

func TestMemoryAPIKeyStore_Update(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()

	key := &storage.APIKey{
		ID:          "test-id",
		Name:        "original-name",
		KeyHash:     "hash123",
		Permissions: []storage.Permission{storage.PermReadConfig},
	}
	store.Create(key)

	// Update
	key.Name = "updated-name"
	err := store.Update(key)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	retrieved, _ := store.Get("test-id")
	if retrieved.Name != "updated-name" {
		t.Errorf("expected name 'updated-name', got %s", retrieved.Name)
	}

	// Update non-existent
	err = store.Update(&storage.APIKey{ID: "non-existent"})
	if err == nil {
		t.Error("expected error updating non-existent key")
	}
}

func TestMemoryAPIKeyStore_Delete(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()

	key := &storage.APIKey{
		ID:      "test-id",
		Name:    "test-key",
		KeyHash: "hash123",
	}
	store.Create(key)

	err := store.Delete("test-id")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = store.Get("test-id")
	if err == nil {
		t.Error("expected error for deleted key")
	}

	// Delete non-existent
	err = store.Delete("non-existent")
	if err == nil {
		t.Error("expected error deleting non-existent key")
	}
}

func TestMemoryAPIKeyStore_UpdateLastUsed(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()

	key := &storage.APIKey{
		ID:      "test-id",
		Name:    "test-key",
		KeyHash: "hash123",
	}
	store.Create(key)

	err := store.UpdateLastUsed("test-id")
	if err != nil {
		t.Fatalf("UpdateLastUsed failed: %v", err)
	}

	retrieved, _ := store.Get("test-id")
	if retrieved.LastUsedAt == nil {
		t.Error("expected LastUsedAt to be set")
	}
}

func TestMemoryAPIKeyStore_List(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()

	store.Create(&storage.APIKey{ID: "key1", Name: "key1", KeyHash: "h1"})
	store.Create(&storage.APIKey{ID: "key2", Name: "key2", KeyHash: "h2"})
	store.Create(&storage.APIKey{ID: "key3", Name: "key3", KeyHash: "h3"})

	keys, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(keys))
	}
}

func TestAPIKeyManager_GenerateKey(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()
	manager := NewAPIKeyManager(store)

	fullKey, apiKey, err := manager.GenerateKey("test-key", []storage.Permission{storage.PermReadConfig, storage.PermWriteConfig})
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	if fullKey == "" {
		t.Error("expected non-empty full key")
	}
	if apiKey.ID == "" {
		t.Error("expected non-empty ID")
	}
	if apiKey.Name != "test-key" {
		t.Errorf("expected name 'test-key', got %s", apiKey.Name)
	}
	if len(apiKey.Permissions) != 2 {
		t.Errorf("expected 2 permissions, got %d", len(apiKey.Permissions))
	}
}

func TestAPIKeyManager_ValidateKey(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()
	manager := NewAPIKeyManager(store)

	fullKey, _, err := manager.GenerateKey("test-key", []storage.Permission{storage.PermReadConfig})
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	// Valid key
	apiKey, err := manager.ValidateKey(fullKey)
	if err != nil {
		t.Fatalf("ValidateKey failed: %v", err)
	}
	if apiKey.Name != "test-key" {
		t.Errorf("expected name 'test-key', got %s", apiKey.Name)
	}

	// Invalid key
	_, err = manager.ValidateKey("invalid-key")
	if err == nil {
		t.Error("expected error for invalid key")
	}
}

func TestAPIKey_HasPermission(t *testing.T) {
	key := &storage.APIKey{
		Permissions: []storage.Permission{storage.PermReadConfig, storage.PermWriteConfig},
	}

	if !key.HasPermission(storage.PermReadConfig) {
		t.Error("expected HasPermission to return true for PermReadConfig")
	}
	if !key.HasPermission(storage.PermWriteConfig) {
		t.Error("expected HasPermission to return true for PermWriteConfig")
	}
	if key.HasPermission(storage.PermReadDHCP) {
		t.Error("expected HasPermission to return false for PermReadDHCP")
	}
}

func TestAPIKey_HasAnyPermission(t *testing.T) {
	key := &storage.APIKey{
		Permissions: []storage.Permission{storage.PermReadConfig},
	}

	if !key.HasAnyPermission(storage.PermReadConfig, storage.PermWriteConfig) {
		t.Error("expected HasAnyPermission to return true")
	}
	if key.HasAnyPermission(storage.PermWriteConfig, storage.PermReadDHCP) {
		t.Error("expected HasAnyPermission to return false when no matches")
	}
}

func TestAPIKey_WildcardPermissions(t *testing.T) {
	key := &storage.APIKey{
		Permissions: []storage.Permission{storage.PermAll},
	}

	// Wildcard should match everything
	if !key.HasPermission(storage.PermReadConfig) {
		t.Error("expected PermAll to match PermReadConfig")
	}
	if !key.HasPermission(storage.PermWriteConfig) {
		t.Error("expected PermAll to match PermWriteConfig")
	}
}

func TestWithExpiry(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()
	manager := NewAPIKeyManager(store)

	expiry := time.Now().Add(24 * time.Hour)
	_, apiKey, err := manager.GenerateKey("expiring-key", []storage.Permission{storage.PermReadConfig}, WithExpiry(expiry))
	if err != nil {
		t.Fatalf("GenerateKey with expiry failed: %v", err)
	}

	if apiKey.ExpiresAt == nil {
		t.Error("expected ExpiresAt to be set")
	}
}

func TestAPIKey_IsIPAllowed(t *testing.T) {
	// No restrictions
	key := &storage.APIKey{}
	if !key.IsIPAllowed("192.168.1.1") {
		t.Error("expected any IP to be allowed with no restrictions")
	}

	// With restrictions
	key = &storage.APIKey{AllowedIPs: []string{"10.0.0.1", "10.0.0.2"}}
	if !key.IsIPAllowed("10.0.0.1") {
		t.Error("expected allowed IP to pass")
	}
	if key.IsIPAllowed("192.168.1.1") {
		t.Error("expected disallowed IP to fail")
	}
}

func TestAPIKey_IsPathAllowed(t *testing.T) {
	// No restrictions
	key := &storage.APIKey{}
	if !key.IsPathAllowed("/api/anything") {
		t.Error("expected any path to be allowed with no restrictions")
	}

	// With restrictions
	key = &storage.APIKey{AllowedPaths: []string{"/api/config", "/api/status"}}
	if !key.IsPathAllowed("/api/config") {
		t.Error("expected allowed path to pass")
	}
	if !key.IsPathAllowed("/api/config/edit") {
		t.Error("expected path with allowed prefix to pass")
	}
	if key.IsPathAllowed("/api/admin") {
		t.Error("expected disallowed path to fail")
	}
}

func TestNewAuthMiddleware(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()
	manager := NewAPIKeyManager(store)
	mw := NewAuthMiddleware(manager)

	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}
}

func TestAuthMiddleware_SkipPaths(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()
	manager := NewAPIKeyManager(store)
	mw := NewAuthMiddleware(manager).SkipPaths("/custom/path")

	if len(mw.skipPaths) < 4 {
		t.Errorf("expected at least 4 skip paths, got %d", len(mw.skipPaths))
	}
}

func TestWithAPIKey_GetAPIKey(t *testing.T) {
	key := &storage.APIKey{ID: "test-id", Name: "test-key"}

	ctx := WithAPIKey(context.Background(), key)
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}

	retrieved := GetAPIKey(ctx)
	if retrieved == nil {
		t.Fatal("expected to retrieve key from context")
	}
	if retrieved.ID != "test-id" {
		t.Errorf("expected ID 'test-id', got %s", retrieved.ID)
	}
}

func TestGetAPIKey_Empty(t *testing.T) {
	ctx := context.Background()
	key := GetAPIKey(ctx)
	if key != nil {
		t.Error("expected nil key from empty context")
	}
}

func TestAPIKeyManager_AddKey(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()
	manager := NewAPIKeyManager(store)

	key := &storage.APIKey{
		ID:          "imported-id",
		Name:        "imported-key",
		KeyHash:     "somehash",
		Permissions: []storage.Permission{storage.PermReadConfig},
		Enabled:     true,
	}

	err := manager.AddKey(key)
	if err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	retrieved, err := store.Get("imported-id")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if retrieved.Name != "imported-key" {
		t.Errorf("expected name 'imported-key', got %s", retrieved.Name)
	}
}

func TestWithExpiryDuration(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()
	manager := NewAPIKeyManager(store)

	_, apiKey, err := manager.GenerateKey("temp-key", []storage.Permission{storage.PermReadConfig}, WithExpiryDuration(1*time.Hour))
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	if apiKey.ExpiresAt == nil {
		t.Error("expected ExpiresAt to be set")
	}

	// Should expire in about 1 hour
	if time.Until(*apiKey.ExpiresAt) > 70*time.Minute || time.Until(*apiKey.ExpiresAt) < 50*time.Minute {
		t.Errorf("expected expiry around 1 hour, got %v", time.Until(*apiKey.ExpiresAt))
	}
}

func TestWithAllowedIPs(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()
	manager := NewAPIKeyManager(store)

	_, apiKey, err := manager.GenerateKey("ip-restricted", []storage.Permission{storage.PermReadConfig}, WithAllowedIPs([]string{"10.0.0.1"}))
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	if len(apiKey.AllowedIPs) != 1 || apiKey.AllowedIPs[0] != "10.0.0.1" {
		t.Error("expected AllowedIPs to be set")
	}
}

func TestWithAllowedPaths(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()
	manager := NewAPIKeyManager(store)

	_, apiKey, err := manager.GenerateKey("path-restricted", []storage.Permission{storage.PermReadConfig}, WithAllowedPaths([]string{"/api/config"}))
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	if len(apiKey.AllowedPaths) != 1 || apiKey.AllowedPaths[0] != "/api/config" {
		t.Error("expected AllowedPaths to be set")
	}
}

func TestWithRateLimit(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()
	manager := NewAPIKeyManager(store)

	_, apiKey, err := manager.GenerateKey("rate-limited", []storage.Permission{storage.PermReadConfig}, WithRateLimit(100))
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	if apiKey.RateLimit != 100 {
		t.Errorf("expected RateLimit 100, got %d", apiKey.RateLimit)
	}
}

func TestWithDescription(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()
	manager := NewAPIKeyManager(store)

	_, apiKey, err := manager.GenerateKey("described", []storage.Permission{storage.PermReadConfig}, WithDescription("Test description"))
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	if apiKey.Description != "Test description" {
		t.Errorf("expected Description 'Test description', got %s", apiKey.Description)
	}
}

func TestValidateKey_Expired(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()
	manager := NewAPIKeyManager(store)

	// Create expired key
	expiredTime := time.Now().Add(-1 * time.Hour)
	fullKey, _, err := manager.GenerateKey("expired-key", []storage.Permission{storage.PermReadConfig}, WithExpiry(expiredTime))
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	_, err = manager.ValidateKey(fullKey)
	if err == nil {
		t.Error("expected error for expired key")
	}
}

func TestValidateKey_Disabled(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()
	manager := NewAPIKeyManager(store)

	fullKey, apiKey, err := manager.GenerateKey("disabled-key", []storage.Permission{storage.PermReadConfig})
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	// Disable the key
	apiKey.Enabled = false
	store.Update(apiKey)

	_, err = manager.ValidateKey(fullKey)
	if err == nil {
		t.Error("expected error for disabled key")
	}
}

func TestAPIKeyManager_ImportKey(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()
	manager := NewAPIKeyManager(store)

	// Import valid key
	err := manager.ImportKey("imported-key", "my-secret-key-123", []storage.Permission{storage.PermReadConfig})
	if err != nil {
		t.Fatalf("ImportKey failed: %v", err)
	}

	// Verify it exists in store
	// ID is first 16 chars of hash
	hash := sha256.Sum256([]byte("my-secret-key-123"))
	keyHash := hex.EncodeToString(hash[:])
	id := keyHash[:16]

	key, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get failed to retrieve imported key: %v", err)
	}
	if key.Name != "imported-key" {
		t.Errorf("Expected name 'imported-key', got %s", key.Name)
	}
	if key.KeyHash != keyHash {
		t.Error("Key hash mismatch")
	}

	// Verify we can validate it
	validated, err := manager.ValidateKey("my-secret-key-123")
	if err != nil {
		t.Fatalf("ValidateKey failed for imported key: %v", err)
	}
	if validated.ID != key.ID {
		t.Error("Validated key ID mismatch")
	}
}

func TestAPIKeyManager_ImportKey_Duplicate(t *testing.T) {
	store := storage.NewMemoryAPIKeyStore()
	manager := NewAPIKeyManager(store)

	err := manager.ImportKey("key1", "secret", []storage.Permission{storage.PermAll})
	if err != nil {
		t.Fatal(err)
	}

	// Import same key again (same secret = same ID)
	err = manager.ImportKey("key2", "secret", []storage.Permission{storage.PermAll})
	if err == nil {
		t.Error("Expected error for duplicate key")
	}
}
