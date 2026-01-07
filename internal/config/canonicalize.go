package config

// Canonicalize cleans up the configuration by migrating deprecated fields
// to their canonical representations.
//
// This method delegates to ApplyPostLoadMigrations() which runs all registered
// post-load migrations including zone canonicalization.
func (c *Config) Canonicalize() error {
	return ApplyPostLoadMigrations(c)
}
