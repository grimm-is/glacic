package config

import (
	"fmt"

	"github.com/hashicorp/hcl/v2/hclwrite"
)

// Migration represents a config schema migration from one version to another
type Migration struct {
	FromVersion SchemaVersion
	ToVersion   SchemaVersion
	Description string
	Migrate     func(*Config) error
	MigrateHCL  func(*hclwrite.File) error // AST-based migration (preserves comments)
}

// MigrationRegistry holds all registered migrations
type MigrationRegistry struct {
	migrations []Migration
}

// DefaultMigrations is the global migration registry
var DefaultMigrations = &MigrationRegistry{}

// Register adds a migration to the registry
func (r *MigrationRegistry) Register(m Migration) {
	r.migrations = append(r.migrations, m)
}

// GetMigrationPath returns the sequence of migrations needed to go from 'from' to 'to'
func (r *MigrationRegistry) GetMigrationPath(from, to SchemaVersion) ([]Migration, error) {
	if from.Compare(to) >= 0 {
		return nil, nil // No migration needed or downgrade (not supported)
	}

	var path []Migration
	current := from

	for current.Compare(to) < 0 {
		found := false
		for _, m := range r.migrations {
			if m.FromVersion.Compare(current) == 0 {
				path = append(path, m)
				current = m.ToVersion
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("no migration path from %s to %s (stuck at %s)",
				from, to, current)
		}
	}

	return path, nil
}

// MigrateConfig applies all necessary migrations to bring config to target version
func MigrateConfig(cfg *Config, targetVersion SchemaVersion) error {
	currentVersion, err := ParseVersion(cfg.SchemaVersion)
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
		if err := migration.Migrate(cfg); err != nil {
			return fmt.Errorf("migration %s -> %s failed: %w",
				migration.FromVersion, migration.ToVersion, err)
		}
		cfg.SchemaVersion = migration.ToVersion.String()
	}

	// Canonicalize config (clean up deprecated fields even within same version)
	if err := cfg.Canonicalize(); err != nil {
		return fmt.Errorf("canonicalization failed: %w", err)
	}

	return nil
}

// MigrateToLatest migrates config to the current schema version
func MigrateToLatest(cfg *Config) error {
	target, _ := ParseVersion(CurrentSchemaVersion)
	return MigrateConfig(cfg, target)
}

// RegisterMigrations registers all known migrations
// Currently no migrations are needed - schema 1.0 is the canonical version.
// When a new schema version requires migration, add it here.
func init() {
	// No migrations registered for 1.0 - it's the base version.
	// Future migrations would be registered like:
	// DefaultMigrations.Register(Migration{
	//   FromVersion: SchemaVersion{Major: 1, Minor: 0},
	//   ToVersion:   SchemaVersion{Major: 2, Minor: 0},
	//   Description: "...",
	//   ...
	// })
}
