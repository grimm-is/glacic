package config

import (
	"regexp"
	"strings"
)

// LegacyTransforms contains regex replacements to upgrade old HCL syntax
// Deprecated: This is now handled by migrations.ApplyPreParseMigrations()
// when loading configs. Kept for backwards compatibility with any
// external code that may reference it.
var LegacyTransforms = []struct {
	Pattern     *regexp.Regexp
	Replacement string
	Description string
}{
	{
		Pattern:     regexp.MustCompile(`(?m)^(\s*)protection\s*\{`),
		Replacement: "${1}protection \"legacy_global\" {\n${1}  interface = \"*\"",
		Description: "Convert global 'protection' block to wildcard 'protection' block",
	},
	{
		Pattern:     regexp.MustCompile(`(?m)^(\s*)global_protection\s*\{`),
		Replacement: "${1}protection \"legacy_global\" {\n${1}  interface = \"*\"",
		Description: "Convert 'global_protection' block to wildcard 'protection' block",
	},
	{
		Pattern:     regexp.MustCompile(`(?m)\bvpn_link_group\b`),
		Replacement: "uplink_group",
		Description: "Deprecated: 'vpn_link_group' renamed to 'uplink_group'",
	},
	{
		Pattern:     regexp.MustCompile(`(?m)(\s+)vpn_link\s*"`),
		Replacement: "${1}uplink \"",
		Description: "Deprecated: 'vpn_link' renamed to 'uplink'",
	},
}

// TransformLegacyHCL applies transformations to upgrade old HCL syntax.
// Deprecated: Use config loading via LoadFile() which applies migrations automatically.
func TransformLegacyHCL(data []byte) ([]byte, []string) {
	content := string(data)
	var applied []string

	for _, transform := range LegacyTransforms {
		if transform.Pattern.MatchString(content) {
			content = transform.Pattern.ReplaceAllString(content, transform.Replacement)
			applied = append(applied, transform.Description)
		}
	}

	return []byte(content), applied
}

// DetectLegacyFeatures checks for legacy syntax that needs transformation.
// Deprecated: Use config loading via LoadFile() which handles this automatically.
func DetectLegacyFeatures(data []byte) []string {
	content := string(data)
	var features []string

	if regexp.MustCompile(`(?m)^(\s*)protection\s*\{`).MatchString(content) {
		features = append(features, "unlabeled 'protection' block")
	}

	if strings.Contains(content, "vpn_link_group") {
		features = append(features, "deprecated 'vpn_link_group' block")
	}

	if regexp.MustCompile(`(?m)\bvpn_link\s*"`).MatchString(content) {
		features = append(features, "deprecated 'vpn_link' block")
	}

	if !strings.Contains(content, "schema_version") {
		features = append(features, "missing 'schema_version' field")
	}

	if regexp.MustCompile(`(?m)^\s*interfaces\s*=\s*\[`).MatchString(content) {
		features = append(features, "deprecated 'interfaces' field in zone")
	}

	return features
}

// IsLegacyConfig checks if the config uses any legacy syntax.
func IsLegacyConfig(data []byte) bool {
	return len(DetectLegacyFeatures(data)) > 0
}
