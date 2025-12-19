package cmd

import (
	"fmt"
	"os"

	"grimm.is/glacic/internal/ctlplane"
	"grimm.is/glacic/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

// RunConsole starts the TUI console
func RunConsole() {
	// Connect to control plane
	client, err := ctlplane.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to control plane: %v\n", err)
		fmt.Fprintf(os.Stderr, "Make sure 'firewall ctl' is running first.\n")
		os.Exit(1)
	}
	defer client.Close()

	// Start Bubble Tea app
	p := tea.NewProgram(tui.NewModel(client), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running console: %v\n", err)
		os.Exit(1)
	}
}
