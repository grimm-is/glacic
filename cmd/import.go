package cmd

import (
	"flag"
	"os"

	imports "grimm.is/glacic/internal/import"
)

// RunImport runs the import wizard (consolidated)
func RunImport(args []string) {
	var inputFile string
	var outputConfig string
	var importType string

	// Parse flags from args, not os.Args
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	fs.StringVar(&inputFile, "input", "", "Path to backup file")
	fs.StringVar(&outputConfig, "output", "config.hcl", "Output configuration file")
	fs.StringVar(&importType, "type", "pfsense", "Backup type (pfsense, opnsense)")
	fs.Parse(args)

	if inputFile == "" {
		Printer.Fprintf(os.Stderr, "Error: --input is required\n")
		fs.Usage()
		os.Exit(1)
	}

	if importType != "pfsense" && importType != "opnsense" {
		Printer.Fprintf(os.Stderr, "Error: unsupported type '%s'\n", importType)
		os.Exit(1)
	}

	Printer.Printf("Parsed backup file: %s (Type: %s)\n", inputFile, importType)

	result, err := imports.ParsePfSenseBackup(inputFile)
	if err != nil {
		Printer.Fprintf(os.Stderr, "Error parsing backup: %v\n", err)
		os.Exit(1)
	}

	Printer.Printf("Found: %d interfaces, %d filter rules, %d NAT rules\n",
		len(result.Interfaces), len(result.FilterRules), len(result.NATRules))

	// Generate HCL
	hcl := result.GenerateHCLConfig()

	if err := os.WriteFile(outputConfig, []byte(hcl), 0644); err != nil {
		Printer.Fprintf(os.Stderr, "Error writing config: %v\n", err)
		os.Exit(1)
	}

	Printer.Printf("Configuration written to %s\n", outputConfig)
	Printer.Println()
	Printer.Println("WARNING: This config requires manual review! Interfaces must be mapped to Linux names.")

	if len(result.ManualSteps) > 0 {
		Printer.Println("\nManual Steps Required:")
		for _, step := range result.ManualSteps {
			Printer.Printf("- %s\n", step)
		}
	}
}
