//go:build !linux
// +build !linux

package cmd

// SetProcessName stub.
func SetProcessName(name string) error {
	return nil
}
