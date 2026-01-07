package device

import (
	"testing"
	"time"

	"grimm.is/glacic/internal/state"
)

func TestDeviceManager(t *testing.T) {
	opts := state.DefaultOptions(":memory:")
	store, err := state.NewSQLiteStore(opts)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	mockOUI := func(mac string) string {
		if mac == "00:11:22:33:44:55" {
			return "MockVendor"
		}
		return ""
	}

	mgr, err := NewManager(store, mockOUI)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// 1. Create Identity
	id, err := mgr.CreateIdentity("Bobby's iPad", "Bobby", "tablet")
	if err != nil {
		t.Fatalf("Failed to create identity: %v", err)
	}
	if id.ID == "" {
		t.Error("Expected ID to be generated")
	}
	if id.Alias != "Bobby's iPad" {
		t.Errorf("Expected alias 'Bobby's iPad', got %s", id.Alias)
	}

	// 2. Link MAC (Randomization 1)
	mac1 := "00:11:22:33:44:55"
	if err := mgr.LinkMAC(mac1, id.ID); err != nil {
		t.Fatalf("Failed to link MAC 1: %v", err)
	}

	// 3. Link MAC (Randomization 2)
	mac2 := "AA:BB:CC:DD:EE:FF"
	if err := mgr.LinkMAC(mac2, id.ID); err != nil {
		t.Fatalf("Failed to link MAC 2: %v", err)
	}

	// 4. GetDevice
	info1 := mgr.GetDevice(mac1)
	if info1.Device == nil || info1.Device.ID != id.ID {
		t.Error("Expected device 1 to be linked to identity")
	}
	if info1.Vendor != "MockVendor" {
		t.Errorf("Expected mocked vendor, got %s", info1.Vendor)
	}

	info2 := mgr.GetDevice(mac2)
	if info2.Device == nil || info2.Device.ID != id.ID {
		t.Error("Expected device 2 to be linked to identity")
	}

	infoUnknown := mgr.GetDevice("12:34:56:78:90:AB")
	if infoUnknown.Device != nil {
		t.Error("Expected no identity for unknown MAC")
	}

	// 5. Update Identity
	newAlias := "Bobby's Pro iPad"
	updated, err := mgr.UpdateIdentity(id.ID, &newAlias, nil, nil, nil)
	if err != nil {
		t.Fatalf("Failed to update identity: %v", err)
	}
	if updated.Alias != newAlias {
		t.Errorf("Expected updated alias %s, got %s", newAlias, updated.Alias)
	}

	// Verify persistence via fresh manager
	mgr2, err := NewManager(store, nil)
	if err != nil {
		t.Fatalf("Failed to create manager 2: %v", err)
	}

	infoPersist := mgr2.GetDevice(mac1)
	if infoPersist.Device == nil || infoPersist.Device.Alias != newAlias {
		t.Errorf("Expected persisted alias %s, got %v", newAlias, infoPersist.Device)
	}
}

func TestUpdateIdentity_Concurrency(t *testing.T) {
	opts := state.DefaultOptions(":memory:")
	store, _ := state.NewSQLiteStore(opts)
	defer store.Close()
	mgr, _ := NewManager(store, nil)

	id, _ := mgr.CreateIdentity("Concurrent", "User", "test")

	// Run concurrent updates
	iterations := 10
	errCh := make(chan error, iterations)

	for i := 0; i < iterations; i++ {
		go func(idx int) {
			// Update
			alias2 := "New Alias"
			_, err := mgr.UpdateIdentity(id.ID, &alias2, nil, nil, nil)
			errCh <- err
		}(i)
	}

	timeout := time.After(2 * time.Second)
	for i := 0; i < iterations; i++ {
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("Concurrent update failed: %v", err)
			}
		case <-timeout:
			t.Fatal("Timeout waiting for concurrent updates")
		}
	}
}
