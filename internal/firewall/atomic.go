package firewall

import (
	"fmt"
	"grimm.is/glacic/internal/clock"
	"os"
	"os/exec"
	"strings"
)

// AtomicIPSetUpdate performs an atomic update of an nftables set.
// It creates a new set, populates it, then atomically swaps it with the old one.
func (m *IPSetManager) AtomicIPSetUpdate(setName string, setType SetType, elements []string) error {
	tempSetName := fmt.Sprintf("%s_new_%d", setName, clock.Now().UnixNano())

	// Build atomic script
	var script strings.Builder

	// Create temporary set with same type
	script.WriteString(fmt.Sprintf("add set inet %s %s { type %s; flags interval; }\n",
		m.tableName, tempSetName, setType))

	// Add elements to temp set in batches
	batchSize := 500
	for i := 0; i < len(elements); i += batchSize {
		end := i + batchSize
		if end > len(elements) {
			end = len(elements)
		}
		batch := elements[i:end]
		script.WriteString(fmt.Sprintf("add element inet %s %s { %s }\n",
			m.tableName, tempSetName, strings.Join(batch, ", ")))
	}

	// Flush old set
	script.WriteString(fmt.Sprintf("flush set inet %s %s\n", m.tableName, setName))

	// Copy elements from temp to real set
	// Note: nftables doesn't have a direct swap, so we flush and re-add
	for i := 0; i < len(elements); i += batchSize {
		end := i + batchSize
		if end > len(elements) {
			end = len(elements)
		}
		batch := elements[i:end]
		script.WriteString(fmt.Sprintf("add element inet %s %s { %s }\n",
			m.tableName, setName, strings.Join(batch, ", ")))
	}

	// Delete temp set
	script.WriteString(fmt.Sprintf("delete set inet %s %s\n", m.tableName, tempSetName))

	// Execute atomically
	return m.runNftScript([]string{script.String()})
}

// AtomicRulesetUpdate applies a complete ruleset atomically.
func AtomicRulesetUpdate(script string) error {
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("atomic update failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// BackupRuleset saves the current ruleset to a file.
func BackupRuleset(path string) error {
	output, err := exec.Command("nft", "list", "ruleset").Output()
	if err != nil {
		return fmt.Errorf("failed to list ruleset: %w", err)
	}
	return os.WriteFile(path, output, 0644)
}

// RestoreRuleset restores a ruleset from a backup file.
func RestoreRuleset(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read backup: %w", err)
	}

	// Flush existing rules first
	if err := exec.Command("nft", "flush", "ruleset").Run(); err != nil {
		return fmt.Errorf("failed to flush ruleset: %w", err)
	}

	// Apply backup
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(string(data))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to restore ruleset: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// DryRun validates a ruleset without applying it.
func DryRun(script string) error {
	cmd := exec.Command("nft", "-c", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("dry run failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// RollbackManager handles ruleset rollback on failure.
type RollbackManager struct {
	backupPath string
	hasBackup  bool
}

// NewRollbackManager creates a new rollback manager.
func NewRollbackManager() *RollbackManager {
	return &RollbackManager{
		backupPath: "/tmp/firewall_rollback.nft",
	}
}

// SaveCheckpoint saves the current ruleset as a rollback point.
func (r *RollbackManager) SaveCheckpoint() error {
	if err := BackupRuleset(r.backupPath); err != nil {
		return err
	}
	r.hasBackup = true
	return nil
}

// Rollback restores the saved checkpoint.
func (r *RollbackManager) Rollback() error {
	if !r.hasBackup {
		return fmt.Errorf("no checkpoint saved")
	}
	return RestoreRuleset(r.backupPath)
}

// Cleanup removes the backup file.
func (r *RollbackManager) Cleanup() {
	if r.hasBackup {
		os.Remove(r.backupPath)
		r.hasBackup = false
	}
}

// SafeApply applies changes with automatic rollback on failure.
func (r *RollbackManager) SafeApply(applyFn func() error) error {
	// Save checkpoint
	if err := r.SaveCheckpoint(); err != nil {
		return fmt.Errorf("failed to save checkpoint: %w", err)
	}

	// Apply changes
	if err := applyFn(); err != nil {
		// Attempt rollback
		if rbErr := r.Rollback(); rbErr != nil {
			return fmt.Errorf("apply failed: %w; rollback also failed: %v", err, rbErr)
		}
		return fmt.Errorf("apply failed (rolled back): %w", err)
	}

	// Success - cleanup backup
	r.Cleanup()
	return nil
}

// DeduplicateIPs removes duplicate entries from an IP list.
func DeduplicateIPs(ips []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(ips))

	for _, ip := range ips {
		normalized := strings.TrimSpace(ip)
		if normalized == "" {
			continue
		}
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, normalized)
		}
	}

	return result
}

// MergeIPLists merges multiple IP lists with deduplication.
func MergeIPLists(lists ...[]string) []string {
	total := 0
	for _, list := range lists {
		total += len(list)
	}

	merged := make([]string, 0, total)
	for _, list := range lists {
		merged = append(merged, list...)
	}

	return DeduplicateIPs(merged)
}
