package config

// migrate_registry.go provides infrastructure for organizing config migrations.
// Migrations are organized into two types:
//   - Pre-parse: Transform HCL text before parsing (syntax updates)
//   - Post-load: Transform Config struct after parsing (semantic updates)

// PreParseMigration transforms raw HCL text before parsing.
type PreParseMigration struct {
	Name        string
	Description string
	Transform   func([]byte) ([]byte, []string)
}

// PostLoadMigration transforms a parsed Config struct.
type PostLoadMigration struct {
	Name        string
	Description string
	Migrate     func(*Config) error
}

// preParseMigrations holds all pre-parse migrations in order
var preParseMigrations []PreParseMigration

// postLoadMigrations holds all post-load migrations in order
var postLoadMigrations []PostLoadMigration

// RegisterPreParseMigration adds a pre-parse migration
func RegisterPreParseMigration(m PreParseMigration) {
	preParseMigrations = append(preParseMigrations, m)
}

// RegisterPostLoadMigration adds a post-load migration
func RegisterPostLoadMigration(m PostLoadMigration) {
	postLoadMigrations = append(postLoadMigrations, m)
}

// ApplyPreParseMigrations runs all pre-parse migrations on HCL data.
func ApplyPreParseMigrations(data []byte) ([]byte, []string) {
	var allApplied []string
	result := data

	for _, m := range preParseMigrations {
		var applied []string
		result, applied = m.Transform(result)
		allApplied = append(allApplied, applied...)
	}

	return result, allApplied
}

// ApplyPostLoadMigrations runs all post-load migrations on a Config.
func ApplyPostLoadMigrations(cfg *Config) error {
	for _, m := range postLoadMigrations {
		if err := m.Migrate(cfg); err != nil {
			return err
		}
	}
	return nil
}
