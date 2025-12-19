// Package config provides HCL configuration handling with comment preservation.
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsimple"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

// ConfigFile represents an HCL configuration file with preserved source.
// This allows round-trip editing while preserving comments and formatting.
type ConfigFile struct {
	Path     string
	Config   *Config
	hclFile  *hclwrite.File
	original []byte
}

// LoadConfigFile loads an HCL config file, preserving the original source
// for round-trip editing with comments.
func LoadConfigFile(path string) (*ConfigFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	return LoadConfigFromBytes(path, data)
}

// LoadConfigFromBytes loads config from bytes, preserving source for round-trip.
func LoadConfigFromBytes(filename string, data []byte) (*ConfigFile, error) {
	// Parse for writing (preserves comments and formatting)
	hclFile, diags := hclwrite.ParseConfig(data, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to parse HCL for writing: %s", diags.Error())
	}

	// Parse for reading (into Go struct)
	var cfg Config
	if err := hclsimple.Decode(filename, data, nil, &cfg); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}

	return &ConfigFile{
		Path:     filename,
		Config:   &cfg,
		hclFile:  hclFile,
		original: data,
	}, nil
}

// Save writes the config back to disk, preserving comments where possible.
// If the config was modified via the structured API, it merges changes
// while trying to preserve original formatting and comments.
func (cf *ConfigFile) Save() error {
	return cf.SaveTo(cf.Path)
}

// SaveTo writes the config to a specific path.
func (cf *ConfigFile) SaveTo(path string) error {
	// Create backup of original file
	if _, err := os.Stat(path); err == nil {
		backupPath := path + ".bak"
		if err := copyFile(path, backupPath); err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write the HCL file
	data := cf.hclFile.Bytes()
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	cf.Path = path
	cf.original = data
	return nil
}

// GetRawHCL returns the current HCL source as a string.
func (cf *ConfigFile) GetRawHCL() string {
	return string(cf.hclFile.Bytes())
}

// SetRawHCL replaces the entire config with new HCL source.
// Returns an error if the HCL is invalid.
func (cf *ConfigFile) SetRawHCL(hclSource string) error {
	data := []byte(hclSource)

	// Validate by parsing
	newFile, diags := hclwrite.ParseConfig(data, cf.Path, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return fmt.Errorf("invalid HCL: %s", diags.Error())
	}

	// Also validate it decodes to our config struct
	var cfg Config
	if err := hclsimple.Decode(cf.Path, data, nil, &cfg); err != nil {
		return fmt.Errorf("HCL does not match config schema: %w", err)
	}

	cf.hclFile = newFile
	cf.Config = &cfg
	return nil
}

// GetSection returns the raw HCL for a specific section (e.g., "dhcp", "dns_server").
func (cf *ConfigFile) GetSection(sectionType string) (string, error) {
	body := cf.hclFile.Body()

	for _, block := range body.Blocks() {
		if block.Type() == sectionType {
			return formatBlock(block), nil
		}
	}

	return "", fmt.Errorf("section %q not found", sectionType)
}

// GetSectionByLabel returns raw HCL for a labeled block (e.g., interface "eth0").
func (cf *ConfigFile) GetSectionByLabel(sectionType string, labels []string) (string, error) {
	body := cf.hclFile.Body()

	for _, block := range body.Blocks() {
		if block.Type() == sectionType {
			blockLabels := block.Labels()

			// Match by all labels (exact count match)
			matchAll := true
			if len(labels) > 0 && len(blockLabels) == len(labels) {
				for i, l := range labels {
					if blockLabels[i] != l {
						matchAll = false
						break
					}
				}
				if matchAll {
					return formatBlock(block), nil
				}
			}

			// Fallback: match by name attribute if single label provided
			if len(labels) == 1 {
				attr := block.Body().GetAttribute("name")
				if attr != nil {
					val := strings.Trim(string(attr.Expr().BuildTokens(nil).Bytes()), "\" ")
					if val == labels[0] {
						return formatBlock(block), nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("section %s with labels %v not found", sectionType, labels)
}

// SetSection replaces a section with new HCL content.
// The sectionHCL should be a complete block definition.
func (cf *ConfigFile) SetSection(sectionType string, sectionHCL string) error {
	// Parse the new section
	newBlock, err := parseBlock(sectionHCL, cf.Path)
	if err != nil {
		return fmt.Errorf("invalid section HCL: %w", err)
	}

	if newBlock.Type() != sectionType {
		return fmt.Errorf("section type mismatch: expected %q, got %q", sectionType, newBlock.Type())
	}

	body := cf.hclFile.Body()

	// Find and remove existing block
	for _, block := range body.Blocks() {
		if block.Type() == sectionType {
			body.RemoveBlock(block)
			break
		}
	}

	// Append new block
	appendBlock(body, newBlock)

	// Re-decode to update Config struct
	return cf.reloadConfig()
}

// SetSectionByLabel replaces a labeled section with new HCL content.
func (cf *ConfigFile) SetSectionByLabel(sectionType string, labels []string, sectionHCL string) error {
	newBlock, err := parseBlock(sectionHCL, cf.Path)
	if err != nil {
		return fmt.Errorf("invalid section HCL: %w", err)
	}

	if newBlock.Type() != sectionType {
		return fmt.Errorf("section type mismatch: expected %q, got %q", sectionType, newBlock.Type())
	}

	body := cf.hclFile.Body()

	// Find and remove existing block with matching label or name (if fallback)
	found := false
	for _, block := range body.Blocks() {
		if block.Type() == sectionType {
			blockLabels := block.Labels()

			// Match by all labels (exact count match)
			matchAll := true
			if len(labels) > 0 && len(blockLabels) == len(labels) {
				for i, l := range labels {
					if blockLabels[i] != l {
						matchAll = false
						break
					}
				}
				if matchAll {
					body.RemoveBlock(block)
					found = true
					break
				}
			}

			// Fallback: match by name attribute if single label provided
			if !found && len(labels) == 1 {
				attr := block.Body().GetAttribute("name")
				if attr != nil {
					val := strings.Trim(string(attr.Expr().BuildTokens(nil).Bytes()), "\" ")
					if val == labels[0] {
						body.RemoveBlock(block)
						found = true
						break
					}
				}
			}
		}
	}

	// Append new block
	appendBlock(body, newBlock)

	return cf.reloadConfig()
}

// AddSection adds a new section to the config.
func (cf *ConfigFile) AddSection(sectionHCL string) error {
	newBlock, err := parseBlock(sectionHCL, cf.Path)
	if err != nil {
		return fmt.Errorf("invalid section HCL: %w", err)
	}

	body := cf.hclFile.Body()
	body.AppendNewline()
	appendBlock(body, newBlock)

	return cf.reloadConfig()
}

// RemoveSection removes a section by type.
func (cf *ConfigFile) RemoveSection(sectionType string) error {
	body := cf.hclFile.Body()

	for _, block := range body.Blocks() {
		if block.Type() == sectionType {
			body.RemoveBlock(block)
			return cf.reloadConfig()
		}
	}

	return fmt.Errorf("section %q not found", sectionType)
}

// RemoveSectionByLabel removes a labeled section.
// It matches if all provided labels match, OR if only one label is provided
// and it matches the 'name' attribute inside the block.
func (cf *ConfigFile) RemoveSectionByLabel(sectionType string, labels []string) error {
	body := cf.hclFile.Body()

	for _, block := range body.Blocks() {
		if block.Type() == sectionType {
			blockLabels := block.Labels()

			// Match by all labels (exact count match)
			matchAll := true
			if len(labels) > 0 && len(blockLabels) == len(labels) {
				for i, l := range labels {
					if blockLabels[i] != l {
						matchAll = false
						break
					}
				}
				if matchAll {
					body.RemoveBlock(block)
					return cf.reloadConfig()
				}
			}

			// Fallback: match by name attribute if single label provided
			if len(labels) == 1 {
				attr := block.Body().GetAttribute("name")
				if attr != nil {
					// Extract string value from tokens
					val := strings.Trim(string(attr.Expr().BuildTokens(nil).Bytes()), "\" ")
					if val == labels[0] {
						body.RemoveBlock(block)
						return cf.reloadConfig()
					}
				}
			}
		}
	}

	return fmt.Errorf("section %s with labels %v not found", sectionType, labels)
}

// ListSections returns all top-level section types and their labels.
func (cf *ConfigFile) ListSections() []SectionInfo {
	var sections []SectionInfo
	body := cf.hclFile.Body()

	for _, block := range body.Blocks() {
		info := SectionInfo{
			Type: block.Type(),
		}
		if labels := block.Labels(); len(labels) > 0 {
			info.Labels = labels
			info.Label = strings.Join(labels, " ")
		}
		sections = append(sections, info)
	}

	return sections
}

// SectionInfo describes a config section.
type SectionInfo struct {
	Type   string   `json:"type"`
	Labels []string `json:"labels,omitempty"`
	Label  string   `json:"label,omitempty"` // For backward compatibility, joined by space
}

// ValidateHCL validates HCL source without modifying the config.
func ValidateHCL(hclSource string) error {
	data := []byte(hclSource)

	// Check syntax
	_, diags := hclwrite.ParseConfig(data, "validate.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return fmt.Errorf("syntax error: %s", diags.Error())
	}

	// Check schema
	var cfg Config
	if err := hclsimple.Decode("validate.hcl", data, nil, &cfg); err != nil {
		return fmt.Errorf("schema error: %w", err)
	}

	return nil
}

// ValidateSection validates a single section's HCL.
func ValidateSection(sectionType, sectionHCL string) error {
	_, err := parseBlock(sectionHCL, "validate.hcl")
	return err
}

// FormatHCL formats HCL source code.
func FormatHCL(hclSource string) (string, error) {
	data := []byte(hclSource)

	file, diags := hclwrite.ParseConfig(data, "format.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return "", fmt.Errorf("invalid HCL: %s", diags.Error())
	}

	return string(file.Bytes()), nil
}

// reloadConfig re-decodes the HCL into the Config struct.
func (cf *ConfigFile) reloadConfig() error {
	data := cf.hclFile.Bytes()
	var cfg Config
	if err := hclsimple.Decode(cf.Path, data, nil, &cfg); err != nil {
		return fmt.Errorf("failed to reload config: %w", err)
	}
	cf.Config = &cfg
	return nil
}

// MigrateToLatest migrates the config file to the latest schema version,
// preserving comments and formatting.
func (cf *ConfigFile) MigrateToLatest() error {
	target, _ := ParseVersion(CurrentSchemaVersion)
	return cf.MigrateTo(target)
}

// MigrateTo migrates the config file to a specific schema version,
// preserving comments and formatting.
func (cf *ConfigFile) MigrateTo(targetVersion SchemaVersion) error {
	currentVersion, err := ParseVersion(cf.Config.SchemaVersion)
	if err != nil {
		return fmt.Errorf("invalid config schema version: %w", err)
	}

	if currentVersion.Compare(targetVersion) >= 0 {
		return nil // Already at or above target version
	}

	path, err := DefaultMigrations.GetMigrationPath(currentVersion, targetVersion)
	if err != nil {
		return err
	}

	for _, migration := range path {
		// Run AST migration if defined
		if migration.MigrateHCL != nil {
			if err := migration.MigrateHCL(cf.hclFile); err != nil {
				return fmt.Errorf("HCL migration %s -> %s failed: %w",
					migration.FromVersion, migration.ToVersion, err)
			}
		}

		// Always update schema_version attribute
		// We do this via AST to preserve formatting
		cf.hclFile.Body().SetAttributeValue("schema_version", cty.StringVal(migration.ToVersion.String()))

		// Update internal struct state (re-decode)
		// This is less efficient but safer to ensure struct matches AST
		if err := cf.reloadConfig(); err != nil {
			return fmt.Errorf("failed to reload config after migration %s -> %s: %w",
				migration.FromVersion, migration.ToVersion, err)
		}
	}

	return nil
}

// Helper functions

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func formatBlock(block *hclwrite.Block) string {
	f := hclwrite.NewEmptyFile()
	appendBlock(f.Body(), block)
	return string(f.Bytes())
}

func parseBlock(hclSource, filename string) (*hclwrite.Block, error) {
	data := []byte(hclSource)

	file, diags := hclwrite.ParseConfig(data, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse error: %s", diags.Error())
	}

	blocks := file.Body().Blocks()
	if len(blocks) == 0 {
		return nil, fmt.Errorf("no block found in HCL")
	}
	if len(blocks) > 1 {
		return nil, fmt.Errorf("expected single block, got %d", len(blocks))
	}

	return blocks[0], nil
}

func appendBlock(body *hclwrite.Body, src *hclwrite.Block) {
	newBlock := body.AppendNewBlock(src.Type(), src.Labels())
	srcBody := src.Body()
	dstBody := newBlock.Body()

	// Copy attributes
	for name, attr := range srcBody.Attributes() {
		dstBody.SetAttributeRaw(name, attr.Expr().BuildTokens(nil))
	}

	// Copy nested blocks
	for _, nested := range srcBody.Blocks() {
		appendBlock(dstBody, nested)
	}
}

// NewConfigFile creates a new empty config file.
func NewConfigFile(path string) *ConfigFile {
	return &ConfigFile{
		Path:    path,
		Config:  &Config{},
		hclFile: hclwrite.NewEmptyFile(),
	}
}

// SetAttribute sets a top-level attribute (e.g., ip_forwarding = true).
func (cf *ConfigFile) SetAttribute(name string, value interface{}) error {
	body := cf.hclFile.Body()

	ctyVal, err := toCtyValue(value)
	if err != nil {
		return fmt.Errorf("invalid value for %s: %w", name, err)
	}

	body.SetAttributeValue(name, ctyVal)
	return cf.reloadConfig()
}

// toCtyValue converts a Go value to a cty.Value for HCL writing.
func toCtyValue(v interface{}) (cty.Value, error) {
	switch val := v.(type) {
	case bool:
		return cty.BoolVal(val), nil
	case int:
		return cty.NumberIntVal(int64(val)), nil
	case int64:
		return cty.NumberIntVal(val), nil
	case float64:
		return cty.NumberFloatVal(val), nil
	case string:
		return cty.StringVal(val), nil
	case []string:
		if len(val) == 0 {
			return cty.ListValEmpty(cty.String), nil
		}
		vals := make([]cty.Value, len(val))
		for i, s := range val {
			vals[i] = cty.StringVal(s)
		}
		return cty.ListVal(vals), nil
	default:
		return cty.NilVal, fmt.Errorf("unsupported type: %T", v)
	}
}

// GetConfigWithMetadata returns the config along with file metadata.
type ConfigMetadata struct {
	Path         string        `json:"path"`
	LastModified time.Time     `json:"last_modified"`
	Size         int64         `json:"size"`
	Sections     []SectionInfo `json:"sections"`
}

func (cf *ConfigFile) GetMetadata() ConfigMetadata {
	meta := ConfigMetadata{
		Path:     cf.Path,
		Sections: cf.ListSections(),
	}

	if info, err := os.Stat(cf.Path); err == nil {
		meta.LastModified = info.ModTime()
		meta.Size = info.Size()
	}

	return meta
}

// Diff returns a simple diff between original and current HCL.
func (cf *ConfigFile) Diff() string {
	current := cf.hclFile.Bytes()
	if bytes.Equal(cf.original, current) {
		return ""
	}

	// Simple line-by-line diff
	origLines := strings.Split(string(cf.original), "\n")
	currLines := strings.Split(string(current), "\n")

	var diff strings.Builder
	diff.WriteString("--- original\n")
	diff.WriteString("+++ modified\n")

	// Very simple diff - just show changed lines
	maxLines := len(origLines)
	if len(currLines) > maxLines {
		maxLines = len(currLines)
	}

	for i := 0; i < maxLines; i++ {
		origLine := ""
		currLine := ""
		if i < len(origLines) {
			origLine = origLines[i]
		}
		if i < len(currLines) {
			currLine = currLines[i]
		}

		if origLine != currLine {
			if origLine != "" {
				diff.WriteString(fmt.Sprintf("-%s\n", origLine))
			}
			if currLine != "" {
				diff.WriteString(fmt.Sprintf("+%s\n", currLine))
			}
		}
	}

	return diff.String()
}

// HasChanges returns true if the config has been modified since loading.
func (cf *ConfigFile) HasChanges() bool {
	return !bytes.Equal(cf.original, cf.hclFile.Bytes())
}

// Reload discards changes and reloads from disk.
func (cf *ConfigFile) Reload() error {
	newCf, err := LoadConfigFile(cf.Path)
	if err != nil {
		return err
	}
	*cf = *newCf
	return nil
}

// ParseHCLDiagnostics parses HCL and returns detailed diagnostics.
type HCLDiagnostic struct {
	Severity string `json:"severity"` // "error" or "warning"
	Summary  string `json:"summary"`
	Detail   string `json:"detail,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
}

func ParseHCLWithDiagnostics(hclSource string) ([]HCLDiagnostic, error) {
	data := []byte(hclSource)
	parser := hclparse.NewParser()

	_, diags := parser.ParseHCL(data, "input.hcl")

	var result []HCLDiagnostic
	for _, d := range diags {
		diag := HCLDiagnostic{
			Summary: d.Summary,
			Detail:  d.Detail,
		}
		if d.Severity == hcl.DiagError {
			diag.Severity = "error"
		} else {
			diag.Severity = "warning"
		}
		if d.Subject != nil {
			diag.Line = d.Subject.Start.Line
			diag.Column = d.Subject.Start.Column
		}
		result = append(result, diag)
	}

	if diags.HasErrors() {
		return result, fmt.Errorf("HCL has errors")
	}
	return result, nil
}
