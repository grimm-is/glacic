package main

import (
	"embed"
	"flag"
	"fmt"
	"os"

	"grimm.is/glacic/cmd"
	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/client"
)

//go:embed all:ui/dist
var uiAssets embed.FS

func main() {
	// Check for upgrade standby mode via environment variable
	// This allows us to run cleanly without visible --upgrade-standby flags
	if os.Getenv("GLACIC_UPGRADE_STANDBY") == "1" {
		// Prevent infinite recursion in children (like glacic api) by cleaning the env
		os.Unsetenv("GLACIC_UPGRADE_STANDBY")

		standbyFlags := flag.NewFlagSet("upgrade-standby", flag.ExitOnError)
		configFile := standbyFlags.String("config", brand.DefaultConfigDir+"/"+brand.ConfigFileName, "Configuration file")
		// Parse args, assuming they might start with --config or be compliant with standard flag parsing
		// If argv is: [glacic, --config, foo], Parse(os.Args[1:]) should work.
		// Note check len(os.Args)
		if len(os.Args) > 1 {
			standbyFlags.Parse(os.Args[1:])
		}
		cmd.RunUpgradeStandby(*configFile, uiAssets)
		return
	}

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "start":
		// Start the daemon (background by default)
		startFlags := flag.NewFlagSet("start", flag.ExitOnError)
		configFile := startFlags.String("config", brand.DefaultConfigDir+"/"+brand.ConfigFileName, "Configuration file")
		foreground := startFlags.Bool("foreground", false, "Run in foreground (don't daemonize)")
		dryRun := startFlags.Bool("dryrun", false, "Dry run - print rules without applying")
		startFlags.BoolVar(dryRun, "n", false, "Dry run (short)")
		startFlags.Parse(os.Args[2:])

		if *foreground {
			// Foreground mode: run ctl directly
			if err := cmd.RunCtl(*configFile, false, "", *dryRun, nil); err != nil {
				fmt.Fprintf(os.Stderr, "Start failed: %v\n", err)
				os.Exit(1)
			}
		} else {
			// Background mode: daemonize
			if err := cmd.RunStart(*configFile); err != nil {
				fmt.Fprintf(os.Stderr, "Start failed: %v\n", err)
				os.Exit(1)
			}
		}

	case "stop":
		// Stop the daemon
		if err := cmd.RunStop(); err != nil {
			fmt.Fprintf(os.Stderr, "Stop failed: %v\n", err)
			os.Exit(1)
		}

	case "ctl":
		// Privileged control plane daemon
		ctlFlags := flag.NewFlagSet("ctl", flag.ExitOnError)
		stateDir := ctlFlags.String("state-dir", "", "Override state directory")
		dryRun := ctlFlags.Bool("dryrun", false, "Dry run - print rules without applying")
		ctlFlags.BoolVar(dryRun, "n", false, "Dry run (short)")
		ctlFlags.Parse(os.Args[2:])

		configFile := brand.DefaultConfigDir + "/" + brand.ConfigFileName
		if len(ctlFlags.Args()) > 0 {
			configFile = ctlFlags.Arg(0)
		}

		if err := cmd.RunCtl(configFile, false, *stateDir, *dryRun, nil); err != nil {
			fmt.Fprintf(os.Stderr, "Control plane failed: %v\n", err)
			os.Exit(1)
		}

	case "apply":
		// Apply config (one-shot, alias to ctl but usually implies foreground/dryrun check)
		// For now, it aliases ctl behavior but emphasizes config application
		applyFlags := flag.NewFlagSet("apply", flag.ExitOnError)
		dryRun := applyFlags.Bool("dryrun", false, "Dry run - print rules without applying")
		applyFlags.BoolVar(dryRun, "n", false, "Dry run (short)")
		applyFlags.Parse(os.Args[2:])

		configFile := brand.DefaultConfigDir + "/" + brand.ConfigFileName
		if len(applyFlags.Args()) > 0 {
			configFile = applyFlags.Arg(0)
		}

		// Apply runs ctl? Ctl starts the daemon.
		// If the user wants "apply changes to running daemon", they should use `glacic config apply` via API?
		// Or does "apply" start the daemon?
		// Given the request "implement runmodes for check, apply, diff", and "Add --dryrun (-n) to apply".
		// I'll assume apply runs RunCtl (which applies usage).
		if err := cmd.RunCtl(configFile, false, "", *dryRun, nil); err != nil {
			fmt.Fprintf(os.Stderr, "Apply failed: %v\n", err)
			os.Exit(1)
		}

	case "check":
		checkFlags := flag.NewFlagSet("check", flag.ExitOnError)
		verbose := checkFlags.Bool("v", false, "Verbose output")
		checkFlags.BoolVar(verbose, "verbose", false, "Verbose output (long)")
		checkFlags.Parse(os.Args[2:])

		configFile := brand.DefaultConfigDir + "/" + brand.ConfigFileName
		if len(checkFlags.Args()) > 0 {
			configFile = checkFlags.Arg(0)
		}

		if err := cmd.RunCheck(configFile, *verbose); err != nil {
			fmt.Fprintf(os.Stderr, "Check failed: %v\n", err)
			os.Exit(1)
		}

	case "log":
		// Delegate to cmd.RunLog for detailed flag parsing
		if err := cmd.RunLog(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}

	case "diff":
		if len(os.Args) < 3 {
			fmt.Println("Usage: " + brand.BinaryName + " diff <config-file>")
			os.Exit(1)
		}
		if err := cmd.RunDiff(os.Args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}

	case "show":
		showFlags := flag.NewFlagSet("show", flag.ExitOnError)
		summary := showFlags.Bool("summary", false, "Show configuration summary")
		remote := showFlags.String("remote", "", "Remote Glacic API URL (e.g., https://192.168.1.1:8080)")
		apiKey := showFlags.String("api-key", "", "API key for remote authentication")
		showFlags.Parse(os.Args[2:])

		var configFile string
		if len(showFlags.Args()) > 0 {
			configFile = showFlags.Arg(0)
		}
		if err := cmd.RunShow(configFile, *summary, *remote, *apiKey); err != nil {
			fmt.Fprintf(os.Stderr, "Show failed: %v\n", err)
			os.Exit(1)
		}

	case "_api-server":
		// Internal: Unprivileged API server (spawned by control plane)
		// This is not a public command - use `glacic start` instead
		apiFlags := flag.NewFlagSet("_api-server", flag.ExitOnError)
		listenAddr := apiFlags.String("listen", ":8080", "Address to listen on")
		dropUser := apiFlags.String("user", "", "Drop privileges to this user")
		apiFlags.Parse(os.Args[2:])

		cmd.RunAPI(uiAssets, *listenAddr, *dropUser)

	case "test-api":
		// Test mode: Run API server with sandbox disabled for integration testing
		// Usage: glacic test-api [-listen :8080]
		apiFlags := flag.NewFlagSet("test-api", flag.ExitOnError)
		listenAddr := apiFlags.String("listen", ":8080", "Address to listen on")
		apiFlags.Parse(os.Args[2:])

		// Force sandbox bypass for test environment
		os.Setenv("GLACIC_NO_SANDBOX", "1")
		cmd.RunAPI(uiAssets, *listenAddr, "")

	case "api", "apikey":
		// API key management (api is an alias for apikey)
		cmd.RunAPIKey(os.Args[2:])

	case "test":
		// Test mode: run ctl with test flag (for CI/integration tests)
		if len(os.Args) < 3 {
			fmt.Println("Usage: " + brand.BinaryName + " test <config-file>")
			os.Exit(1)
		}
		if err := cmd.RunCtl(os.Args[2], true, "", false, nil); err != nil { // Added nil argument
			fmt.Fprintf(os.Stderr, "Test run failed: %v\n", err)
			os.Exit(1)
		}

	case "monitor":
		// Test/Monitor mode
		// args: monitor <config-file>
		if len(os.Args) < 3 {
			fmt.Println("Usage: " + brand.BinaryName + " monitor <config-file>")
			os.Exit(1)
		}
		cmd.RunMonitor(os.Args[2:])

	case "import":
		// Import wizard
		cmd.RunImport(os.Args[2:])

	case "setup":
		// First-run setup wizard
		setupFlags := flag.NewFlagSet("setup", flag.ExitOnError)
		configDir := setupFlags.String("config-dir", brand.DefaultConfigDir, "Configuration directory")
		setupFlags.Parse(os.Args[2:])
		cmd.RunSetup(*configDir)

	case "reset":
		// Factory reset
		resetFlags := flag.NewFlagSet("reset", flag.ExitOnError)
		confirm := resetFlags.Bool("confirm", false, "Confirm factory reset")
		configDir := resetFlags.String("config-dir", brand.DefaultConfigDir, "Configuration directory")
		resetFlags.Parse(os.Args[2:])
		cmd.RunFactoryReset(*configDir, *confirm)

	case "console":
		// TUI Console
		cmd.RunConsole()

	case "config":
		// Configuration management commands
		cmd.RunConfig(os.Args[2:])

	case "status":
		// Query daemon status
		cmd.RunStatus()

	case "restart":
		// Restart daemon (alias for upgrade --self)
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error determining current binary path: %v\n", err)
			os.Exit(1)
		}
		configFile := brand.DefaultConfigDir + "/" + brand.ConfigFileName
		cmd.RunUpgrade(exe, configFile)

	case "version":
		// Print version info
		fmt.Printf("%s version %s\n", brand.Name, brand.Version)
		fmt.Printf("Build: %s\n", brand.BuildTime)

	case "ipset":
		// IPSet management commands
		cmd.RunIPSet(os.Args[2:])

	case "upgrade":
		// Seamless upgrade with socket handoff (local or remote)
		upgradeFlags := flag.NewFlagSet("upgrade", flag.ExitOnError)
		newBinary := upgradeFlags.String("binary", "", "Path to binary (required for remote upgrade)")
		selfUpgrade := upgradeFlags.Bool("self", false, "Restart using the current binary")
		configFile := upgradeFlags.String("config", brand.DefaultConfigDir+"/"+brand.ConfigFileName, "Configuration file")
		remoteURL := upgradeFlags.String("remote", "", "Remote URL to upgrade (e.g., https://192.168.1.1:8443)")
		apiKey := upgradeFlags.String("api-key", "", "API key for remote authentication")
		upgradeFlags.Parse(os.Args[2:])

		// Remote upgrade mode
		if *remoteURL != "" {
			if *newBinary == "" {
				fmt.Fprintln(os.Stderr, "Error: --binary is required for remote upgrade")
				fmt.Println("Usage: " + brand.BinaryName + " upgrade --remote <url> --binary <path> --api-key <key>")
				os.Exit(1)
			}
			if *apiKey == "" {
				fmt.Fprintln(os.Stderr, "Error: --api-key is required for remote upgrade")
				os.Exit(1)
			}

			// Create client
			apiClient := client.NewHTTPClient(*remoteURL, client.WithAPIKey(*apiKey))

			// Auto-detect remote architecture from status
			fmt.Println("Checking remote system...")
			status, err := apiClient.GetStatus()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get remote status: %v\n", err)
				os.Exit(1)
			}

			remoteArch := status.BuildArch
			if remoteArch == "" {
				fmt.Fprintln(os.Stderr, "Error: remote did not report architecture")
				os.Exit(1)
			}

			fmt.Printf("Remote: %s (version %s, arch %s)\n", *remoteURL, status.Version, remoteArch)

			// Upload binary
			result, err := apiClient.UploadBinary(*newBinary, remoteArch)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Remote upgrade failed: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("Remote upgrade initiated successfully!\n")
			fmt.Printf("  Bytes uploaded: %d\n", result.Bytes)
			fmt.Printf("  Checksum: %s\n", result.Checksum)
			fmt.Printf("  Message: %s\n", result.Message)
			os.Exit(0)
		}

		// Local upgrade mode
		if *selfUpgrade {
			exe, err := os.Executable()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error determining current binary path: %v\n", err)
				os.Exit(1)
			}
			*newBinary = exe
		}

		// Auto-detect staged binary if --binary not specified
		if *newBinary == "" {
			stagedPath := "/usr/sbin/" + brand.BinaryName + "_new"
			if info, err := os.Stat(stagedPath); err == nil && !info.IsDir() {
				fmt.Printf("Detected staged binary: %s\n", stagedPath)
				*newBinary = stagedPath
			}
		}

		if *newBinary == "" {
			fmt.Println("Usage: " + brand.BinaryName + " upgrade --binary <path-to-new-binary> OR --self")
			fmt.Println("       Or place new binary at /usr/sbin/" + brand.BinaryName + "_new and run without arguments")
			fmt.Println("\nRemote: " + brand.BinaryName + " upgrade --remote <url> --binary <path> --api-key <key>")
			os.Exit(1)
		}
		cmd.RunUpgrade(*newBinary, *configFile)

	case "reload":
		// Reload running daemon
		reloadFlags := flag.NewFlagSet("reload", flag.ExitOnError)
		configFile := reloadFlags.String("config", brand.DefaultConfigDir+"/"+brand.ConfigFileName, "Configuration file")
		reloadFlags.Parse(os.Args[2:])

		// If user provides a config file argument, use it as override or just validation?
		// RunReload takes config file to validate.
		if len(reloadFlags.Args()) > 0 {
			// e.g. glacic reload myconfig.hcl
			*configFile = reloadFlags.Arg(0)
		}

		if err := cmd.RunReload(*configFile); err != nil {
			fmt.Fprintf(os.Stderr, "Reload failed: %v\n", err)
			os.Exit(1)
		}

	case "help", "-h", "--help":
		// Help command
		if len(os.Args) > 2 {
			// Command-specific help: glacic help <command>
			switch os.Args[2] {
			case "api", "apikey":
				cmd.RunAPIKey([]string{"help"})
			case "ipset":
				cmd.RunIPSet([]string{"help"})
			case "config":
				cmd.RunConfig([]string{"help"})
			default:
				fmt.Printf("No detailed help available for '%s'\n", os.Args[2])
				printUsage()
			}
		} else {
			printUsage()
		}

	default:
		fmt.Printf("Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`%s - %s

Usage:
  %s <command> [options]

Core Commands:
  start     Start the firewall daemon
            Options: --foreground, --dryrun/-n, --config <file>
  stop      Stop the running daemon
  reload    Reload configuration (hot reload)
  status    Show daemon status

Management Commands:
  api       Manage API keys (alias: apikey)
            Subcommands: generate, list, revoke, help
  config    Manage firewall configuration (HCL)
            Subcommands: show, edit, validate, export
  ipset     Manage IPSet blocklists
            Subcommands: list, update, add, remove, info

Utility Commands:
  check     Validate configuration file
            Options: -v (verbose/dry-run simulation)
  show      Display firewall rules
            Options: --summary, --remote <url>, --api-key <key>
  log       View and stream system logs
            Options: -f (follow), -n (lines), --remote <url>
  diff      Compare two configuration files
  import    Import configuration from other firewalls
  console   Interactive TUI dashboard
  setup     First-run setup wizard
  reset     Factory reset (--confirm required)
  upgrade   Seamless upgrade with socket handoff
            Options: --binary <path>, --self

Examples:
  %s start                          # Start in background
  %s start --foreground             # Start in foreground (debug)
  %s start --dryrun                 # Dry run (show what would happen)
  %s stop                           # Stop the daemon
  %s api generate --name "monitor" --preset readonly
  %s check -v /etc/glacic/glacic.hcl
  %s show --remote https://192.168.1.1:8080 --api-key xxx
  %s log -f

For command-specific help: %s help <command>
`,
		brand.Name, brand.Description,
		brand.LowerName,
		brand.LowerName, brand.LowerName, brand.LowerName, brand.LowerName,
		brand.LowerName, brand.LowerName, brand.LowerName, brand.LowerName,
		brand.LowerName)
}
