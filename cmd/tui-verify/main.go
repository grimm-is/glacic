package main

import (
	"fmt"
	"os"

	"grimm.is/glacic/internal/tui"
)

// Test Config Struct
type FirewallRule struct {
	Name      string `tui:"title=Rule Name,desc=Unique identifier,validate=required"`
	Protocol  string `tui:"title=Protocol,options=TCP:tcp,UDP:udp,ICMP:icmp"`
	Source    string `tui:"title=Source CIDR,validate=cidr"`
	Logging   bool   `tui:"title=Enable Logging,desc=Log to nflog group 100"`
	Reflected bool   `tui:"title=NAT Reflection,desc=Enable Hairpin NAT"`
}

func main() {
	// 1. Create default data
	rule := &FirewallRule{
		Protocol: "tcp",
		Logging:  true,
		Source:   "10.0.0.1/32",
	}

	// 2. Generate Form
	form := tui.AutoForm(rule)

	// 3. Run interacting
	Printer.Println("Launch AutoForm verification...")
	err := form.Run()
	if err != nil {
		Printer.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// 4. Print Result
	Printer.Printf("\n--- Result ---\n")
	Printer.Printf("Name: %s\n", rule.Name)
	Printer.Printf("Proto: %s\n", rule.Protocol)
	Printer.Printf("Source: %s\n", rule.Source)
	Printer.Printf("Logging: %v\n", rule.Logging)
	Printer.Printf("Reflected: %v\n", rule.Reflected)
}
