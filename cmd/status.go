package cmd

import (
	"fmt"
	"os"
	"strings"

	"grimm.is/glacic/internal/ctlplane"
)

// RunStatus queries the daemon for current status and prints it
func RunStatus() {
	client, err := ctlplane.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to control plane: %v\n", err)
		fmt.Fprintln(os.Stderr, "Is the daemon running? Start with: glacic ctl <config>")
		os.Exit(1)
	}
	defer client.Close()

	// Get status
	status, err := client.GetStatus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get status: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("=== Glacic Firewall Status ===")
	fmt.Println()

	if status.Running {
		fmt.Println("Status:  RUNNING")
	} else {
		fmt.Println("Status:  STOPPED")
	}
	fmt.Printf("Uptime:  %s\n", status.Uptime)
	fmt.Printf("Config:  %s\n", status.ConfigFile)
	fmt.Println()

	// Get services
	services, err := client.GetServices()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to get services: %v\n", err)
	} else {
		fmt.Println("Services:")
		for _, svc := range services {
			state := "stopped"
			if svc.Running {
				state = "running"
			}
			fmt.Printf("  %-12s %s\n", svc.Name+":", state)
		}
		fmt.Println()
	}

	// Get interfaces
	ifaces, err := client.GetInterfaces()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to get interfaces: %v\n", err)
	} else {
		fmt.Println("Interfaces:")
		for _, iface := range ifaces {
			ips := strings.Join(iface.IPv4Addrs, ", ")
			if ips == "" {
				ips = "(no IP)"
			}
			fmt.Printf("  %-10s %-6s %-8s %s\n", iface.Name, iface.Zone, iface.State, ips)
		}
	}
}
