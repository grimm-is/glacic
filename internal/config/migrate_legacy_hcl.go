package config

import (
	"regexp"
	"strings"
)

// migrate_legacy_hcl.go transforms legacy HCL syntax to current schema.
// This runs BEFORE HCL parsing.

var legacyHCLTransforms = []struct {
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

func transformLegacyHCL(data []byte) ([]byte, []string) {
	content := string(data)
	var applied []string

	for _, transform := range legacyHCLTransforms {
		if transform.Pattern.MatchString(content) {
			content = transform.Pattern.ReplaceAllString(content, transform.Replacement)
			applied = append(applied, transform.Description)
		}
	}

	return []byte(content), applied
}

func detectLegacyFeaturesInternal(data []byte) []string {
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

func init() {
	RegisterPreParseMigration(PreParseMigration{
		Name:        "legacy_hcl",
		Description: "Transform legacy HCL syntax to current schema",
		Transform:   transformLegacyHCL,
	})
}
