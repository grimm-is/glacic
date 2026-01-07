//go:build !linux

package cmd

// setupProxyChroot is a no-op on non-Linux systems
func setupProxyChroot(jailPath, hostSocketPath string) error {
	return nil
}
