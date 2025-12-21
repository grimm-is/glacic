//go:build linux

package api

import (
	"context"
	"fmt"

	"github.com/google/nftables"
)

// checkNFTables performs a lightweight check of nftables connectivity
func checkNFTables(ctx context.Context) error {
	// Create a new connection
	// We don't need to hold it open, just verify we can open it and list something
	conn, err := nftables.New()
	if err != nil {
		return fmt.Errorf("failed to open nftables connection: %v", err)
	}

	// We don't rely on system calls, so just list tables.
	// Note: nftables.New() doesn't strictly verify kernel connectivity until we try an operation.
	// ListTables is lightweight.
	tables, err := conn.ListTables()
	if err != nil {
		return fmt.Errorf("failed to list tables: %v", err)
	}

	// If we got here, we're good.
	// We typically expect at least one table (filter/main) if firewall is active,
	// but an empty list is also a valid state (just means no rules), so checking err is enough.
	_ = tables
	return nil
}
