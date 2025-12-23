package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/ctlplane"
)

// RunConfig handles configuration CLI commands
func RunConfig(args []string) {
	if len(args) < 1 {
		printConfigUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "get":
		runConfigGet(args[1:])
	case "set":
		runConfigSet(args[1:])
	case "validate":
		runConfigValidate(args[1:])
	case "edit":
		runConfigEdit(args[1:])
	case "delete":
		runConfigDelete(args[1:])
	case "migrate":
		runConfigMigrate(args[1:])
	default:
		fmt.Printf("Unknown config command: %s\n\n", args[0])
		printConfigUsage()
		os.Exit(1)
	}
}

func runConfigGet(args []string) {
	flags := flag.NewFlagSet("config get", flag.ExitOnError)
	output := flags.String("output", "hcl", "Output format: hcl, json")
	flags.Parse(args)

	// Connect to control plane
	client, err := ctlplane.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to control plane: %v\n", err)
		fmt.Fprintf(os.Stderr, "Make sure 'firewall ctl' is running first.\n")
		os.Exit(1)
	}
	defer client.Close()

	// Get current HCL configuration
	reply, err := client.GetRawHCL()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get configuration: %v\n", err)
		os.Exit(1)
	}

	switch *output {
	case "hcl":
		fmt.Print(reply.HCL)
	case "json":
		fmt.Printf(`{"hcl": %q}`, reply.HCL)
	default:
		fmt.Fprintf(os.Stderr, "Invalid output format: %s\n", *output)
		os.Exit(1)
	}
}

func runConfigSet(args []string) {
	flags := flag.NewFlagSet("config set", flag.ExitOnError)
	file := flags.String("file", "", "Configuration file to read (default: stdin)")
	confirm := flags.Bool("confirm", true, "Ask for confirmation before applying changes")
	flags.Parse(args)

	var hclContent string
	var err error

	if *file != "" {
		// Read from file
		content, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read config file: %v\n", err)
			os.Exit(1)
		}
		hclContent = string(content)
	} else {
		// Read from stdin
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read from stdin: %v\n", err)
			os.Exit(1)
		}
		hclContent = string(content)
	}

	// Validate HCL first
	if err := validateHCLContent(hclContent); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration validation failed: %v\n", err)
		os.Exit(1)
	}

	// Ask for confirmation if enabled
	if *confirm {
		fmt.Print("Apply this configuration? [y/N]: ")
		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
			fmt.Println("Aborted")
			return
		}
	}

	// Connect to control plane
	client, err := ctlplane.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to control plane: %v\n", err)
		fmt.Fprintf(os.Stderr, "Make sure 'firewall ctl' is running first.\n")
		os.Exit(1)
	}
	defer client.Close()

	// Create backup before applying changes
	fmt.Println("Creating backup before applying changes...")
	backupReply, err := client.CreateBackup("pre-config-set backup", false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to create backup: %v\n", err)
	} else if backupReply.Error != "" {
		fmt.Fprintf(os.Stderr, "Warning: Failed to create backup: %s\n", backupReply.Error)
	} else if backupReply.Success {
		fmt.Printf("Backup created: version %d\n", backupReply.Backup.Version)
	}

	// Apply the configuration
	reply, err := client.SetRawHCL(hclContent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to set configuration: %v\n", err)
		os.Exit(1)
	}

	if reply.Error != "" {
		fmt.Fprintf(os.Stderr, "Error applying configuration: %s\n", reply.Error)
		if backupReply.Success {
			fmt.Fprintf(os.Stderr, "You can restore from backup version %d if needed\n", backupReply.Backup.Version)
		}
		os.Exit(1)
	}

	fmt.Println("Configuration applied successfully")

	if reply.RestartHint != "" {
		fmt.Println()
		fmt.Println("TIP: " + reply.RestartHint)
	}
}

func runConfigValidate(args []string) {
	flags := flag.NewFlagSet("config validate", flag.ExitOnError)
	file := flags.String("file", "", "Configuration file to validate (default: stdin)")
	flags.Parse(args)

	var hclContent string

	if *file != "" {
		// Read from file
		content, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read config file: %v\n", err)
			os.Exit(1)
		}
		hclContent = string(content)
	} else {
		// Read from stdin
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read from stdin: %v\n", err)
			os.Exit(1)
		}
		hclContent = string(content)
	}

	// Validate HCL
	if err := validateHCLContent(hclContent); err != nil {
		fmt.Fprintf(os.Stderr, "Validation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Configuration is valid")
}

func runConfigEdit(args []string) {
	flags := flag.NewFlagSet("config edit", flag.ExitOnError)
	editor := flags.String("editor", "", "Editor to use (default: $EDITOR or vi)")
	flags.Parse(args)

	// Determine editor
	if *editor == "" {
		*editor = os.Getenv("EDITOR")
		if *editor == "" {
			*editor = "vi"
		}
	}

	// Connect to control plane
	client, err := ctlplane.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to control plane: %v\n", err)
		fmt.Fprintf(os.Stderr, "Make sure 'firewall ctl' is running first.\n")
		os.Exit(1)
	}
	defer client.Close()

	// Get current configuration
	reply, err := client.GetRawHCL()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get configuration: %v\n", err)
		os.Exit(1)
	}

	// Create temporary file
	tmpFile, err := os.CreateTemp("", "firewall-config-*.hcl")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create temporary file: %v\n", err)
		os.Exit(1)
	}
	tmpFileName := tmpFile.Name()
	defer os.Remove(tmpFileName)

	// Write current config to temp file
	if _, err := tmpFile.WriteString(reply.HCL); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write temporary file: %v\n", err)
		os.Exit(1)
	}
	tmpFile.Close()

	// Get original content for comparison
	originalContent := reply.HCL

	// Launch editor
	for {
		cmd := exec.Command(*editor, tmpFileName)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Editor failed: %v\n", err)
			os.Exit(1)
		}

		// Read edited content
		editedContent, err := os.ReadFile(tmpFileName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read edited file: %v\n", err)
			os.Exit(1)
		}

		// Check if content changed
		if string(editedContent) == originalContent {
			fmt.Println("No changes made")
			return
		}

		// Validate edited content
		if err := validateHCLContent(string(editedContent)); err != nil {
			fmt.Fprintf(os.Stderr, "Validation failed: %v\n", err)
			fmt.Printf("Options: [r]etry, [a]bort, [e]dit again: ")

			var choice string
			fmt.Scanln(&choice)
			switch strings.ToLower(choice) {
			case "r", "retry":
				continue
			case "a", "abort":
				fmt.Println("Aborted")
				return
			case "e", "edit":
				continue
			default:
				fmt.Println("Aborted")
				return
			}
		}

		// Apply the configuration
		reply, err := client.SetRawHCL(string(editedContent))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to apply configuration: %v\n", err)
			os.Exit(1)
		}

		if reply.Error != "" {
			fmt.Fprintf(os.Stderr, "Error applying configuration: %s\n", reply.Error)
			os.Exit(1)
		}

		fmt.Println("Configuration applied successfully")
		return
	}
}

func runConfigDelete(args []string) {
	flags := flag.NewFlagSet("config delete", flag.ExitOnError)
	confirm := flags.Bool("confirm", true, "Ask for confirmation before deletion")
	flags.Parse(args)

	if flags.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: firewall config delete <section-type> [label1] [label2]...\n")
		fmt.Fprintf(os.Stderr, "Examples: firewall config delete dhcp\n")
		fmt.Fprintf(os.Stderr, "          firewall config delete interface eth0\n")
		fmt.Fprintf(os.Stderr, "          firewall config delete policy lan wan\n")
		fmt.Fprintf(os.Stderr, "          firewall config delete policy \"allow ssh\" (by name)\n")
		os.Exit(1)
	}

	sectionType := flags.Arg(0)
	labels := flags.Args()[1:]

	// Ask for confirmation
	if *confirm {
		fmt.Printf("Delete %s", sectionType)
		if len(labels) > 0 {
			fmt.Printf(" with labels %v", labels)
		}
		fmt.Printf("? [y/N]: ")

		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
			fmt.Println("Aborted")
			return
		}
	}

	// Connect to control plane
	client, err := ctlplane.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to control plane: %v\n", err)
		fmt.Fprintf(os.Stderr, "Make sure 'firewall ctl' is running first.\n")
		os.Exit(1)
	}
	defer client.Close()

	// Create backup before deletion
	fmt.Println("Creating backup before deletion...")
	backupReply, err := client.CreateBackup("pre-config-delete backup", false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to create backup: %v\n", err)
	} else if backupReply.Error != "" {
		fmt.Fprintf(os.Stderr, "Warning: Failed to create backup: %s\n", backupReply.Error)
	} else if backupReply.Success {
		fmt.Printf("Backup created: version %d\n", backupReply.Backup.Version)
	}

	// Delete the section
	var reply *ctlplane.DeleteSectionReply
	if len(labels) > 0 {
		reply, err = client.DeleteSectionByLabel(sectionType, labels...)
	} else {
		reply, err = client.DeleteSection(sectionType)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to delete section: %v\n", err)
		os.Exit(1)
	}

	if reply.Error != "" {
		fmt.Fprintf(os.Stderr, "Error deleting section: %s\n", reply.Error)
		if backupReply.Success {
			fmt.Fprintf(os.Stderr, "You can restore from backup version %d if needed\n", backupReply.Backup.Version)
		}
		os.Exit(1)
	}

	fmt.Printf("Successfully deleted %s", sectionType)
	if len(labels) > 0 {
		fmt.Printf(" with labels %v", labels)
	}
	fmt.Println()
}

func validateHCLContent(hcl string) error {
	// Connect to control plane
	client, err := ctlplane.NewClient()
	if err != nil {
		return fmt.Errorf("failed to connect to control plane: %w", err)
	}
	defer client.Close()

	// Validate HCL
	reply, err := client.ValidateHCL(hcl)
	if err != nil {
		return fmt.Errorf("failed to validate HCL: %w", err)
	}

	if reply.Error != "" {
		return fmt.Errorf("HCL validation error: %s", reply.Error)
	}

	return nil
}

func printConfigUsage() {
	fmt.Println("Firewall Configuration Management")
	fmt.Println()
	fmt.Println("Usage: firewall config <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  get        Get current configuration")
	fmt.Println("  set        Set configuration from stdin (or file with -file)")
	fmt.Println("  validate   Validate configuration from stdin (or file with -file)")
	fmt.Println("  edit       Edit configuration in $EDITOR")
	fmt.Println("  delete     Delete a specific configuration section")
	fmt.Println("  migrate    Migrate configuration to the latest schema version")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -output    Output format for 'get' (hcl, json) [default: hcl]")
	fmt.Println("  -file      Configuration file for 'set', 'validate', 'migrate' [default: stdin/firewall.hcl]")
	fmt.Println("  -out       Output file for 'migrate' [default: overwrite input]")
	fmt.Println("  -dry-run   Print migrated config to stdout without saving")
	fmt.Println("  -editor    Editor for 'edit' [default: $EDITOR or vi]")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  firewall config get")
	fmt.Println("  firewall config get -output json")
	fmt.Println("  cat new-config.hcl | firewall config set")
	fmt.Println("  firewall config validate < test.hcl")
	fmt.Println("  firewall config set -file new-config.hcl")
	fmt.Println("  firewall config delete dhcp")
	fmt.Println("  firewall config delete interface eth0")
	fmt.Println("  firewall config edit")
	fmt.Println("  firewall config migrate -file old.hcl -out new.hcl")
	fmt.Println("  firewall config migrate -dry-run")
	fmt.Println()
	fmt.Println("Unix-style usage:")
	fmt.Println("  echo 'ip_forwarding = true' | firewall config set")
	fmt.Println("  curl -s https://example.com/config.hcl | firewall config set")
}

func runConfigMigrate(args []string) {
	flags := flag.NewFlagSet("config migrate", flag.ExitOnError)
	inputFile := flags.String("file", "firewall.hcl", "Configuration file to migrate")
	outFile := flags.String("out", "", "Output file (default: overwrite input file)")
	dryRun := flags.Bool("dry-run", false, "Print migrated config to stdout without saving")
	flags.Parse(args)

	// Load config preserving comments
	cf, err := config.LoadConfigFile(*inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config file: %v\n", err)
		os.Exit(1)
	}

	// Migrate (version schema)
	err = cf.MigrateToLatest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
		os.Exit(1)
	}

	// Canonicalize (clean up deprecated fields)
	err = cf.Config.Canonicalize()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Canonicalization failed: %v\n", err)
		os.Exit(1)
	}

	// Regenerate HCL from struct to enforce canonical form
	// Note: This effectively strips comments, but guarantees valid/canonical HCL syntax
	rawHCL, err := config.GenerateHCL(cf.Config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate HCL: %v\n", err)
		os.Exit(1)
	}

	// Handle output
	if *dryRun {
		fmt.Print(string(rawHCL))
		return
	}

	targetFile := *inputFile
	if *outFile != "" {
		targetFile = *outFile
	}

	// Save
	if err := os.WriteFile(targetFile, rawHCL, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save migrated config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully migrated %s to schema version %s\n", targetFile, cf.Config.SchemaVersion)
}
