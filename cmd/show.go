package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/client"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/firewall"
	"grimm.is/glacic/internal/logging"
)

// RunShow dumps the firewall ruleset.
// If remoteURL is provided, it fetches the config from a remote Glacic instance.
// If configFile is provided, it generates the ruleset from the config and normalizes it via a temp netns.
// If configFile is empty, it dumps the running ruleset (nft list ruleset).
func RunShow(configFile string, summary bool, remoteURL, apiKey string) error {
	var cfg *config.Config

	// Remote mode: fetch config from API
	if remoteURL != "" {
		opts := []client.ClientOption{}
		if apiKey != "" {
			opts = append(opts, client.WithAPIKey(apiKey))
		}
		apiClient := client.NewHTTPClient(remoteURL, opts...)

		remoteCfg, err := apiClient.GetConfig()
		if err != nil {
			return fmt.Errorf("failed to fetch remote config: %w", err)
		}
		cfg = remoteCfg

		if summary {
			printSummary(cfg)
			Printer.Println("\n--- Generated Ruleset (from remote config) ---")
		}

		// Generate rules from fetched config
		logger := logging.New(logging.DefaultConfig())
		fwMgr := firewall.NewManagerWithConn(nil, logger, "")
		generatedRules, err := fwMgr.GenerateRules(firewall.FromGlobalConfig(cfg))
		if err != nil {
			return fmt.Errorf("failed to generate ruleset: %w", err)
		}
		Printer.Printf("%s", generatedRules)
		return nil
	}

	if configFile == "" {
		// Live mode
		// Check root
		if os.Geteuid() != 0 {
			// If not root, maybe they forgot the config file argument?
			// Help them out.
			return fmt.Errorf("must run as root to view running ruleset, or specify config file.\\nExample: %s show --summary /etc/glacic/glacic.hcl", brand.BinaryName)
		}
		cmd := exec.Command("nft", "list", "ruleset")
		output, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("failed to list ruleset: %w", err)
		}
		Printer.Printf("%s", string(output))
		return nil

	}

	// Config mode
	result, err := config.LoadFileWithOptions(configFile, config.DefaultLoadOptions())
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	cfg = result.Config

	if summary {
		printSummary(cfg)
		// If summary only is requested? user said "expand... to include summaries".
		// Does that mean ONLY summary? Or BOTH?
		// "include summaries... to represent the firewall in a state closer to plain english"
		// If I run `glacic show --summary`, I probably want just summary?
		// Or summary + rules?
		// If I assume typical CLI behavior, flags usually toggle output modes.
		// Let's print summary. If they want rules too, they run without summary?
		// Or maybe --summary adds it.
		// Let's print summary followed by a separator?
		Printer.Println("\n--- Normalized Ruleset ---")
	}

	// Generate
	logger := logging.New(logging.DefaultConfig())
	// Use nil conn as we only generate
	fwMgr := firewall.NewManagerWithConn(nil, logger, "")

	generatedRules, err := fwMgr.GenerateRules(firewall.FromGlobalConfig(cfg))
	if err != nil {
		return fmt.Errorf("failed to generate ruleset: %w", err)
	}

	// Normalize
	normalizedRules, err := NormalizeRuleset(generatedRules)
	if err != nil {
		// Fallback: use raw generated rules
		// Likely unstructured (atomic)
		Printer.Println(generatedRules)
		return nil
	}

	// Strip noise from normalized rules (handles/counters are created by kernel even in temp ns)
	Printer.Printf("%s", StripNoise(normalizedRules))
	return nil
}

// NormalizeRuleset runs the rules inside a temporary namespace to get canonical formatting.
func NormalizeRuleset(rules string) (string, error) {
	cmdGen := exec.Command("unshare", "-n", "sh", "-c", "nft -f - && nft list ruleset")
	cmdGen.Stdin = strings.NewReader(rules)
	output, err := cmdGen.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// StripNoise removes handles, counters, and comments.
func StripNoise(s string) string {
	lines := strings.Split(s, "\n")
	var out []string

	reHandle := regexp.MustCompile(` handle \d+`)
	reCounters := regexp.MustCompile(` packets \d+ bytes \d+`)

	for _, line := range lines {
		// Remove handles (handle 123)
		line = reHandle.ReplaceAllString(line, "")
		// Remove counters (packets 123 bytes 456)
		line = reCounters.ReplaceAllString(line, "")

		// Remove trailing whitespace
		line = strings.TrimRight(line, " \t")

		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
