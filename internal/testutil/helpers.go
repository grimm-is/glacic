package testutil

import (
	"os"
	"testing"
)

// RequireVM skips the test if the GLACIC_VM_TEST environment variable is not set.
// This ensures that tests requiring real kernel capabilities (nftables, interfaces)
// are only run in the proper environment.
func RequireVM(t *testing.T) {
	t.Helper()
	if os.Getenv("GLACIC_VM_TEST") == "" {
		t.Skip("Skipping test: requires GLACIC_VM_TEST environment")
	}
}
