//go:build linux
// +build linux

package firewall

import (
	"os/exec"
)

// dumpRules executes nft list ruleset to show applied rules
func (m *Manager) dumpRules() {
	cmd := exec.Command("nft", "list", "ruleset")
	output, err := cmd.CombinedOutput()
	if err != nil {
		m.logger.Error("Error dumping rules", "error", err, "output", string(output))
		return
	}
	m.logger.Info("NFTables Ruleset", "ruleset", string(output))
}
