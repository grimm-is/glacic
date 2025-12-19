package config

import (
	"regexp"
	"strings"
)

// LegacyTransforms contains regex replacements to upgrade old HCL syntax
var LegacyTransforms = []struct {
	Pattern     *regexp.Regexp
	Replacement string
	Description string
}{
	{
		// Transform: protection { ... } -> protection "legacy_global" { interface = "*" ... }
		// This translates global protection settings to a wildcard interface protection block
		Pattern:     regexp.MustCompile(`(?m)^(\s*)protection\s*\{`),
		Replacement: "${1}protection \"legacy_global\" {\n${1}  interface = \"*\"",
		Description: "Convert global 'protection' block to wildcard 'protection' block",
	},
	{
		// Transform: global_protection { ... } -> protection "legacy_global" { interface = "*" ... }
		Pattern:     regexp.MustCompile(`(?m)^(\s*)global_protection\s*\{`),
		Replacement: "${1}protection \"legacy_global\" {\n${1}  interface = \"*\"",
		Description: "Convert 'global_protection' block to wildcard 'protection' block",
	},
	{
		// Transform: vpn_link_group -> uplink_group (deprecated terminology)
		Pattern:     regexp.MustCompile(`(?m)\bvpn_link_group\b`),
		Replacement: "uplink_group",
		Description: "Deprecated: 'vpn_link_group' renamed to 'uplink_group'",
	},
	{
		// Transform: vpn_link -> uplink within blocks (deprecated terminology)
		Pattern:     regexp.MustCompile(`(?m)(\s+)vpn_link\s*"`),
		Replacement: "${1}uplink \"",
		Description: "Deprecated: 'vpn_link' renamed to 'uplink'",
	},
}

// TransformLegacyHCL applies transformations to upgrade old HCL syntax
// to the current schema. This is done before parsing to handle syntax
// changes that can't be handled by post-parse migrations.
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

// DetectLegacyFeatures checks for legacy syntax that needs transformation
func DetectLegacyFeatures(data []byte) []string {
	content := string(data)
	var features []string

	// Check for unlabeled protection block
	if regexp.MustCompile(`(?m)^(\s*)protection\s*\{`).MatchString(content) {
		features = append(features, "unlabeled 'protection' block (use 'global_protection' or 'protection \"name\"')")
	}

	// Check for deprecated vpn_link_group (should be uplink_group)
	if strings.Contains(content, "vpn_link_group") {
		features = append(features, "deprecated 'vpn_link_group' block (use 'uplink_group' instead)")
	}

	// Check for deprecated vpn_link within uplink_group (should be uplink)
	if regexp.MustCompile(`(?m)\bvpn_link\s*"`).MatchString(content) {
		features = append(features, "deprecated 'vpn_link' block (use 'uplink' instead)")
	}

	// Check for missing schema_version
	if !strings.Contains(content, "schema_version") {
		features = append(features, "missing 'schema_version' field")
	}

	return features
}

// IsLegacyConfig checks if the config uses any legacy syntax
func IsLegacyConfig(data []byte) bool {
	return len(DetectLegacyFeatures(data)) > 0
}
