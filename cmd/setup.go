package cmd

import (
	"fmt"
	"os"

	"grimm.is/glacic/internal/auth"
	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/setup"
)

// RunSetup runs the initial setup wizard
func RunSetup(configDir string) {
	// Check if running as root
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "Error: setup must run as root")
		os.Exit(1)
	}

	wizard := setup.NewWizard(configDir)

	// Check if already configured
	if !wizard.NeedsSetup() {
		fmt.Println("Firewall is already configured.")
		fmt.Println("To reconfigure, remove existing config.")
		os.Exit(0)
	}

	// Run auto setup
	result, err := wizard.RunAutoSetup()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Setup failed: %v\n", err)
		os.Exit(1)
	}

	// Create auth store and prompt for admin password
	// Use default config dir for auth store
	authStore, err := auth.NewStore("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize auth: %v\n", err)
	} else if !authStore.HasUsers() {
		fmt.Println("Creating admin user...")
		// For now, set a default password - in production, prompt interactively
		// or require setup via web UI
		if err := authStore.CreateUser("admin", "admin", auth.RoleAdmin); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create admin user: %v\n", err)
		} else {
			fmt.Println("Default admin user created (username: admin, password: admin)")
			fmt.Println("IMPORTANT: Change the password immediately after first login!")
		}
	}

	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  1. Start the firewall:  %s start\n", brand.LowerName)
	if result.LANInterface != "" {
		fmt.Printf("  2. Access the UI:       https://%s/\n", result.LANIP)
		fmt.Println("  3. Login with:          admin / admin")
		fmt.Println("  4. Change your password immediately!")
	} else {
		fmt.Println("  2. Login with:          admin / admin")
		fmt.Println("  3. Change your password immediately!")
	}
}

// RunFactoryReset performs a factory reset
func RunFactoryReset(configDir string, confirm bool) {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "Error: reset must run as root")
		os.Exit(1)
	}

	if configDir == "" {
		configDir = setup.DefaultConfigDir
	}

	if !confirm {
		fmt.Println("WARNING: This will delete all configuration and user data!")
		fmt.Println("To confirm, run: firewall reset --confirm")
		os.Exit(1)
	}

	// Remove config directory contents
	entries, err := os.ReadDir(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Nothing to reset - no configuration found.")
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "Error reading config dir: %v\n", err)
		os.Exit(1)
	}

	for _, entry := range entries {
		path := configDir + "/" + entry.Name()
		if err := os.RemoveAll(path); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove %s: %v\n", path, err)
		}
	}

	fmt.Println("Factory reset complete.")
	fmt.Println("Run 'firewall setup' to reconfigure.")
}
