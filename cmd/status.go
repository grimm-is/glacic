package cmd

import (
	"os"
	"strings"

	"grimm.is/glacic/internal/ctlplane"
)

// RunStatus queries the daemon for current status and prints it
func RunStatus() {
	client, err := ctlplane.NewClient()
	if err != nil {
		Printer.Fprintf(os.Stderr, "Failed to connect to control plane: %v\n", err)
		Printer.Fprintf(os.Stderr, "Is the daemon running? Start with: glacic ctl <config>\n")
		os.Exit(1)
	}
	defer client.Close()

	// Get status
	status, err := client.GetStatus()
	if err != nil {
		Printer.Fprintf(os.Stderr, "Failed to get status: %v\n", err)
		os.Exit(1)
	}

	Printer.Println("=== Glacic Firewall Status ===")
	Printer.Println()

	if status.Running {
		Printer.Println("Status:  RUNNING")
	} else {
		Printer.Println("Status:  STOPPED")
	}
	Printer.Printf("Uptime:  %s\n", status.Uptime)
	Printer.Printf("Config:  %s\n", status.ConfigFile)
	Printer.Println()

	// Get services
	services, err := client.GetServices()
	if err != nil {
		Printer.Fprintf(os.Stderr, "Warning: Failed to get services: %v\n", err)
	} else {
		Printer.Println("Services:")
		for _, svc := range services {
			state := "stopped"
			if svc.Running {
				state = "running"
			}
			Printer.Printf("  %-12s %s\n", svc.Name+":", state)
		}
		Printer.Println()
	}

	// Get interfaces
	ifaces, err := client.GetInterfaces()
	if err != nil {
		Printer.Fprintf(os.Stderr, "Warning: Failed to get interfaces: %v\n", err)
	} else {
		Printer.Println("Interfaces:")
		for _, iface := range ifaces {
			ips := strings.Join(iface.IPv4Addrs, ", ")
			if ips == "" {
				ips = "(no IP)"
			}
			Printer.Printf("  %-10s %-6s %-8s %s\n", iface.Name, iface.Zone, iface.State, ips)
		}
	}
}
