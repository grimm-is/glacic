//go:build linux

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// setupProxyChroot prepares the restricted environment
func setupProxyChroot(jailPath, hostSocketPath string) error {
	// hostSocketPath is e.g. /var/run/glacic/api/api.sock
	// We want to mount the directory containing it.
	socketDir := filepath.Dir(hostSocketPath) // /var/run/glacic/api

	// 1. Create jail
	if err := os.MkdirAll(jailPath, 0755); err != nil {
		return fmt.Errorf("mkdir jail: %w", err)
	}

	// 2. Wait for socket directory to exist (race condition fix)
	// The API server may not have created it yet
	for i := 0; i < 30; i++ {
		if _, err := os.Stat(socketDir); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 3. Mount socket dir
	// Target: /run/api
	jailSocketDir := filepath.Join(jailPath, "run/api")
	if err := os.MkdirAll(jailSocketDir, 0755); err != nil {
		return fmt.Errorf("mkdir jail socket dir: %w", err)
	}

	// Bind mount
	// Ensure we unmount first if re-running
	_ = syscall.Unmount(jailSocketDir, 0)
	if err := syscall.Mount(socketDir, jailSocketDir, "", syscall.MS_BIND|syscall.MS_RDONLY, ""); err != nil {
		return fmt.Errorf("mount socket dir: %w", err)
	}

	return nil
}
