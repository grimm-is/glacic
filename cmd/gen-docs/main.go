package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
	"grimm.is/glacic/internal/api/spec"
)

func main() {
	doc, err := spec.GenerateSpec()
	if err != nil {
		Printer.Printf("Error generating spec: %v\n", err)
		os.Exit(1)
	}

	// Output to internal/api/spec/openapi.yaml
	// We assume we are running from project root or cmd/gen-docs
	// Best effort to find the path
	path := "internal/api/spec/openapi.yaml"
	if _, err := os.Stat("internal/api/spec"); os.IsNotExist(err) {
		// Try relative to cmd/gen-docs if running from there?
		// Actually, let's just assume project root for now as per makefile convention
		if err := os.MkdirAll("internal/api/spec", 0755); err != nil {
			Printer.Printf("Failed to create dir: %v\n", err)
			os.Exit(1)
		}
	}

	// Convert to JSON first (intermediate) or direct YAML
	// OpenAPI is usually YAML.
	f, err := os.Create(path)
	if err != nil {
		Printer.Printf("Failed to create file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	enc := yaml.NewEncoder(f)
	if err := enc.Encode(doc); err != nil {
		Printer.Printf("Failed to encode YAML: %v\n", err)
		os.Exit(1)
	}

	Printer.Printf("Successfully generated %s\n", path)
}
