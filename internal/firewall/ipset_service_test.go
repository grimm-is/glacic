//go:build linux
// +build linux

package firewall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/state"

	"github.com/stretchr/testify/mock"
)

// MockStateStore for testing
type MockStateStore struct {
	mock.Mock
}

func (m *MockStateStore) Set(bucket, key string, value []byte) error {
	args := m.Called(bucket, key, value)
	return args.Error(0)
}

func (m *MockStateStore) Get(bucket, key string) ([]byte, error) {
	args := m.Called(bucket, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockStateStore) ListKeys(bucket string) ([]string, error) {
	args := m.Called(bucket)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

// Stub other methods to satisfy interface
func (m *MockStateStore) CreateBucket(name string) error                       { return nil }
func (m *MockStateStore) DeleteBucket(name string) error                       { return nil }
func (m *MockStateStore) ListBuckets() ([]string, error)                       { return nil, nil }
func (m *MockStateStore) GetWithMeta(bucket, key string) (*state.Entry, error) { return nil, nil }
func (m *MockStateStore) SetWithTTL(bucket, key string, value []byte, ttl time.Duration) error {
	return nil
}
func (m *MockStateStore) Delete(bucket, key string) error                 { return nil }
func (m *MockStateStore) List(bucket string) (map[string][]byte, error)   { return nil, nil }
func (m *MockStateStore) GetJSON(bucket, key string, v interface{}) error { return nil }
func (m *MockStateStore) SetJSON(bucket, key string, v interface{}) error { return nil }
func (m *MockStateStore) SetJSONWithTTL(bucket, key string, v interface{}, ttl time.Duration) error {
	return nil
}
func (m *MockStateStore) Subscribe(ctx context.Context) <-chan state.Change      { return nil }
func (m *MockStateStore) GetChangesSince(version uint64) ([]state.Change, error) { return nil, nil }
func (m *MockStateStore) CurrentVersion() uint64                                 { return 0 }
func (m *MockStateStore) CreateSnapshot() (*state.Snapshot, error)               { return nil, nil }
func (m *MockStateStore) RestoreSnapshot(snapshot *state.Snapshot) error         { return nil }
func (m *MockStateStore) Close() error                                           { return nil }

// MockCommandRunner is defined in mocks.go

func TestIPSetService_ListIPSets(t *testing.T) {
	mockStore := new(MockStateStore)
	logger := logging.New(logging.DefaultConfig())
	tmpDir, _ := os.MkdirTemp("", "ipset-test-list")
	defer os.RemoveAll(tmpDir)

	svc := NewIPSetService("glacic", tmpDir, mockStore, logger)

	mockStore.On("ListKeys", "ipset_metadata").Return([]string{"test_set"}, nil)

	meta := IPSetMetadata{Name: "test_set", Type: "ipv4_addr"}
	metaBytes, _ := json.Marshal(meta)
	mockStore.On("Get", "ipset_metadata", "test_set").Return(metaBytes, nil)

	list, err := svc.ListIPSets()
	if err != nil {
		t.Fatalf("ListIPSets failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 ipset, got %d", len(list))
	}
	if list[0].Name != "test_set" {
		t.Errorf("expected name test_set, got %s", list[0].Name)
	}
}

func TestIPSetService_ForceUpdate(t *testing.T) {
	mockStore := new(MockStateStore)
	mockRunner := new(MockCommandRunner)
	logger := logging.New(logging.DefaultConfig())

	tmpDir, _ := os.MkdirTemp("", "ipset-test-update")
	defer os.RemoveAll(tmpDir)

	svc := NewIPSetService("glacic", tmpDir, mockStore, logger)
	svc.ipsetManager.SetRunner(mockRunner)

	// Setup metadata
	url := "http://example.com/list.txt"
	meta := IPSetMetadata{
		Name:      "test_set",
		Type:      "ipv4_addr",
		Source:    "url",
		SourceURL: url,
	}
	metaBytes, _ := json.Marshal(meta)

	mockStore.On("Get", "ipset_metadata", "test_set").Return(metaBytes, nil)
	// Expectations for saveMetadata
	mockStore.On("Set", "ipset_metadata", "test_set", mock.Anything).Return(nil)

	// Prepopulate cache to skip network download
	cacheKeyHash := sha256.Sum256([]byte(url))
	cacheKey := hex.EncodeToString(cacheKeyHash[:])

	dataContent := "1.2.3.4\n5.6.7.8\n"
	metaContent := map[string]interface{}{
		"cached_at": float64(time.Now().Unix()),
		"checksum":  "dummy", // checksum valid logic is inside loadFromCache, need to be careful
	}
	// Wait, loadFromCache verifies checksum!
	// I need to calculate correct checksum.
	// FireHOLManager.calculateChecksum uses sha256 hex.
	dataHash := sha256.Sum256([]byte(dataContent))
	metaContent["checksum"] = hex.EncodeToString(dataHash[:])

	metaBytesFile, _ := json.Marshal(metaContent)

	os.WriteFile(filepath.Join(tmpDir, cacheKey+".txt"), []byte(dataContent), 0644)
	os.WriteFile(filepath.Join(tmpDir, cacheKey+".meta"), metaBytesFile, 0644)

	// Mock nft commands
	// ReloadSet uses RunInput with "nft -f -"
	mockRunner.On("RunInput", mock.MatchedBy(func(input string) bool {
		return strings.Contains(input, "flush set inet glacic test_set") &&
			strings.Contains(input, "1.2.3.4") &&
			strings.Contains(input, "5.6.7.8")
	}), "nft", "-f", "-").Return(nil)

	err := svc.ForceUpdate("test_set")
	if err != nil {
		t.Fatalf("ForceUpdate failed: %v", err)
	}

	mockRunner.AssertExpectations(t)
	mockStore.AssertExpectations(t)
}

func TestIPSetService_ValidateIPSet(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	svc := NewIPSetService("glacic", "/tmp", nil, logger)

	valid := &config.IPSet{
		Name:    "valid",
		Type:    "ipv4_addr",
		Entries: []string{"1.1.1.1"},
	}
	if err := svc.validateIPSet(valid); err != nil {
		t.Errorf("expected valid, got error: %v", err)
	}

	invalidType := &config.IPSet{
		Name:    "invalid",
		Type:    "bad_type",
		Entries: []string{"1.1.1.1"},
	}
	if err := svc.validateIPSet(invalidType); err == nil {
		t.Error("expected error for invalid type, got nil")
	}
}
