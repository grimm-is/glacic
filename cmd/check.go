package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/firewall"
	"grimm.is/glacic/internal/network"
)

// RunCheck validates the configuration file syntax and semantics.
func RunCheck(configFile string, verbose bool) error {
	// Parse with default options
	if len(configFile) == 0 {
		return fmt.Errorf("usage: %s check [-v] <config-file>\nExample: %s check -v /etc/glacic/glacic.hcl", brand.BinaryName, brand.BinaryName)
	}

	result, err := config.LoadFileWithOptions(configFile, config.DefaultLoadOptions())
	if err != nil {
		return fmt.Errorf("configuration invalid: %w", err)
	}

	cfg := result.Config
	Printer.Printf("Configuration valid!\n")
	Printer.Printf("Schema Version: %s\n", cfg.SchemaVersion)
	Printer.Printf("Interfaces: %d\n", len(cfg.Interfaces))
	Printer.Printf("Zones: %d\n", len(cfg.Zones))
	Printer.Printf("Policies: %d\n", len(cfg.Policies))

	if result.WasMigrated {
		Printer.Printf("Migration: %s -> %s\n", result.OriginalVersion, result.CurrentVersion)
	}

	if verbose {
		Printer.Println()
		printSummary(cfg)

		Printer.Println("\n[DRY RUN] Generated Operations:")

		// 1. Network Operations
		dryExecutor := network.NewDryRunExecutor()
		drySys := &network.DryRunSystemController{}
		dryNL := &network.DryRunNetlinker{} // Ensure this is pointer if methods are pointer receivers

		nm := network.NewManagerWithDeps(dryNL, drySys, dryExecutor)

		Printer.Println("\n--- Network Configuration ---")

		// Simulate network setup
		if cfg.IPForwarding {
			nm.SetIPForwarding(true)
		}

		for _, iface := range cfg.Interfaces {
			nm.ApplyInterface(iface)
		}

		// Print collected logs
		for _, w := range drySys.Writes {
			Printer.Println(w)
		}
		for _, op := range dryNL.Ops {
			Printer.Println(op)
		}
		for _, cmd := range dryExecutor.Commands {
			Printer.Println(cmd)
		}

		// 2. Firewall Rules
		Printer.Println("\n--- Firewall Rules (nftables) ---")
		// Use a temporary cache dir for dry run
		fm, err := firewall.NewManager(nil, "/tmp/glacic-dryrun")
		if err == nil {
			script, err := fm.GenerateRules(firewall.FromGlobalConfig(cfg))
			if err != nil {
				Printer.Printf("Error generating firewall rules: %v\n", err)
			} else {
				Printer.Println(script)
			}
		} else {
			Printer.Printf("Error initializing firewall manager: %v\n", err)
		}
	}

	return nil
}

func printSummary(cfg *config.Config) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)

	// Interfaces
	Printer.Fprintln(w, "INTERFACE\tVLANS\tDHCP\tIPV4\tIPV6\tZONE")
	for _, iface := range cfg.Interfaces {
		ipv4 := "-"
		if len(iface.IPv4) > 0 {
			ipv4 = fmt.Sprintf("%v", iface.IPv4)
		}
		ipv6 := "-"
		if len(iface.IPv6) > 0 {
			ipv6 = fmt.Sprintf("%v", iface.IPv6)
		}
		dhcp := "no"
		if iface.DHCP {
			dhcp = "yes"
		}
		zone := "-"
		if iface.Zone != "" {
			zone = iface.Zone
		} else {
			// Reverse lookup zone from zone definitions
			for _, z := range cfg.Zones {
				for _, zi := range z.Interfaces {
					if zi == iface.Name {
						zone = z.Name
						break
					}
				}
			}
		}

		Printer.Fprintf(w, "%s\t%d\t%s\t%s\t%s\t%s\n", iface.Name, len(iface.VLANs), dhcp, ipv4, ipv6, zone)
	}
	Printer.Fprintln(w)
	w.Flush()

	// Zones
	Printer.Fprintln(w, "ZONE\tINTERFACES")
	for _, z := range cfg.Zones {
		ifaces := fmt.Sprintf("%v", z.Interfaces)
		Printer.Fprintf(w, "%s\t%s\n", z.Name, ifaces)
	}
	Printer.Fprintln(w)
	w.Flush()

	// Policies
	Printer.Fprintln(w, "FROM\tTO\tRULES")
	for _, p := range cfg.Policies {
		Printer.Fprintf(w, "%s\t%s\t%d\n", p.From, p.To, len(p.Rules))
	}
	w.Flush()
}
