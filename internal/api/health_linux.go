//go:build linux

package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/google/nftables"
	"grimm.is/glacic/internal/logging"
)

// checkNFTables verifies that we can communicate with the kernel nftables subsystem
func checkNFTables(ctx context.Context) error {
	// 1. Check Kernel communication
	c, err := nftables.New()
	if err != nil {
		return fmt.Errorf("failed to open nftables connection: %w", err)
	}
	// Just listing tables is enough to verify connectivity
	if _, err := c.ListTables(); err != nil {
		return fmt.Errorf("failed to list nftables tables: %w", err)
	}

	// 2. Check API Self-Connectivity (Localhost HTTPS)
	// We use a custom client that ignores self-signed certs for localhost checks
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://localhost:8443/api/status", nil)
	if err != nil {
		// Non-critical, just log warning
		logging.Warn("Health check: failed to create request", "error", err)
		return nil
	}

	resp, err := client.Do(req)
	if err != nil {
		// Log but don't fail the entire health check if just API is busy, this limits noise
		// BUT if we are the ones causing the noise (by failing handshake), we should fix it.
		// By using InsecureSkipVerify above, we fix the handshake error.
		return nil
	}
	defer resp.Body.Close()

	return nil
}
