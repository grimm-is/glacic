package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
)

// ForgivingLoadResult contains the result of a forgiving config load.
type ForgivingLoadResult struct {
	// Config is the best-effort parsed configuration
	Config *Config

	// OriginalHCL is the original file content
	OriginalHCL string

	// SalvagedHCL is the modified HCL with broken blocks commented out
	SalvagedHCL string

	// SkippedBlocks contains information about blocks that were skipped
	SkippedBlocks []SkippedBlock

	// HadErrors indicates if errors were encountered (partial parse)
	HadErrors bool

	// FatalError is set if even forgiving parse failed completely
	FatalError error
}

// SkippedBlock describes a block that was commented out due to parse errors.
type SkippedBlock struct {
	StartLine int
	EndLine   int
	Reason    string
	Content   string
}

// Diff returns a unified diff showing what was changed in the salvage process.
func (r *ForgivingLoadResult) Diff() string {
	if !r.HadErrors {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("--- original\n")
	sb.WriteString("+++ salvaged\n")

	for _, block := range r.SkippedBlocks {
		sb.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@ %s\n",
			block.StartLine, block.EndLine-block.StartLine+1,
			block.StartLine, block.EndLine-block.StartLine+1,
			block.Reason))

		lines := strings.Split(block.Content, "\n")
		for _, line := range lines {
			sb.WriteString("-" + line + "\n")
		}
		sb.WriteString("+# [SKIPPED - " + block.Reason + "]\n")
	}

	return sb.String()
}

// LoadForgiving attempts to parse an HCL config file, recovering from errors
// by iteratively commenting out broken blocks until the parse succeeds.
// This allows the system to boot into safe mode with a partial config.
func LoadForgiving(data []byte, filename string) *ForgivingLoadResult {
	result := &ForgivingLoadResult{
		OriginalHCL: string(data),
	}

	// First, try normal parse
	cfg, err := LoadHCL(data, filename)
	if err == nil {
		// No errors - return clean result
		result.Config = cfg
		result.SalvagedHCL = string(data)
		return result
	}

	// Parse failed - enter forgiving mode
	result.HadErrors = true
	workingHCL := string(data)

	// Maximum iterations to prevent infinite loops
	maxIterations := 50
	for i := 0; i < maxIterations; i++ {
		cfg, parseErr := tryParse(workingHCL, filename)
		if parseErr == nil {
			// Success!
			result.Config = cfg
			result.SalvagedHCL = workingHCL
			return result
		}

		// Extract error location
		errorLine := extractErrorLine(parseErr.Error())
		if errorLine <= 0 {
			// Can't determine error location - try block detection
			errorLine = 1
		}

		// Find and remove the problematic block
		newHCL, skipped := commentOutBlock(workingHCL, errorLine, parseErr.Error())
		if newHCL == workingHCL {
			// Couldn't make progress - give up
			result.FatalError = fmt.Errorf("could not recover from parse errors: %w", parseErr)
			// Return minimal config
			result.Config = &Config{
				SchemaVersion: CurrentSchemaVersion,
				IPForwarding:  true,
				API: &APIConfig{
					Enabled:   true,
					Listen:    ":8080",
					TLSListen: ":8443",
				},
			}
			result.SalvagedHCL = workingHCL
			return result
		}

		result.SkippedBlocks = append(result.SkippedBlocks, skipped)
		workingHCL = newHCL
	}

	// Exceeded max iterations
	result.FatalError = fmt.Errorf("exceeded maximum recovery iterations")
	result.Config = &Config{
		SchemaVersion: CurrentSchemaVersion,
		IPForwarding:  true,
	}
	result.SalvagedHCL = workingHCL
	return result
}

// tryParse attempts to parse HCL and returns the config or error.
func tryParse(hcl string, filename string) (*Config, error) {
	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL([]byte(hcl), filename)
	if diags.HasErrors() {
		return nil, fmt.Errorf("%s", diags.Error())
	}

	var cfg Config
	decodeDiags := gohcl.DecodeBody(file.Body, nil, &cfg)
	if decodeDiags.HasErrors() {
		return nil, fmt.Errorf("%s", decodeDiags.Error())
	}

	if cfg.SchemaVersion == "" {
		cfg.SchemaVersion = "1.0"
	}

	cfg.NormalizeZoneMappings()
	cfg.NormalizePolicies()

	return &cfg, nil
}

// extractErrorLine tries to extract a line number from an HCL error message.
func extractErrorLine(errMsg string) int {
	// HCL errors typically include "filename:line:col:"
	patterns := []string{
		`:(\d+):`,    // filename:line:col
		`line (\d+)`, // "line N"
		`Line (\d+)`, // "Line N"
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(errMsg)
		if len(matches) >= 2 {
			var line int
			fmt.Sscanf(matches[1], "%d", &line)
			if line > 0 {
				return line
			}
		}
	}

	return 0
}

// commentOutBlock finds the block containing the error line and comments it out.
// Returns the modified HCL and information about the skipped block.
func commentOutBlock(hcl string, errorLine int, reason string) (string, SkippedBlock) {
	lines := strings.Split(hcl, "\n")
	if errorLine > len(lines) {
		errorLine = len(lines)
	}

	// Find block boundaries
	// Look backwards for block start (line with opening brace or block keyword)
	startLine := errorLine - 1 // Convert to 0-indexed
	for startLine > 0 {
		line := strings.TrimSpace(lines[startLine-1])
		// Check if this looks like a block start
		if isBlockStart(line) {
			startLine--
			break
		}
		// Check if we hit an empty line or closing brace (previous block ended)
		if line == "" || line == "}" {
			break
		}
		startLine--
	}

	// Find block end (matching closing brace)
	endLine := errorLine - 1 // Convert to 0-indexed
	braceCount := 0
	for endLine < len(lines) {
		line := lines[endLine]
		braceCount += strings.Count(line, "{")
		braceCount -= strings.Count(line, "}")
		if braceCount <= 0 && strings.Contains(line, "}") {
			break
		}
		endLine++
	}

	// Build skipped block info
	skipped := SkippedBlock{
		StartLine: startLine + 1, // Convert back to 1-indexed
		EndLine:   endLine + 1,
		Reason:    truncateReason(reason),
	}

	// Extract the content being skipped
	if startLine < len(lines) && endLine < len(lines) {
		skippedLines := lines[startLine : endLine+1]
		skipped.Content = strings.Join(skippedLines, "\n")
	}

	// Comment out the lines
	for i := startLine; i <= endLine && i < len(lines); i++ {
		lines[i] = "# [SKIPPED] " + lines[i]
	}

	return strings.Join(lines, "\n"), skipped
}

// isBlockStart checks if a line looks like the start of an HCL block.
func isBlockStart(line string) bool {
	// Common block patterns
	blockPatterns := []string{
		`^\s*(interface|zone|policy|dhcp|dns|api|nat|ipset|route|vpn|features)\s+`,
		`^\s*\w+\s*"[^"]*"\s*\{`,
		`^\s*\w+\s*\{`,
	}

	for _, pattern := range blockPatterns {
		re := regexp.MustCompile(pattern)
		if re.MatchString(line) {
			return true
		}
	}

	return false
}

// truncateReason shortens the error reason for display.
func truncateReason(reason string) string {
	// Take first line only
	if idx := strings.Index(reason, "\n"); idx > 0 {
		reason = reason[:idx]
	}
	// Limit length
	if len(reason) > 100 {
		reason = reason[:97] + "..."
	}
	return reason
}
