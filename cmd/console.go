package cmd

import (
	"os"

	"grimm.is/glacic/internal/ctlplane"
	"grimm.is/glacic/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

// RunConsole starts the TUI console
func RunConsole(remote string, apiKey string, insecure bool, debug bool) {
	if debug {
		if err := tui.EnableDebugLogging("tui.log"); err != nil {
			Printer.Fprintf(os.Stderr, "Failed to enable debug logging: %v\n", err)
		} else {
			defer tui.CloseDebugLog()
			tui.DebugLog("Starting TUI Console")
		}
	}

	var backend tui.Backend

	if remote != "" {
		if apiKey == "" {
			Printer.Fprintf(os.Stderr, "Error: --api-key is required for remote connection\n")
			os.Exit(1)
		}
		backend = tui.NewRemoteBackend(remote, apiKey, insecure)
	} else {
		// Local mode - connect to Unix socket - deferred to nil check below
	}

	// Start Bubble Tea app
	if backend == nil {
		client, err := ctlplane.NewClient()
		if err != nil {
			Printer.Fprintf(os.Stderr, "Failed to connect to control plane: %v\n", err)
			Printer.Fprintf(os.Stderr, "Make sure 'firewall ctl' is running first.\n")
			os.Exit(1)
		}
		defer client.Close()
		backend = tui.NewLocalBackend(client)
	}

	p := tea.NewProgram(tui.NewModel(backend), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		Printer.Fprintf(os.Stderr, "Error running console: %v\n", err)
		os.Exit(1)
	}
}
