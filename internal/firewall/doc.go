// Package firewall implements nftables-based firewall management for Glacic.
//
// # Overview
//
// This package generates and applies firewall rules using nftables. Rules are
// generated as a complete script and applied atomically to prevent windows of
// vulnerability during updates.
//
// # Architecture
//
//	Config → ScriptBuilder → nftables script → AtomicApplier → Kernel
//
// # Key Types
//
//   - [Manager]: Main firewall manager that applies configs and monitors integrity
//   - [Config]: Firewall-specific configuration extracted from global config
//   - [ScriptBuilder]: Fluent builder for generating nftables scripts
//   - [AtomicApplier]: Applies scripts atomically via nft -f
//
// # Safe Mode
//
// The package provides emergency lockdown capability via safe mode:
//   - Pre-rendered ruleset for instant application
//   - Allows LAN management access (SSH, Web UI, API)
//   - Drops ALL forwarding (kills existing connections)
//   - Maintains VPN connectivity for remote management
//
// See [BuildSafeModeScript] and [Manager.ApplySafeMode].
//
// # Rule Generation
//
// Rules are organized by chain:
//   - INPUT: Traffic to the firewall itself
//   - OUTPUT: Traffic from the firewall
//   - FORWARD: Traffic passing through (routing)
//
// Each zone policy generates appropriate rules based on source/destination zones.
//
// # Example
//
//	fwMgr, _ := firewall.NewManager(logger, cacheDir)
//	fwMgr.PreRenderSafeMode(cfg)
//	fwMgr.ApplySafeMode()  // Secure baseline
//	fwMgr.ApplyConfig(cfg) // Full rules
package firewall
