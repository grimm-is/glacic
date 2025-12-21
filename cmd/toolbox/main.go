package main

import (
	"fmt"
	"os"
	"path/filepath"

	"grimm.is/glacic/internal/toolbox/agent"
	"grimm.is/glacic/internal/toolbox/harness"
	"grimm.is/glacic/internal/toolbox/mcast"
	"grimm.is/glacic/internal/toolbox/orca"
)

func main() {
	// Busybox-style dispatch based on argv[0]
	cmd := filepath.Base(os.Args[0])

	switch cmd {
	case "agent", "glacic", "glacic-agent":
		if err := agent.Run(os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "agent error: %v\n", err)
			os.Exit(1)
		}
	case "orca", "orchestrator", "glacic-orchestrator", "orch", "ctl", "fleet":
		if err := orca.Run(os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "orca error: %v\n", err)
			os.Exit(1)
		}
	case "prove":
		if err := harness.Run(os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "prove error: %v\n", err)
			os.Exit(1)
		}
	case "toolbox", "toolbox-linux":
		// Fallback for direct invocation: "toolbox agent", "toolbox prove", etc.
		if len(os.Args) < 2 {
			help()
			os.Exit(1)
		}
		sub := os.Args[1]
		args := os.Args[2:]

		switch sub {
		case "agent":
			if err := agent.Run(args); err != nil {
				fmt.Fprintf(os.Stderr, "agent error: %v\n", err)
				os.Exit(1)
			}
		case "orca", "orchestrator", "orch", "ctl", "fleet":
			if err := orca.Run(args); err != nil {
				fmt.Fprintf(os.Stderr, "orca error: %v\n", err)
				os.Exit(1)
			}
		case "prove":
			if err := harness.Run(args); err != nil {
				fmt.Fprintf(os.Stderr, "prove error: %v\n", err)
				os.Exit(1)
			}
		case "mcast":
			if err := mcast.Run(args); err != nil {
				fmt.Fprintf(os.Stderr, "mcast error: %v\n", err)
				os.Exit(1)
			}
		default:
			help()
			os.Exit(1)
		}
	default:
		help()
		os.Exit(1)
	}
}

func help() {
	fmt.Println("Glacic Toolbox - Busybox style test utils")
	fmt.Println("Usage: invoke as 'agent', 'orca', 'prove', or 'toolbox <subcmd>'")
}
