package firewall

import (
	"crypto/sha256"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"

	"grimm.is/glacic/internal/brand"
)

// TableMetadata contains versioning and tracking info embedded in nftables comments.
type TableMetadata struct {
	Version    string // Glacic version (e.g., "1.2.3" or "dev")
	ApplyCount int    // Number of times rules have been applied
	ConfigHash string // First 8 chars of SHA256 of config
}

// metadataRegex parses the glacic metadata comment format:
// glacic:v<version>:c=<count>:h=<hash>
var metadataRegex = regexp.MustCompile(`glacic:v([^:]+):c=(\d+):h=([a-f0-9]+)`)

// GetTableMetadata reads the current metadata from an nftables table comment.
// Returns nil if not found or unparseable.
func GetTableMetadata(tableName, family string) *TableMetadata {
	// Run: nft list table inet glacic
	cmd := exec.Command("nft", "-j", "list", "table", family, tableName)
	out, err := cmd.Output()
	if err != nil {
		return nil // Table doesn't exist or error
	}

	// Look for comment in output
	// JSON output has {"comment": "..."} somewhere
	// Simple approach: regex the raw output
	outStr := string(out)

	// Find comment value
	commentRegex := regexp.MustCompile(`"comment"\s*:\s*"([^"]*)"`)
	match := commentRegex.FindStringSubmatch(outStr)
	if match == nil {
		return nil
	}

	return ParseMetadataComment(match[1])
}

// ParseMetadataComment parses a metadata comment string.
func ParseMetadataComment(comment string) *TableMetadata {
	match := metadataRegex.FindStringSubmatch(comment)
	if match == nil {
		return nil
	}

	count, _ := strconv.Atoi(match[2])
	return &TableMetadata{
		Version:    match[1],
		ApplyCount: count,
		ConfigHash: match[3],
	}
}

// BuildMetadataComment creates a metadata comment string.
func BuildMetadataComment(applyCount int, configHash string) string {
	version := brand.Version
	if version == "" {
		version = "dev"
	}
	return fmt.Sprintf("glacic:v%s:c=%d:h=%s", version, applyCount, configHash)
}

// HashConfig generates a short hash of the config content.
func HashConfig(configContent []byte) string {
	h := sha256.Sum256(configContent)
	return fmt.Sprintf("%x", h[:4]) // First 8 hex chars
}

// GetNextApplyCount returns the current apply count + 1.
// If no metadata exists, returns 1.
func GetNextApplyCount(tableName, family string) int {
	meta := GetTableMetadata(tableName, family)
	if meta == nil {
		return 1
	}
	return meta.ApplyCount + 1
}

// FormatMetadataForDisplay returns a human-readable string of the metadata.
func FormatMetadataForDisplay(meta *TableMetadata) string {
	if meta == nil {
		return "no metadata"
	}
	return fmt.Sprintf("v%s, applied %d times, config hash: %s",
		meta.Version, meta.ApplyCount, meta.ConfigHash)
}
