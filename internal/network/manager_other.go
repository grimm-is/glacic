//go:build !linux
// +build !linux

package network

// NewManager creates a new network manager (stub for non-Linux).
func NewManager() *Manager {
	// Panic or log warning in production?
	// For CLI tools on Darwin that accidentally use NewManager without deps, this is risky.
	// But glacic-darwin main might trigger it.
	// Better to return a Manager that panics on methods or use a Log-Only impl?
	// But we removed Manager struct from here, it uses manager_logic.go Manager.
	// We need to initialize it with SOMETHING.
	// Use DryRun components?
	return &Manager{
		nl:  &DryRunNetlinker{}, // Safe default?
		sys: &DryRunSystemController{},
		cmd: &DryRunExecutor{},
	}
}
