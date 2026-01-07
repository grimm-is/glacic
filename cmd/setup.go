package cmd

import (
	"os"

	"grimm.is/glacic/internal/auth"
	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/setup"
)

// RunSetup runs the initial setup wizard
func RunSetup(configDir string) {
	// Check if running as root
	if os.Geteuid() != 0 {
		Printer.Fprintf(os.Stderr, "Error: setup must run as root\n")
		os.Exit(1)
	}

	wizard := setup.NewWizard(configDir)

	// Check if already configured
	if !wizard.NeedsSetup() {
		Printer.Println("Firewall is already configured.")
		Printer.Println("To reconfigure, remove existing config.")
		os.Exit(0)
	}

	// Run auto setup
	result, err := wizard.RunAutoSetup()
	if err != nil {
		Printer.Fprintf(os.Stderr, "Setup failed: %v\n", err)
		os.Exit(1)
	}

	// Create auth store and prompt for admin password
	// Use default config dir for auth store
	authStore, err := auth.NewStore("")
	if err != nil {
		Printer.Fprintf(os.Stderr, "Warning: failed to initialize auth: %v\n", err)
	} else if !authStore.HasUsers() {
		Printer.Println("Creating admin user...")
		// For now, set a default password - in production, prompt interactively
		// or require setup via web UI
		if err := authStore.CreateUser("admin", "admin", auth.RoleAdmin); err != nil {
			Printer.Fprintf(os.Stderr, "Warning: failed to create admin user: %v\n", err)
		} else {
			Printer.Println("Default admin user created (username: admin, password: admin)")
			Printer.Println("IMPORTANT: Change the password immediately after first login!")
		}
	}

	Printer.Println()
	Printer.Println("Next steps:")
	Printer.Printf("  1. Start the firewall:  %s start\n", brand.LowerName)
	if result.LANInterface != "" {
		Printer.Printf("  2. Access the UI:       https://%s/\n", result.LANIP)
		Printer.Println("  3. Login with:          admin / admin")
		Printer.Println("  4. Change your password immediately!")
	} else {
		Printer.Println("  2. Login with:          admin / admin")
		Printer.Println("  3. Change your password immediately!")
	}
}

// RunFactoryReset performs a factory reset
func RunFactoryReset(configDir string, confirm bool) {
	if os.Geteuid() != 0 {
		Printer.Fprintf(os.Stderr, "Error: reset must run as root\n")
		os.Exit(1)
	}

	if configDir == "" {
		configDir = setup.DefaultConfigDir
	}

	if !confirm {
		Printer.Println("WARNING: This will delete all configuration and user data!")
		Printer.Println("To confirm, run: firewall reset --confirm")
		os.Exit(1)
	}

	// Remove config directory contents
	entries, err := os.ReadDir(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			Printer.Println("Nothing to reset - no configuration found.")
			os.Exit(0)
		}
		Printer.Fprintf(os.Stderr, "Error reading config dir: %v\n", err)
		os.Exit(1)
	}

	for _, entry := range entries {
		path := configDir + "/" + entry.Name()
		if err := os.RemoveAll(path); err != nil {
			Printer.Fprintf(os.Stderr, "Warning: failed to remove %s: %v\n", path, err)
		}
	}

	Printer.Println("Factory reset complete.")
	Printer.Println("Run 'firewall setup' to reconfigure.")
}
