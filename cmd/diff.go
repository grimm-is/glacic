package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/pmezard/go-difflib/difflib"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/firewall"
	"grimm.is/glacic/internal/logging"
)

// RunDiff compares the generated ruleset against the running configuration.
func RunDiff(configFile string) error {
	// 1. Load configuration
	result, err := config.LoadFileWithOptions(configFile, config.DefaultLoadOptions())
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	cfg := result.Config

	// 2. Generate expected ruleset
	// Use a nil logger or stdout logger?
	logger := logging.New(logging.DefaultConfig())

	// We need a firewall manager to generate rules
	// We don't need a real nftables connection for generation
	// Use nil logger for now or stdout?
	fwMgr := firewall.NewManagerWithConn(nil, logger, "")

	generatedRules, err := fwMgr.GenerateRules(firewall.FromGlobalConfig(cfg))
	if err != nil {
		return fmt.Errorf("failed to generate ruleset: %w", err)
	}

	// 3. Get running ruleset
	// Requires root
	if os.Geteuid() != 0 {
		return fmt.Errorf("must run as root to read running ruleset")
	}

	cmd := exec.Command("nft", "list", "ruleset")
	runningBytes, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list running ruleset: %w", err)
	}
	runningRules := string(runningBytes)

	// 4. Normalize generated ruleset using a temporary network namespace
	normalizedRules, err := NormalizeRuleset(generatedRules)
	if err != nil {
		// Fallback to generated rules if normalization fails
		normalizedRules = generatedRules
	}

	// 5. Clean / Strip noise (handles, counters, comments) for fair comparison
	finalGenerated := StripNoise(normalizedRules)
	finalRunning := StripNoise(runningRules)

	if finalGenerated == finalRunning {
		Printer.Println("No changes detected.")
		return nil
	}

	Printer.Println("Configuration differs from running state:")

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(finalGenerated),
		B:        difflib.SplitLines(finalRunning),
		FromFile: "Generated",
		ToFile:   "Running",
		Context:  3,
	}
	text, _ := difflib.GetUnifiedDiffString(diff)
	fmt.Print(text)

	return fmt.Errorf("configuration differs")
}
