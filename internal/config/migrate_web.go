package config

func init() {
	RegisterPostLoadMigration(PostLoadMigration{
		Name:        "MigrateAPIConfigToWeb",
		Description: "Migrates legacy API listener/TLS fields and Interface AccessWebUI to WebConfig",
		Migrate:     migrateLegacyAPIConfig,
	})
}

func migrateLegacyAPIConfig(cfg *Config) error {
	// Initialize Web config if missing
	if cfg.Web == nil {
		cfg.Web = &WebConfig{
			ServeUI:  true,
			ServeAPI: true,
		}
	}

	// 1. Migrate API fields if present and not overridden
	if cfg.API != nil {
		// API.Listen -> Web.Listen
		if cfg.Web.Listen == "" && cfg.API.Listen != "" {
			cfg.Web.Listen = cfg.API.Listen
		}

		// API.TLSListen -> Web.TLSListen
		if cfg.Web.TLSListen == "" && cfg.API.TLSListen != "" {
			cfg.Web.TLSListen = cfg.API.TLSListen
		}

		// API.TLSCert -> Web.TLSCert
		if cfg.Web.TLSCert == "" && cfg.API.TLSCert != "" {
			cfg.Web.TLSCert = cfg.API.TLSCert
		}

		// API.TLSKey -> Web.TLSKey
		if cfg.Web.TLSKey == "" && cfg.API.TLSKey != "" {
			cfg.Web.TLSKey = cfg.API.TLSKey
		}

		// API.DisableHTTPRedirect -> Web.DisableRedirect
		if cfg.API.DisableHTTPRedirect {
			cfg.Web.DisableRedirect = true
		}
	}

	// 2. Migrate Interface.AccessWebUI -> Web.Allow
	// Only if no explicit Allow/Deny rules are defined
	if len(cfg.Web.Allow) == 0 && len(cfg.Web.Deny) == 0 {
		var allowedIfaces []string
		for _, iface := range cfg.Interfaces {
			// Check legacy flag
			if iface.AccessWebUI {
				allowedIfaces = append(allowedIfaces, iface.Name)
			}
			// Check Management block (partial migration, Management block is preferred over AccessWebUI but also legacy)
			if iface.Management != nil && (iface.Management.Web || iface.Management.WebUI) {
				allowedIfaces = append(allowedIfaces, iface.Name)
			}
		}

		// 3. Migrate Zone Management -> Web.Allow (resolve interfaces from zones)
		// Note: resolving zone to interfaces requires logic similar to script builder.
		// For simplicity, we can't easily resolve Zone -> Interfaces here without ZoneResolver.
		// BUT we can use "interfaces = []" rule with the specific interfaces if we knew them.
		// OR we can just add a comment/rule?
		// HACK: The configuration object doesn't have the ZoneResolver loaded/map built.
		
		// If we skip Zone migration here, we MUST keep Zone support in script_builder.
		// This implies hybrid approach: cfg.Web is truth, BUT Zone.Management adds to it?
		// No, better to keep script_builder able to generate "legacy" rules if they exist.

		if len(allowedIfaces) > 0 {
			// Deduplicate not needed strictly as firewall handles it, but good hygiene?
			// For now, just append.
			cfg.Web.Allow = append(cfg.Web.Allow, AccessRule{
				Interfaces: allowedIfaces,
			})
		}
	}

	return nil
}
