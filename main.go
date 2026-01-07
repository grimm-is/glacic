package main

import (
	"embed"
	"flag"
	"os"

	"grimm.is/glacic/cmd"
	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/client"
	"grimm.is/glacic/internal/toolbox/dhcp"
	"grimm.is/glacic/internal/toolbox/mcast"
	"grimm.is/glacic/internal/toolbox/mdns"
	"grimm.is/glacic/internal/i18n"
)

//go:embed all:ui/dist
var uiAssets embed.FS

var printer = i18n.NewCLIPrinter()

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
		startFlags.StringVar(configFile, "c", brand.DefaultConfigDir+"/"+brand.ConfigFileName, "Configuration file (short)")
		
		foreground := startFlags.Bool("foreground", false, "Run in foreground (don't daemonize)")
		startFlags.BoolVar(foreground, "f", false, "Run in foreground (short)")
		
		dryRun := startFlags.Bool("dry-run", false, "Dry run - print rules without applying")
		startFlags.BoolVar(dryRun, "n", false, "Dry run (short)")
		// Legacy support (hidden)
		startFlags.BoolVar(dryRun, "dryrun", false, "Dry run (legacy)")
		
		startFlags.Parse(os.Args[2:])

		if *foreground {
			// Foreground mode: run ctl directly
			if err := cmd.RunCtl(*configFile, false, "", *dryRun, nil); err != nil {
				printer.Fprintf(os.Stderr, "Start failed: %v\n", err)
				os.Exit(1)
			}
		} else {
			// Background mode: daemonize
			if err := cmd.RunStart(*configFile); err != nil {
				printer.Fprintf(os.Stderr, "Start failed: %v\n", err)
				os.Exit(1)
			}
		}

	case "stop":
		// Stop the daemon
		if err := cmd.RunStop(); err != nil {
			printer.Fprintf(os.Stderr, "Stop failed: %v\n", err)
			os.Exit(1)
		}

	case "ctl":
		// Privileged control plane daemon
		ctlFlags := flag.NewFlagSet("ctl", flag.ExitOnError)
		stateDir := ctlFlags.String("state-dir", "", "Override state directory")
		
		dryRun := ctlFlags.Bool("dry-run", false, "Dry run - print rules without applying")
		ctlFlags.BoolVar(dryRun, "n", false, "Dry run (short)")
		// Legacy support
		ctlFlags.BoolVar(dryRun, "dryrun", false, "Dry run (legacy)")
		
		ctlFlags.Parse(os.Args[2:])

		configFile := brand.DefaultConfigDir + "/" + brand.ConfigFileName
		if len(ctlFlags.Args()) > 0 {
			configFile = ctlFlags.Arg(0)
		}

		if err := cmd.RunCtl(configFile, false, *stateDir, *dryRun, nil); err != nil {
			printer.Fprintf(os.Stderr, "Control plane failed: %v\n", err)
			os.Exit(1)
		}

	case "apply":
		// Apply config (one-shot, alias to ctl but usually implies foreground/dryrun check)
		// For now, it aliases ctl behavior but emphasizes config application
		applyFlags := flag.NewFlagSet("apply", flag.ExitOnError)
		dryRun := applyFlags.Bool("dry-run", false, "Dry run - print rules without applying")
		applyFlags.BoolVar(dryRun, "n", false, "Dry run (short)")
		// Legacy support
		applyFlags.BoolVar(dryRun, "dryrun", false, "Dry run (legacy)")
		
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
			printer.Fprintf(os.Stderr, "Apply failed: %v\n", err)
			os.Exit(1)
		}

	case "check":
		checkFlags := flag.NewFlagSet("check", flag.ExitOnError)
		verbose := checkFlags.Bool("verbose", false, "Verbose output")
		checkFlags.BoolVar(verbose, "v", false, "Verbose output (short)")
		checkFlags.Parse(os.Args[2:])

		configFile := brand.DefaultConfigDir + "/" + brand.ConfigFileName
		if len(checkFlags.Args()) > 0 {
			configFile = checkFlags.Arg(0)
		}

		if err := cmd.RunCheck(configFile, *verbose); err != nil {
			printer.Fprintf(os.Stderr, "Check failed: %v\n", err)
			os.Exit(1)
		}

	case "log":
		// Delegate to cmd.RunLog for detailed flag parsing
		if err := cmd.RunLog(os.Args[2:]); err != nil {
			printer.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}

	case "diff":
		if len(os.Args) < 3 {
			printer.Println("Usage: " + brand.BinaryName + " diff <config-file>")
			os.Exit(1)
		}
		if err := cmd.RunDiff(os.Args[2]); err != nil {
			printer.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}

	case "show":
		showFlags := flag.NewFlagSet("show", flag.ExitOnError)
		summary := showFlags.Bool("summary", false, "Show configuration summary")
		showFlags.BoolVar(summary, "s", false, "Show configuration summary (short)")
		
		remote := showFlags.String("remote", "", "Remote Glacic API URL (e.g., https://192.168.1.1:8443)")
		showFlags.StringVar(remote, "r", "", "Remote Glacic API URL (short)")
		
		apiKey := showFlags.String("api-key", "", "API key for remote authentication")
		showFlags.StringVar(apiKey, "k", "", "API key (short)")
		
		showFlags.Parse(os.Args[2:])

		var configFile string
		if len(showFlags.Args()) > 0 {
			configFile = showFlags.Arg(0)
		}
		if err := cmd.RunShow(configFile, *summary, *remote, *apiKey); err != nil {
			printer.Fprintf(os.Stderr, "Show failed: %v\n", err)
			os.Exit(1)
		}

	case "_api-server":
		// Internal: Unprivileged API server (spawned by control plane)
		// This is not a public command - use `glacic start` instead
		apiFlags := flag.NewFlagSet("_api-server", flag.ExitOnError)
		listenAddr := apiFlags.String("listen", ":8080", "HTTP address to listen on (for redirect)")
		dropUser := apiFlags.String("user", "", "Drop privileges to this user")
		apiFlags.Parse(os.Args[2:])

		cmd.RunAPI(uiAssets, *listenAddr, *dropUser)

	case "_proxy":
		// Internal: Unprivileged TCP Proxy (spawned by control plane)
		cmd.RunProxy(os.Args[2:])


	case "test-api":
		// Test mode: Run API server with sandbox disabled for integration testing
		// Usage: glacic test-api [-listen :8080]
		apiFlags := flag.NewFlagSet("test-api", flag.ExitOnError)
		listenAddr := apiFlags.String("listen", ":8080", "Address to listen on")
		noTLS := apiFlags.Bool("no-tls", false, "Disable TLS (force HTTP)")
		apiFlags.Parse(os.Args[2:])

		// Force sandbox bypass for test environment
		os.Setenv("GLACIC_NO_SANDBOX", "1")
		if *noTLS {
			os.Setenv("GLACIC_NO_TLS", "1")
		}
		cmd.RunAPI(uiAssets, *listenAddr, "")

	case "api", "apikey":
		// API key management (api is an alias for apikey)
		cmd.RunAPIKey(os.Args[2:])

	case "test":
		// Test mode: run ctl with test flag (for CI/integration tests)
		if len(os.Args) < 3 {
			printer.Println("Usage: " + brand.BinaryName + " test <config-file>")
			os.Exit(1)
		}
		if err := cmd.RunCtl(os.Args[2], true, "", false, nil); err != nil { // Added nil argument
			printer.Fprintf(os.Stderr, "Test run failed: %v\n", err)
			os.Exit(1)
		}

	case "monitor":
		// Test/Monitor mode
		// args: monitor <config-file>
		if len(os.Args) < 3 {
			printer.Println("Usage: " + brand.BinaryName + " monitor <config-file>")
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
		setupFlags.StringVar(configDir, "d", brand.DefaultConfigDir, "Configuration directory (short)")
		
		setupFlags.Parse(os.Args[2:])
		cmd.RunSetup(*configDir)

	case "reset":
		// Factory reset
		resetFlags := flag.NewFlagSet("reset", flag.ExitOnError)
		confirm := resetFlags.Bool("confirm", false, "Confirm factory reset")
		resetFlags.BoolVar(confirm, "y", false, "Confirm factory reset (short)")
		
		configDir := resetFlags.String("config-dir", brand.DefaultConfigDir, "Configuration directory")
		resetFlags.StringVar(configDir, "d", brand.DefaultConfigDir, "Configuration directory (short)")
		
		resetFlags.Parse(os.Args[2:])
		cmd.RunFactoryReset(*configDir, *confirm)

	case "console":
		// TUI Console
		consoleFlags := flag.NewFlagSet("console", flag.ExitOnError)
		remote := consoleFlags.String("remote", "", "Remote Glacic API URL")
		apiKey := consoleFlags.String("api-key", "", "API Key for remote authentication")
		insecure := consoleFlags.Bool("insecure", false, "Skip TLS verification")
		debug := consoleFlags.Bool("debug", false, "Enable debug logging to tui.log")
		consoleFlags.Parse(os.Args[2:])
		
		cmd.RunConsole(*remote, *apiKey, *insecure, *debug)

	case "config":
		// Configuration management commands
		cmd.RunConfig(os.Args[2:])

	case "status":
		// Query daemon status
		cmd.RunStatus()



	case "mcast":
		// Integration test helper: mDNS multicast tool
		if err := mcast.Run(os.Args[2:]); err != nil {
			printer.Fprintf(os.Stderr, "mcast failed: %v\n", err)
			os.Exit(1)
		}

	case "mdns-publish":
		// Integration test helper: mDNS publisher
		if err := mdns.Run(os.Args[2:]); err != nil {
			printer.Fprintf(os.Stderr, "mdns-publish failed: %v\n", err)
			os.Exit(1)
		}

	case "dhcp-request":
		// Integration test helper: DHCP requester
		if err := dhcp.Run(os.Args[2:]); err != nil {
			printer.Fprintf(os.Stderr, "dhcp-request failed: %v\n", err)
			os.Exit(1)
		}

	case "restart":
		// Restart daemon (alias for upgrade --self)
		exe, err := os.Executable()
		if err != nil {
			printer.Fprintf(os.Stderr, "Error determining current binary path: %v\n", err)
			os.Exit(1)
		}
		configFile := brand.DefaultConfigDir + "/" + brand.ConfigFileName
		cmd.RunUpgrade(exe, configFile)

	case "version":
		// Print version info
		printer.Printf("%s version %s\n", brand.Name, brand.Version)
		printer.Printf("Build: %s\n", brand.BuildTime)

	case "ipset":
		// IPSet management commands
		cmd.RunIPSet(os.Args[2:])

	case "upgrade":
		// Seamless upgrade with socket handoff (local or remote)
		upgradeFlags := flag.NewFlagSet("upgrade", flag.ExitOnError)
		newBinary := upgradeFlags.String("binary", "", "Path to binary (required for remote upgrade)")
		upgradeFlags.StringVar(newBinary, "b", "", "Path to binary (short)")
		
		selfUpgrade := upgradeFlags.Bool("self", false, "Restart using the current binary")
		
		configFile := upgradeFlags.String("config", brand.DefaultConfigDir+"/"+brand.ConfigFileName, "Configuration file")
		upgradeFlags.StringVar(configFile, "c", brand.DefaultConfigDir+"/"+brand.ConfigFileName, "Configuration file (short)")
		
		remoteURL := upgradeFlags.String("remote", "", "Remote URL to upgrade (e.g., https://192.168.1.1:8443)")
		upgradeFlags.StringVar(remoteURL, "r", "", "Remote URL (short)")
		
		apiKey := upgradeFlags.String("api-key", "", "API key for remote authentication")
		upgradeFlags.StringVar(apiKey, "k", "", "API key (short)")
		
		upgradeFlags.Parse(os.Args[2:])

		// Remote upgrade mode
		if *remoteURL != "" {
			if *newBinary == "" {
				printer.Fprintf(os.Stderr, "Error: --binary is required for remote upgrade\n")
				printer.Println("Usage: " + brand.BinaryName + " upgrade --remote <url> --binary <path> --api-key <key>")
				os.Exit(1)
			}
			if *apiKey == "" {
				printer.Fprintf(os.Stderr, "Error: --api-key is required for remote upgrade\n")
				os.Exit(1)
			}

			// Create client
			apiClient := client.NewHTTPClient(*remoteURL, client.WithAPIKey(*apiKey))

			// Auto-detect remote architecture from status
			printer.Println("Checking remote system...")
			status, err := apiClient.GetStatus()
			if err != nil {
				printer.Fprintf(os.Stderr, "Failed to get remote status: %v\n", err)
				os.Exit(1)
			}

			remoteArch := status.BuildArch
			if remoteArch == "" {
				printer.Fprintf(os.Stderr, "Error: remote did not report architecture\n")
				os.Exit(1)
			}

			printer.Printf("Remote: %s (version %s, arch %s)\n", *remoteURL, status.Version, remoteArch)

			// Upload binary
			result, err := apiClient.UploadBinary(*newBinary, remoteArch)
			if err != nil {
				printer.Fprintf(os.Stderr, "Remote upgrade failed: %v\n", err)
				os.Exit(1)
			}

			printer.Printf("Remote upgrade initiated successfully!\n")
			printer.Printf("  Bytes uploaded: %d\n", result.Bytes)
			printer.Printf("  Checksum: %s\n", result.Checksum)
			printer.Printf("  Message: %s\n", result.Message)
			os.Exit(0)
		}

		// Local upgrade mode
		if *selfUpgrade {
			exe, err := os.Executable()
			if err != nil {
				printer.Fprintf(os.Stderr, "Error determining current binary path: %v\n", err)
				os.Exit(1)
			}
			*newBinary = exe
		}

		// Auto-detect staged binary if --binary not specified
		if *newBinary == "" {
			stagedPath := "/usr/sbin/" + brand.BinaryName + "_new"
			if info, err := os.Stat(stagedPath); err == nil && !info.IsDir() {
				printer.Printf("Detected staged binary: %s\n", stagedPath)
				*newBinary = stagedPath
			}
		}

		if *newBinary == "" {
			printer.Println("Usage: " + brand.BinaryName + " upgrade --binary <path-to-new-binary> OR --self")
			printer.Println("       Or place new binary at /usr/sbin/" + brand.BinaryName + "_new and run without arguments")
			printer.Println("\nRemote: " + brand.BinaryName + " upgrade --remote <url> --binary <path> --api-key <key>")
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
			printer.Fprintf(os.Stderr, "Reload failed: %v\n", err)
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
				printer.Printf("No detailed help available for '%s'\n", os.Args[2])
				printUsage()
			}
		} else {
			printUsage()
		}

	default:
		printer.Printf("Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	printer.Printf(`%s - %s

Usage:
  %s <command> [options]

Core Commands:
  start     Start the firewall daemon
            Options: --foreground (-f), --dry-run (-n), --config (-c) <file>
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
            Options: --verbose (-v)
  show      Display firewall rules
            Options: --summary (-s), --remote (-r) <url>, --api-key (-k) <key>
  log       View and stream system logs
            Options: -f (follow), -n (lines), --remote <url>
  diff      Compare two configuration files
  import    Import configuration from other firewalls
  console   Interactive TUI dashboard
            Options: --remote <url>, --api-key <key>, --insecure, --debug
  setup     First-run setup wizard
            Options: --config-dir (-d)
  reset     Factory reset (--confirm/-y required)
            Options: --config-dir (-d)
  upgrade   Seamless upgrade with socket handoff
            Options: --binary (-b) <path>, --self, --remote (-r), --api-key (-k)

Examples:
  %s start                          # Start in background
  %s start --foreground             # Start in foreground (debug)
  %s start --dry-run                # Dry run (show what would happen)
  %s stop                           # Stop the daemon
  %s api generate --name "monitor" --preset readonly
  %s check -v /etc/glacic/glacic.hcl
  %s show --remote https://192.168.1.1:8443 --api-key xxx
  %s log -f

For command-specific help: %s help <command>
`,
		brand.Name, brand.Description,
		brand.LowerName,
		brand.LowerName, brand.LowerName, brand.LowerName, brand.LowerName,
		brand.LowerName, brand.LowerName, brand.LowerName, brand.LowerName,
		brand.LowerName)
}
