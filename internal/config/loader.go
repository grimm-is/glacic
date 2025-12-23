package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclwrite"
)

// LoadOptions controls how configs are loaded
type LoadOptions struct {
	// AutoMigrate automatically migrates old configs to current version
	AutoMigrate bool

	// StrictVersion fails if config version doesn't match current
	StrictVersion bool

	// AllowUnknownFields ignores unknown HCL fields (useful for forward compat)
	AllowUnknownFields bool
}

// DefaultLoadOptions returns sensible defaults for loading configs
func DefaultLoadOptions() LoadOptions {
	return LoadOptions{
		AutoMigrate:        true,
		StrictVersion:      false,
		AllowUnknownFields: false,
	}
}

// LoadResult contains the loaded config and metadata about the load
type LoadResult struct {
	Config          *Config
	OriginalVersion SchemaVersion
	CurrentVersion  SchemaVersion
	WasMigrated     bool
	MigrationPath   []string // List of migrations applied
	Warnings        []string
}

// LoadFile loads a config file (HCL or JSON) with version handling
func LoadFile(path string) (*Config, error) {
	result, err := LoadFileWithOptions(path, DefaultLoadOptions())
	if err != nil {
		return nil, err
	}
	return result.Config, nil
}

// LoadFileWithOptions loads a config file with explicit options
func LoadFileWithOptions(path string, opts LoadOptions) (*LoadResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".hcl":
		return LoadHCLWithOptions(data, path, opts)
	case ".json":
		return LoadJSONWithOptions(data, opts)
	default:
		// Try HCL first, fall back to JSON
		result, err := LoadHCLWithOptions(data, path, opts)
		if err != nil {
			return LoadJSONWithOptions(data, opts)
		}
		return result, nil
	}
}

// LoadHCL loads config from HCL bytes
func LoadHCL(data []byte, filename string) (*Config, error) {
	result, err := LoadHCLWithOptions(data, filename, DefaultLoadOptions())
	if err != nil {
		return nil, err
	}
	return result.Config, nil
}

// LoadHCLWithOptions loads HCL with explicit options
func LoadHCLWithOptions(data []byte, filename string, opts LoadOptions) (*LoadResult, error) {
	// Apply pre-parse migrations (legacy HCL syntax transforms)
	transformedData, transforms := ApplyPreParseMigrations(data)

	// Detect deprecated features that can't be auto-transformed
	legacyFeatures := detectLegacyFeaturesInternal(transformedData)

	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL(transformedData, filename)
	if diags.HasErrors() {
		return nil, fmt.Errorf("HCL parse error: %s", diags.Error())
	}

	// First, extract just the version to determine which parser to use
	var versionProbe struct {
		SchemaVersion string `hcl:"schema_version,optional"`
	}
	_ = gohcl.DecodeBody(file.Body, nil, &versionProbe)

	// Track legacy transformations and deprecated features as warnings
	var warnings []string
	if len(transforms) > 0 {
		for _, t := range transforms {
			warnings = append(warnings, fmt.Sprintf("Legacy syntax transformed: %s", t))
		}
	}
	for _, f := range legacyFeatures {
		warnings = append(warnings, fmt.Sprintf("Deprecated: %s", f))
	}

	version, err := ParseVersion(versionProbe.SchemaVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid schema version: %w", err)
	}

	// Check if we support this version
	if !IsSupportedVersion(version) {
		return nil, fmt.Errorf("unsupported config schema version %s (supported: %v)",
			version, SupportedVersions)
	}

	// Parse with the appropriate version's schema
	cfg, err := parseHCLVersion(file, version, opts)
	if err != nil {
		return nil, err
	}

	result := &LoadResult{
		Config:          cfg,
		OriginalVersion: version,
		CurrentVersion:  version,
		Warnings:        warnings,
	}

	// Handle migration if needed
	currentVersion, _ := ParseVersion(CurrentSchemaVersion)
	if opts.AutoMigrate && version.NeedsMigration(currentVersion) {
		if err := MigrateToLatest(cfg); err != nil {
			return nil, fmt.Errorf("migration failed: %w", err)
		}
		result.CurrentVersion = currentVersion
		result.WasMigrated = true
	} else if opts.StrictVersion && version.Compare(currentVersion) != 0 {
		return nil, fmt.Errorf("config version %s does not match current version %s",
			version, currentVersion)
	}

	return result, nil
}

// parseHCLVersion parses HCL using the appropriate schema for the version
func parseHCLVersion(file *hcl.File, version SchemaVersion, opts LoadOptions) (*Config, error) {
	// For now, all 1.x versions use the same parser
	// When we introduce 2.x, we'll add version-specific parsing here
	switch version.Major {
	case 1:
		return parseHCLv1(file, opts)
	default:
		return nil, fmt.Errorf("no parser for schema version %s", version)
	}
}

// parseHCLv1 parses v1.x HCL configs
func parseHCLv1(file *hcl.File, opts LoadOptions) (*Config, error) {
	var cfg Config
	diags := gohcl.DecodeBody(file.Body, nil, &cfg)
	if diags.HasErrors() {
		return nil, fmt.Errorf("HCL decode error: %s", diags.Error())
	}

	// Set default version if not specified
	if cfg.SchemaVersion == "" {
		cfg.SchemaVersion = "1.0"
	}

	cfg.NormalizeZoneMappings()
	cfg.NormalizePolicies()

	// Apply post-load migrations (DNS server, zone canonicalization, etc.)
	if err := ApplyPostLoadMigrations(&cfg); err != nil {
		return nil, fmt.Errorf("post-load migration failed: %w", err)
	}

	return &cfg, nil
}

// LoadJSON loads config from JSON bytes
func LoadJSON(data []byte) (*Config, error) {
	result, err := LoadJSONWithOptions(data, DefaultLoadOptions())
	if err != nil {
		return nil, err
	}
	return result.Config, nil
}

// LoadJSONWithOptions loads JSON with explicit options
func LoadJSONWithOptions(data []byte, opts LoadOptions) (*LoadResult, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w", err)
	}

	cfg.NormalizeZoneMappings()
	cfg.NormalizePolicies()

	// Apply post-load migrations (DNS server, zone canonicalization, etc.)
	if err := ApplyPostLoadMigrations(&cfg); err != nil {
		return nil, fmt.Errorf("post-load migration failed: %w", err)
	}

	version, err := ParseVersion(cfg.SchemaVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid schema version: %w", err)
	}

	result := &LoadResult{
		Config:          &cfg,
		OriginalVersion: version,
		CurrentVersion:  version,
	}

	// Handle migration if needed
	currentVersion, _ := ParseVersion(CurrentSchemaVersion)
	if opts.AutoMigrate && version.NeedsMigration(currentVersion) {
		if err := MigrateToLatest(&cfg); err != nil {
			return nil, fmt.Errorf("migration failed: %w", err)
		}
		result.CurrentVersion = currentVersion
		result.WasMigrated = true
	}

	return result, nil
}

// SaveFile saves config to a file (format determined by extension)
func SaveFile(cfg *Config, path string) error {
	// Ensure version is set
	if cfg.SchemaVersion == "" {
		cfg.SchemaVersion = CurrentSchemaVersion
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		return SaveJSON(cfg, path)
	case ".hcl":
		return SaveHCL(cfg, path)
	default:
		return SaveJSON(cfg, path)
	}
}

// SaveJSON saves config as JSON
func SaveJSON(cfg *Config, path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// SaveHCL saves config as HCL using hclwrite for formatting
func SaveHCL(cfg *Config, path string) error {
	bytes, err := GenerateHCL(cfg)
	if err != nil {
		return err
	}

	// Create parent dir
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, bytes, 0644); err != nil {
		return fmt.Errorf("failed to write HCL file: %w", err)
	}

	return nil
}

// GenerateHCL generates HCL bytes from Config
func GenerateHCL(cfg *Config) ([]byte, error) {
	f := hclwrite.NewEmptyFile()
	gohcl.EncodeIntoBody(cfg, f.Body())
	return f.Bytes(), nil
}
