package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileAPIKeyStore_Persistence(t *testing.T) {
	// Create temp dir
	tmpDir, err := os.MkdirTemp("", "api_keys")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "keys.json")

	// 1. Create store and add key
	store1, err := NewFileAPIKeyStore(dbPath)
	if err != nil {
		t.Fatalf("NewFileAPIKeyStore failed: %v", err)
	}

	key := &APIKey{
		ID:          "persist-id",
		Name:        "persist-key",
		KeyHash:     "hash123",
		Permissions: []Permission{PermReadConfig},
		CreatedAt:   time.Now(),
		Enabled:     true,
	}

	if err := store1.Create(key); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 2. Open new store instance from same file
	store2, err := NewFileAPIKeyStore(dbPath)
	if err != nil {
		t.Fatalf("NewFileAPIKeyStore (2) failed: %v", err)
	}

	// 3. Verify key exists
	retrieved, err := store2.Get("persist-id")
	if err != nil {
		t.Fatalf("Get failed after reload: %v", err)
	}

	if retrieved.Name != "persist-key" {
		t.Errorf("Expected name 'persist-key', got %s", retrieved.Name)
	}
}

func TestFileAPIKeyStore_InvalidFile(t *testing.T) {
	// Create temp dir
	tmpDir, err := os.MkdirTemp("", "api_keys_invalid")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "corrupt.json")

	// Write garbage data
	if err := os.WriteFile(dbPath, []byte("{invalid-json"), 0600); err != nil {
		t.Fatalf("Failed to write corrupt file: %v", err)
	}

	// Try to open
	_, err = NewFileAPIKeyStore(dbPath)
	if err == nil {
		t.Error("Expected error opening corrupt file, got nil")
	}
}

func TestFileAPIKeyStore_ConcurrentAccess(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "api_keys_conc")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "keys.json")
	store, _ := NewFileAPIKeyStore(dbPath)

	done := make(chan bool)

	// Writer
	go func() {
		for i := 0; i < 100; i++ {
			store.Create(&APIKey{ID: "key" + string(rune(i)), KeyHash: "hash" + string(rune(i))})
		}
		done <- true
	}()

	// Reader
	go func() {
		for i := 0; i < 100; i++ {
			store.List()
		}
		done <- true
	}()

	<-done
	<-done
}
