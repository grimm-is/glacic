package api

import (
	"grimm.is/glacic/internal/clock"
	"testing"
	"time"

	"grimm.is/glacic/internal/api/storage"
)

func TestKeyOptions(t *testing.T) {
	key := &storage.APIKey{}

	t.Run("WithExpiry", func(t *testing.T) {
		now := clock.Now()
		WithExpiry(now)(key)
		if key.ExpiresAt == nil || !key.ExpiresAt.Equal(now) {
			t.Error("WithExpiry failed")
		}
	})

	t.Run("WithExpiryDuration", func(t *testing.T) {
		WithExpiryDuration(1 * time.Hour)(key)
		if key.ExpiresAt == nil {
			t.Error("WithExpiryDuration failed")
		}
		// Allow some delta
		if time.Until(*key.ExpiresAt) > time.Hour || time.Until(*key.ExpiresAt) < time.Minute*59 {
			t.Error("WithExpiryDuration time incorrect")
		}
	})

	t.Run("WithAllowedIPs", func(t *testing.T) {
		ips := []string{"1.2.3.4"}
		WithAllowedIPs(ips)(key)
		if len(key.AllowedIPs) != 1 || key.AllowedIPs[0] != "1.2.3.4" {
			t.Error("WithAllowedIPs failed")
		}
	})

	t.Run("WithAllowedPaths", func(t *testing.T) {
		paths := []string{"/api/v1"}
		WithAllowedPaths(paths)(key)
		if len(key.AllowedPaths) != 1 || key.AllowedPaths[0] != "/api/v1" {
			t.Error("WithAllowedPaths failed")
		}
	})

	t.Run("WithRateLimit", func(t *testing.T) {
		WithRateLimit(100)(key)
		if key.RateLimit != 100 {
			t.Error("WithRateLimit failed")
		}
	})

	t.Run("WithDescription", func(t *testing.T) {
		WithDescription("desc")(key)
		if key.Description != "desc" {
			t.Error("WithDescription failed")
		}
	})

	t.Run("WithCreatedBy", func(t *testing.T) {
		WithCreatedBy("admin")(key)
		if key.CreatedBy != "admin" {
			t.Error("WithCreatedBy failed")
		}
	})
}

func TestValidationChecks(t *testing.T) {
	key := &storage.APIKey{
		AllowedIPs:   []string{"10.0.0.1"},
		AllowedPaths: []string{"/api"},
	}

	if !key.IsIPAllowed("10.0.0.1") {
		t.Error("IsIPAllowed failed for valid IP")
	}
	if key.IsIPAllowed("10.0.0.2") {
		t.Error("IsIPAllowed passed for invalid IP")
	}

	if !key.IsPathAllowed("/api/users") {
		t.Error("IsPathAllowed failed for valid path")
	}
	if key.IsPathAllowed("/other") {
		t.Error("IsPathAllowed passed for invalid path")
	}

	// Test empty restrictions
	openKey := &storage.APIKey{}
	if !openKey.IsIPAllowed("1.2.3.4") {
		t.Error("IsIPAllowed failed for open key")
	}
	if !openKey.IsPathAllowed("/any") {
		t.Error("IsPathAllowed failed for open key")
	}
}
