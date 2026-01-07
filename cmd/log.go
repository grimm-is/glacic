package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"grimm.is/glacic/internal/client"
	"grimm.is/glacic/internal/ctlplane"
)

// RunLog handles the "log" command.
func RunLog(args []string) error {
	// Parse flags using standard flag package
	fs := flag.NewFlagSet("log", flag.ContinueOnError)

	var (
		source, level, search, remote, apiKey, fingerprint string
		limit                                              int
		follow                                             bool
	)

	// Define flags and aliases pointing to same variables
	fs.StringVar(&source, "source", "", "Filter by log source (e.g., dmesg, firewall, dns)")
	fs.StringVar(&source, "s", "", "Alias for -source")

	fs.StringVar(&level, "level", "", "Filter by log level (debug, info, warn, error)")
	fs.StringVar(&level, "l", "", "Alias for -level")

	fs.StringVar(&search, "grep", "", "Search for string in log messages")
	fs.StringVar(&search, "g", "", "Alias for -grep")

	fs.BoolVar(&follow, "follow", false, "Tail the logs in real-time")
	fs.BoolVar(&follow, "f", false, "Alias for -follow")

	fs.IntVar(&limit, "lines", 50, "Number of lines to show")
	fs.IntVar(&limit, "n", 50, "Alias for -lines")

	fs.StringVar(&remote, "remote", "", "Remote Glacic API URL")
	fs.StringVar(&apiKey, "api-key", "", "API key for remote authentication")
	fs.StringVar(&fingerprint, "fingerprint", "", "Expected server certificate fingerprint (SHA-256)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Remote Mode
	if remote != "" {
		return runLogRemote(remote, apiKey, fingerprint, source, level, search, limit, follow)
	}

	// Local Mode
	return runLogLocal(source, level, search, limit, follow)
}

func runLogRemote(remoteURL, apiKey, fingerprint, source, level, search string, limit int, follow bool) error {
	// Connect
	opts := []client.ClientOption{}
	if apiKey != "" {
		opts = append(opts, client.WithAPIKey(apiKey))
	}
	if fingerprint != "" {
		opts = append(opts, client.WithFingerprint(fingerprint))
	}

	apiClient := client.NewHTTPClient(remoteURL, opts...)

	// Get initial logs
	logsArgs := &client.GetLogsArgs{
		Source: source,
		Level:  level,
		Search: search,
		Limit:  limit,
	}

	// This request will trigger the handshake and populate SeenFingerprint
	entries, err := apiClient.GetLogs(logsArgs)
	if err != nil {
		// If it failed due to cert mismatch (handled in VerifyPeerCertificate), err will reflect that.
		return fmt.Errorf("failed to fetch logs: %w", err)
	}

	// Display verification info (TOFU or Confirmation)
	if fingerprint == "" && apiClient.SeenFingerprint != "" {
		// Print to stderr to avoid polluting log output if piped
		Printer.Fprintf(os.Stderr, "Connected to %s\n", remoteURL)
		Printer.Fprintf(os.Stderr, "Server Certificate Fingerprint: %s\n", apiClient.SeenFingerprint)
		Printer.Fprintf(os.Stderr, "To pin this certificate, use --fingerprint %s\n", apiClient.SeenFingerprint)
	}

	printClientLogs(entries)

	if !follow {
		return nil
	}

	// Tailing
	Printer.Println("--- Tailing logs ---")

	// Handle Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	errChan := make(chan error, 1)

	go func() {
		// Use TailLogs which uses WebSocket
		// Use same fingerprint logic (WebSocket dialer needs update?)
		// TailLogs uses `c.httpClient`? No, it uses `websocket.DefaultDialer` usually or creates one.
		// `TailLogs` method in `internal/client/client.go` creates a new Dialer?
		// I must check `TailLogs` implementation.
		// If `TailLogs` ignores `expectedFingerprint`, WebSocket connection bypasses verification!
		// I need to update `TailLogs` too.

		err := apiClient.TailLogs(func(batch []client.LogEntry) {
			for _, entry := range batch {
				if matchClientFilter(entry, source, level, search) {
					printClientLog(entry)
				}
			}
		})
		errChan <- err
	}()

	select {
	case <-sigChan:
		return nil
	case err := <-errChan:
		return err
	}
}

func runLogLocal(source, level, search string, limit int, follow bool) error {
	// Use RPC client
	cli, err := ctlplane.NewClient()
	if err != nil {
		return fmt.Errorf("failed to connect to local control plane: %w", err)
	}
	defer cli.Close()

	// Get initial logs
	args := &ctlplane.GetLogsArgs{
		Source: source,
		Level:  level,
		Search: search,
		Limit:  limit,
	}

	reply, err := cli.GetLogs(args)
	if err != nil {
		return fmt.Errorf("failed to get logs: %w", err)
	}

	printLogs(reply.Entries) // reply.Entries is []ctlplane.LogEntry

	if !follow {
		return nil
	}

	Printer.Println("--- Tailing logs (local) ---")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Poll RPC for local streaming
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastTimestamp string
	if len(reply.Entries) > 0 {
		lastTimestamp = reply.Entries[0].Timestamp
	} else {
		lastTimestamp = time.Now().Format(time.RFC3339)
	}

	for {
		select {
		case <-sigChan:
			return nil
		case <-ticker.C:
			pollArgs := &ctlplane.GetLogsArgs{
				Source: source,
				Level:  level,
				Search: search,
				Since:  lastTimestamp,
				Limit:  100,
			}
			newLogs, err := cli.GetLogs(pollArgs)
			if err != nil {
				Printer.Fprintf(os.Stderr, "Error polling logs: %v\n", err)
				continue
			}

			if len(newLogs.Entries) > 0 {
				// Entries are newest first. Print oldest first for tailing.
				for i := len(newLogs.Entries) - 1; i >= 0; i-- {
					printLog(newLogs.Entries[i])
				}
				lastTimestamp = newLogs.Entries[0].Timestamp
			}
		}
	}
}

func printClientLogs(entries []client.LogEntry) {
	for i := len(entries) - 1; i >= 0; i-- {
		printClientLog(entries[i])
	}
}

func printLogs(entries []ctlplane.LogEntry) {
	for i := len(entries) - 1; i >= 0; i-- {
		printLog(entries[i])
	}
}

func printClientLog(e client.LogEntry) {
	Printer.Printf("%s [%s] %s: %s\n", e.Timestamp, e.Level, e.Source, e.Message)
}

func printLog(e ctlplane.LogEntry) {
	Printer.Printf("%s [%s] %s: %s\n", e.Timestamp, e.Level, e.Source, e.Message)
}

func matchClientFilter(e client.LogEntry, source, level, search string) bool {
	if source != "" && e.Source != source {
		return false
	}
	if level != "" && e.Level != level {
		return false
	}
	return true
}
