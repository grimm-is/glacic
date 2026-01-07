package ctlplane

import (
	"log"
	"os/exec"
	"sync"
	"time"

	"grimm.is/glacic/internal/clock"
)

// SystemManager handles system-level operations like reboot and status.
type SystemManager struct {
	startTime       time.Time
	configFile      string
	firewallActive  bool
	firewallApplied time.Time
	safeMode        bool
	mu              sync.RWMutex
}

// NewSystemManager creates a new system manager.
func NewSystemManager(configFile string) *SystemManager {
	return &SystemManager{
		startTime:  clock.Now(),
		configFile: configFile,
	}
}

// GetStatus returns the current system status.
func (sm *SystemManager) GetStatus() Status {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	status := Status{
		Running:        true,
		Uptime:         time.Since(sm.startTime).Round(time.Second).String(),
		ConfigFile:     sm.configFile,
		FirewallActive: sm.firewallActive,
		SafeMode:       sm.safeMode,
	}

	if !sm.firewallApplied.IsZero() {
		status.FirewallApplied = sm.firewallApplied.Format(time.RFC3339)
	}

	return status
}

// SetFirewallApplied records when firewall rules were applied.
func (sm *SystemManager) SetFirewallApplied(active bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.firewallActive = active
	if active {
		sm.firewallApplied = clock.Now()
	}
}

// SetSafeMode updates the safe mode status.
func (sm *SystemManager) SetSafeMode(enabled bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.safeMode = enabled
}

// IsSafeMode returns whether the system is in safe mode.
func (sm *SystemManager) IsSafeMode() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.safeMode
}

// Reboot reboots the system.
func (sm *SystemManager) Reboot() error {
	log.Printf("[SYS] Reboot request received. Rebooting in 1 second...")
	auditLog("Reboot", "force=false")

	go func() {
		time.Sleep(1 * time.Second)
		// Sync filesystem before rebooting
		exec.Command("sync").Run()

		// Try systemctl reboot first (Alpine/OpenRC), fall back to reboot command
		if err := exec.Command("reboot").Run(); err != nil {
			log.Printf("[SYS] Reboot command failed: %v", err)
			// Try forceful reboot if standard fails
			exec.Command("reboot", "-f").Run()
		}
	}()
	return nil
}
