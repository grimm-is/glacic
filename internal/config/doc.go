// Package config handles HCL configuration parsing, validation, and management.
//
// # Overview
//
// Glacic uses HCL (HashiCorp Configuration Language) for its configuration files.
// This package provides:
//   - HCL parsing with schema validation
//   - Configuration migration between schema versions
//   - Forgiving parsing for recovery from corrupted configs
//   - Safe mode hints for boot-time recovery
//
// # Key Types
//
//   - [Config]: Main configuration struct with all settings
//   - [LoadResult]: Result of loading a config file (includes migration info)
//   - [ForgivingLoadResult]: Result of forgiving parse with skipped blocks
//   - [SafeModeHints]: Cached metadata for safe mode boot
//
// # Configuration Blocks
//
// Main HCL blocks:
//   - interface: Network interface configuration
//   - zone: Firewall zone definition
//   - policy: Zone-to-zone traffic rules
//   - dhcp: DHCP server scopes
//   - dns: DNS server settings
//   - nat: NAT and port forwarding
//   - api: API server configuration
//
// # Time-of-Day Rules (Policy)
//
// Policy rules support time-based matching using nftables meta expressions.
// Requires kernel 5.4+ with nftables support for meta hour/day.
//
// Fields:
//   - time_start: Start time in HH:MM format (e.g., "09:00")
//   - time_end: End time in HH:MM format (e.g., "17:00")
//   - days: List of day names (e.g., ["Monday", "Tuesday"])
//
// Example:
//
//	policy "lan" "wan" {
//	    rule "block_during_work" {
//	        action = "drop"
//	        time_start = "09:00"
//	        time_end = "17:00"
//	        days = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday"]
//	        dest_port = 8888
//	    }
//	}
//
// # Schema Versioning
//
// Configs include a schema_version field. When loading older configs,
// automatic migration is attempted. See [CurrentSchemaVersion].
//
// # Forgiving Parser
//
// When normal parsing fails, [LoadForgiving] attempts to salvage the config
// by iteratively commenting out broken blocks until parsing succeeds.
// This allows the system to boot into safe mode for recovery.
//
// # Example
//
//	result, err := config.LoadFileWithOptions(path, config.DefaultLoadOptions())
//	if err != nil {
//	    // Try forgiving parse
//	    data, _ := os.ReadFile(path)
//	    forgiving := config.LoadForgiving(data, path)
//	    cfg = forgiving.Config
//	}
package config
