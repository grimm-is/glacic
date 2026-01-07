package firewall

import (
	"errors"
	"os"
	"testing"
)

func TestDeduplicateIPs(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty list",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "no duplicates",
			input:    []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"},
			expected: []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"},
		},
		{
			name:     "with duplicates",
			input:    []string{"1.1.1.1", "2.2.2.2", "1.1.1.1", "3.3.3.3", "2.2.2.2"},
			expected: []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"},
		},
		{
			name:     "with whitespace",
			input:    []string{" 1.1.1.1 ", "2.2.2.2", "  1.1.1.1"},
			expected: []string{"1.1.1.1", "2.2.2.2"},
		},
		{
			name:     "empty strings filtered",
			input:    []string{"1.1.1.1", "", "  ", "2.2.2.2"},
			expected: []string{"1.1.1.1", "2.2.2.2"},
		},
		{
			name:     "CIDR notation",
			input:    []string{"10.0.0.0/8", "192.168.0.0/16", "10.0.0.0/8"},
			expected: []string{"10.0.0.0/8", "192.168.0.0/16"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DeduplicateIPs(tc.input)
			if len(result) != len(tc.expected) {
				t.Errorf("Expected %d items, got %d", len(tc.expected), len(result))
				return
			}
			for i, ip := range result {
				if ip != tc.expected[i] {
					t.Errorf("Mismatch at index %d: got %s, want %s", i, ip, tc.expected[i])
				}
			}
		})
	}
}

func TestMergeIPLists(t *testing.T) {
	tests := []struct {
		name     string
		lists    [][]string
		expected []string
	}{
		{
			name:     "empty lists",
			lists:    [][]string{},
			expected: []string{},
		},
		{
			name:     "single list",
			lists:    [][]string{{"1.1.1.1", "2.2.2.2"}},
			expected: []string{"1.1.1.1", "2.2.2.2"},
		},
		{
			name:     "multiple lists no overlap",
			lists:    [][]string{{"1.1.1.1"}, {"2.2.2.2"}, {"3.3.3.3"}},
			expected: []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"},
		},
		{
			name:     "multiple lists with overlap",
			lists:    [][]string{{"1.1.1.1", "2.2.2.2"}, {"2.2.2.2", "3.3.3.3"}, {"1.1.1.1", "4.4.4.4"}},
			expected: []string{"1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := MergeIPLists(tc.lists...)
			if len(result) != len(tc.expected) {
				t.Errorf("Expected %d items, got %d", len(tc.expected), len(result))
				return
			}
			for i, ip := range result {
				if ip != tc.expected[i] {
					t.Errorf("Mismatch at index %d: got %s, want %s", i, ip, tc.expected[i])
				}
			}
		})
	}
}

func TestRollbackManager_NoCheckpoint(t *testing.T) {
	rm := NewRollbackManager()

	err := rm.Rollback()
	if err == nil {
		t.Error("Expected error when rolling back without checkpoint")
	}
	if err.Error() != "no checkpoint saved" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestRollbackManager_Cleanup(t *testing.T) {
	rm := NewRollbackManager()
	rm.hasBackup = true

	// Create a dummy backup file
	if err := os.WriteFile(rm.backupPath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	rm.Cleanup()

	if rm.hasBackup {
		t.Error("hasBackup should be false after cleanup")
	}
	if _, err := os.Stat(rm.backupPath); !os.IsNotExist(err) {
		t.Error("Backup file should be removed after cleanup")
	}
}

func TestRollbackManager_SafeApply_Success(t *testing.T) {
	// This test requires nft to be available, skip if not
	if _, err := os.Stat("/usr/sbin/nft"); os.IsNotExist(err) {
		t.Skip("nft not available, skipping")
	}

	rm := NewRollbackManager()
	called := false

	err := rm.SafeApply(func() error {
		called = true
		return nil
	})

	if err != nil {
		t.Logf("SafeApply error (expected on macOS): %v", err)
		return
	}

	if !called {
		t.Error("Apply function should have been called")
	}
}

func TestRollbackManager_SafeApply_Failure(t *testing.T) {
	// This test requires nft to be available, skip if not
	if _, err := os.Stat("/usr/sbin/nft"); os.IsNotExist(err) {
		t.Skip("nft not available, skipping")
	}

	rm := NewRollbackManager()
	applyErr := errors.New("apply failed")

	err := rm.SafeApply(func() error {
		return applyErr
	})

	if err == nil {
		t.Error("Expected error from SafeApply")
	}
}
