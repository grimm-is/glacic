//go:build linux

package firewall

import (
	"fmt"
	"os/exec"
	"strings"

	"grimm.is/glacic/internal/brand"
)

// AtomicApplier handles atomic ruleset application.
type AtomicApplier struct {
	productionTable string
}

// NewAtomicApplier creates a new atomic applier.
func NewAtomicApplier() *AtomicApplier {
	return &AtomicApplier{
		productionTable: brand.LowerName,
	}
}

// ValidateScript validates an nft script without applying it.
func (a *AtomicApplier) ValidateScript(script string) error {
	cmd := exec.Command("nft", "-c", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("script validation failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// ApplyScript applies an nft script atomically.
func (a *AtomicApplier) ApplyScript(script string) error {
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("script application failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// BuildAtomicSwapScript creates a script that atomically replaces the production table.
// It prepends commands to delete the existing table before adding the new one.
func (a *AtomicApplier) BuildAtomicSwapScript(newTableScript string) string {
	var sb strings.Builder

	// Delete existing production table (ignore errors if doesn't exist)
	sb.WriteString(fmt.Sprintf("delete table inet %s\n", a.productionTable))

	// Add the new table script
	sb.WriteString(newTableScript)

	return sb.String()
}

// ApplyAtomically validates and applies a script atomically.
func (a *AtomicApplier) ApplyAtomically(script string) error {
	// First validate
	if err := a.ValidateScript(script); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Then apply
	return a.ApplyScript(script)
}
