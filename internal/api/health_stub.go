//go:build !linux

package api

import (
	"context"
)

// checkNFTables is a stub for non-Linux systems
func checkNFTables(ctx context.Context) error {
	// On macOS/Windows dev environment, we assume "healthy" or skip the check.
	// Returning nil allows development to proceed without error spam.
	return nil
}
