//go:build linux

package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"grimm.is/glacic/internal/brand"
)

// setupChroot prepares a chroot jail
func setupChroot(jailPath string) error {
	// 1. Create jail root
	if err := os.MkdirAll(jailPath, 0755); err != nil {
		return fmt.Errorf("failed to create jail root: %w", err)
	}

	// 2. Setup Persistence: Bind mount state dir
	// This ensures that when the app writes to state files (auth.json, certs) inside the jail,
	// it goes to the real directory.
	authDir := brand.GetStateDir()
	jailAuthDir := filepath.Join(jailPath, authDir)

	if err := os.MkdirAll(jailAuthDir, 0755); err != nil {
		return fmt.Errorf("failed to create jail auth dir: %w", err)
	}

	// Bind mount
	// Try to unmount first to avoid stacking mounts if previously crashed
	_ = syscall.Unmount(jailAuthDir, 0)
	if err := syscall.Mount(authDir, jailAuthDir, "", syscall.MS_BIND, ""); err != nil {
		return fmt.Errorf("failed to bind mount auth dir: %w", err)
	}

	// 3. Setup Network: Copy /etc/resolv.conf and /etc/hosts
	// We copy instead of bind mount to avoid exposing all of /etc or dealing with symlinks
	etcDir := filepath.Join(jailPath, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		return fmt.Errorf("failed to create jail etc: %w", err)
	}

	filesToCopy := []string{"/etc/resolv.conf", "/etc/hosts", "/etc/ssl/certs/ca-certificates.crt"}
	for _, src := range filesToCopy {
		// Only copy if source exists
		if _, err := os.Stat(src); err == nil {
			dst := filepath.Join(jailPath, src)
			// Ensure directory exists for deep paths like ssl/certs
			if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
				continue
			}
			if err := copyFile(src, dst); err != nil {
				Printer.Printf("Warning: failed to copy %s to jail: %v\n", src, err)
			}
		}
	}

	// 4. Setup Runtime Directory: Bind mount /var/run for control socket
	runDir := "/var/run"
	jailRunDir := filepath.Join(jailPath, runDir)
	if err := os.MkdirAll(jailRunDir, 0755); err != nil {
		return fmt.Errorf("failed to create jail run dir: %w", err)
	}

	_ = syscall.Unmount(jailRunDir, 0)
	// We bind mount /var/run so we can access glacic-ctl.sock
	if err := syscall.Mount(runDir, jailRunDir, "", syscall.MS_BIND, ""); err != nil {
		return fmt.Errorf("failed to bind mount run dir: %w", err)
	}

	return nil
}

// enterChroot enters the jail
func enterChroot(jailPath string) error {
	if err := syscall.Chdir(jailPath); err != nil {
		return fmt.Errorf("failed to chdir to jail: %w", err)
	}

	if err := syscall.Chroot("."); err != nil {
		return fmt.Errorf("failed to chroot: %w", err)
	}

	if err := syscall.Chdir("/"); err != nil {
		return fmt.Errorf("failed to chdir to / inside jail: %w", err)
	}

	return nil
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}
