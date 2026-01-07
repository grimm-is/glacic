package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"grimm.is/glacic/internal/api"
	"grimm.is/glacic/internal/api/storage"
	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/state"
)

// APIKeyCommand handles API key management from the CLI.
type APIKeyCommand struct {
	store   storage.APIKeyStore
	manager *api.APIKeyManager
}

// NewAPIKeyCommand creates a new API key command handler.
// Uses persistent state store so keys survive restarts.
func NewAPIKeyCommand() (*APIKeyCommand, error) {
	// Initialize state store - MUST match the path used by the API server
	// API server uses: brand.GetStateDir() + "/api_state.db"
	stateDir := brand.GetStateDir()
	dbPath := stateDir + "/api_state.db"
	stateStore, err := state.NewSQLiteStore(state.Options{Path: dbPath})
	if err != nil {
		return nil, fmt.Errorf("failed to open state store at %s: %w", dbPath, err)
	}

	// Use persistent store
	persistentStore, err := storage.NewStateAPIKeyStore(stateStore)
	if err != nil {
		return nil, fmt.Errorf("failed to create API key store: %w", err)
	}

	return &APIKeyCommand{
		store:   persistentStore,
		manager: api.NewAPIKeyManager(persistentStore),
	}, nil
}

// Run executes the API key command.
func (c *APIKeyCommand) Run(args []string) error {
	if len(args) == 0 {
		return c.usage()
	}

	switch args[0] {
	case "generate", "create":
		return c.generate(args[1:])
	case "list", "ls":
		return c.list()
	case "revoke", "delete":
		return c.revoke(args[1:])
	case "help":
		return c.usage()
	default:
		return fmt.Errorf("unknown subcommand: %s", args[0])
	}
}

func (c *APIKeyCommand) usage() error {
	Printer.Printf(`%s API Key Management

Usage:
  %s apikey <command> [options]

Commands:
  generate    Generate a new API key
  list        List all API keys
  revoke      Revoke an API key
  help        Show this help

Generate Options:
  --name <name>           Name for the key (required)
  --permissions <perms>   Comma-separated permissions (required)
  --expires <duration>    Expiration (e.g., "24h", "7d", "30d")
  --allowed-ips <ips>     Comma-separated allowed IPs
  --rate-limit <rpm>      Requests per minute limit
  --description <desc>    Description
  --json, -j              Output in JSON format (for scripting)

Permission Values:
  Read:   config:read, uplinks:read, firewall:read, dhcp:read, dns:read,
          learning:read, metrics:read, health:read, logs:read, read:*
  Write:  config:write, uplinks:write, firewall:write, dhcp:write,
          dns:write, learning:write, write:*
  Admin:  admin:keys, admin:system, admin:backup, admin:*
  All:    * (full access)

Preset Permissions:
  --preset readonly       Read-only access to all resources
  --preset uplink         Uplink management (read/write uplinks, read health)
  --preset monitoring     Metrics, health, and logs (read-only)
  --preset admin          Full administrative access

Examples:
  # Generate a read-only key
  %s apikey generate --name "monitoring" --preset readonly

  # Generate an uplink control key for your script
  %s apikey generate --name "vpn-switcher" --preset uplink --expires 365d

  # Generate a full admin key
  %s apikey generate --name "admin-key" --preset admin

  # Generate a key with specific permissions
  %s apikey generate --name "custom" --permissions "uplinks:read,uplinks:write"

  # Generate a key restricted to specific IPs
  %s apikey generate --name "local-only" --preset admin --allowed-ips "127.0.0.1,192.168.1.0/24"

`, brand.Name, brand.BinaryName, brand.BinaryName, brand.BinaryName, brand.BinaryName, brand.BinaryName, brand.BinaryName)
	return nil
}

func (c *APIKeyCommand) generate(args []string) error {
	var (
		name        string
		permissions []storage.Permission
		expiresIn   string
		allowedIPs  []string
		rateLimit   int
		description string
		preset      string
		jsonOutput  bool
	)

	// Parse arguments
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name", "-n":
			if i+1 >= len(args) {
				return fmt.Errorf("--name requires a value")
			}
			i++
			name = args[i]
		case "--permissions", "-p":
			if i+1 >= len(args) {
				return fmt.Errorf("--permissions requires a value")
			}
			i++
			for _, p := range strings.Split(args[i], ",") {
				permissions = append(permissions, storage.Permission(strings.TrimSpace(p)))
			}
		case "--preset":
			if i+1 >= len(args) {
				return fmt.Errorf("--preset requires a value")
			}
			i++
			preset = args[i]
		case "--expires", "-e":
			if i+1 >= len(args) {
				return fmt.Errorf("--expires requires a value")
			}
			i++
			expiresIn = args[i]
		case "--allowed-ips":
			if i+1 >= len(args) {
				return fmt.Errorf("--allowed-ips requires a value")
			}
			i++
			allowedIPs = strings.Split(args[i], ",")
		case "--rate-limit":
			if i+1 >= len(args) {
				return fmt.Errorf("--rate-limit requires a value")
			}
			i++
			fmt.Sscanf(args[i], "%d", &rateLimit)
		case "--description", "-d":
			if i+1 >= len(args) {
				return fmt.Errorf("--description requires a value")
			}
			i++
			description = args[i]
		case "--json", "-j":
			jsonOutput = true
		default:
			return fmt.Errorf("unknown option: %s", args[i])
		}
	}

	// Validate
	if name == "" {
		return fmt.Errorf("--name is required")
	}

	// Apply preset
	if preset != "" {
		switch preset {
		case "readonly", "read-only":
			permissions = api.ReadOnlyPermissions()
		case "uplink", "uplinks":
			permissions = api.UplinkControlPermissions()
		case "monitoring", "monitor":
			permissions = api.MonitoringPermissions()
		case "admin", "full":
			permissions = api.FullAdminPermissions()
		default:
			return fmt.Errorf("unknown preset: %s", preset)
		}
	}

	if len(permissions) == 0 {
		return fmt.Errorf("--permissions or --preset is required")
	}

	// Build options
	var opts []api.KeyOption
	if description != "" {
		opts = append(opts, api.WithDescription(description))
	}
	if expiresIn != "" {
		duration, err := parseDuration(expiresIn)
		if err != nil {
			return fmt.Errorf("invalid --expires value: %w", err)
		}
		opts = append(opts, api.WithExpiryDuration(duration))
	}
	if len(allowedIPs) > 0 {
		opts = append(opts, api.WithAllowedIPs(allowedIPs))
	}
	if rateLimit > 0 {
		opts = append(opts, api.WithRateLimit(rateLimit))
	}

	// Generate the key
	fullKey, keyInfo, err := c.manager.GenerateKey(name, permissions, opts...)
	if err != nil {
		return fmt.Errorf("failed to generate key: %w", err)
	}

	// JSON output for scripting
	if jsonOutput {
		output := map[string]interface{}{
			"api_key":     fullKey,
			"id":          keyInfo.ID,
			"name":        keyInfo.Name,
			"prefix":      keyInfo.KeyPrefix,
			"permissions": keyInfo.Permissions,
			"created_at":  keyInfo.CreatedAt.Format(time.RFC3339),
		}
		if keyInfo.ExpiresAt != nil {
			output["expires_at"] = keyInfo.ExpiresAt.Format(time.RFC3339)
		}
		if len(keyInfo.AllowedIPs) > 0 {
			output["allowed_ips"] = keyInfo.AllowedIPs
		}
		if keyInfo.RateLimit > 0 {
			output["rate_limit"] = keyInfo.RateLimit
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	}

	// Human-readable output
	Printer.Println()
	Printer.Println("╔════════════════════════════════════════════════════════════════╗")
	Printer.Println("║                    API KEY GENERATED                           ║")
	Printer.Println("╠════════════════════════════════════════════════════════════════╣")
	Printer.Println("║  ⚠️  SAVE THIS KEY NOW - IT WILL NOT BE SHOWN AGAIN!           ║")
	Printer.Println("╚════════════════════════════════════════════════════════════════╝")
	Printer.Println()
	Printer.Printf("API Key: %s\n", fullKey)
	Printer.Println()
	Printer.Println("Key Details:")
	Printer.Printf("  ID:          %s\n", keyInfo.ID)
	Printer.Printf("  Name:        %s\n", keyInfo.Name)
	Printer.Printf("  Prefix:      %s\n", keyInfo.KeyPrefix)
	Printer.Printf("  Permissions: %v\n", keyInfo.Permissions)
	if keyInfo.ExpiresAt != nil {
		Printer.Printf("  Expires:     %s\n", keyInfo.ExpiresAt.Format(time.RFC3339))
	}
	if len(keyInfo.AllowedIPs) > 0 {
		Printer.Printf("  Allowed IPs: %v\n", keyInfo.AllowedIPs)
	}
	if keyInfo.RateLimit > 0 {
		Printer.Printf("  Rate Limit:  %d req/min\n", keyInfo.RateLimit)
	}
	Printer.Println()
	Printer.Println("Usage:")
	Printer.Printf("  curl -H \"Authorization: Bearer %s\" http://localhost:8080/api/...\n", fullKey)
	Printer.Printf("  curl -H \"X-API-Key: %s\" http://localhost:8080/api/...\n", fullKey)
	Printer.Println()

	return nil
}

func (c *APIKeyCommand) list() error {
	keys, err := c.store.List()
	if err != nil {
		return err
	}

	if len(keys) == 0 {
		Printer.Println("No API keys found.")
		return nil
	}

	// JSON output for now
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(keys)
}

func (c *APIKeyCommand) revoke(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("key ID required")
	}

	id := args[0]
	if err := c.store.Delete(id); err != nil {
		return err
	}

	Printer.Printf("Key %s revoked.\n", id)
	return nil
}

func parseDuration(s string) (time.Duration, error) {
	// Handle days specially
	if strings.HasSuffix(s, "d") {
		days := strings.TrimSuffix(s, "d")
		var d int
		if _, err := fmt.Sscanf(days, "%d", &d); err != nil {
			return 0, err
		}
		return time.Duration(d) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// RunAPIKey is the entry point for the API key management command.
func RunAPIKey(args []string) {
	cmd, err := NewAPIKeyCommand()
	if err != nil {
		Printer.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := cmd.Run(args); err != nil {
		Printer.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
