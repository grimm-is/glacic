package cmd

import (
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// IPSetResponse represents the API response for IPSet operations
type IPSetResponse struct {
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	Source       string    `json:"source"`
	SourceURL    string    `json:"source_url,omitempty"`
	LastUpdate   time.Time `json:"last_update"`
	NextUpdate   time.Time `json:"next_update,omitempty"`
	EntriesCount int       `json:"entries_count"`
	Age          string    `json:"age"`
}

// IPSetListResponse represents the API response for listing IPSets
type IPSetListResponse struct {
	IPSets []IPSetResponse `json:"ipsets"`
	Count  int             `json:"count"`
}

// CacheInfoResponse represents cache information
type CacheInfoResponse struct {
	CachedLists int    `json:"cached_lists"`
	TotalSize   int64  `json:"total_size"`
	CacheDir    string `json:"cache_dir"`
}

// RunIPSet handles IPSet CLI commands
func RunIPSet(args []string) {
	if len(args) < 1 {
		printIPSetUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "list":
		runIPSetList(args[1:])
	case "show":
		runIPSetShow(args[1:])
	case "update":
		runIPSetUpdate(args[1:])
	case "refresh":
		runIPSetRefresh(args[1:])
	case "cache":
		runIPSetCache(args[1:])
	default:
		Printer.Printf("Unknown ipset command: %s\n\n", args[0])
		printIPSetUsage()
		os.Exit(1)
	}
}

func runIPSetList(args []string) {
	flags := flag.NewFlagSet("ipset list", flag.ExitOnError)
	apiAddr := flags.String("api", "http://localhost:8080", "API server address")
	flags.Parse(args)

	resp, err := http.Get(*apiAddr + "/api/ipsets")
	if err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to connect to API server: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		Printer.Fprintf(os.Stderr, "Error: API server returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to read response: %v\n", err)
		os.Exit(1)
	}

	var listResp IPSetListResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	if listResp.Count == 0 {
		Printer.Println("No IPSets configured")
		return
	}

	Printer.Printf("IPSets (%d total):\n\n", listResp.Count)
	Printer.Printf("%-20s %-12s %-12s %-10s %-15s %s\n", "NAME", "TYPE", "SOURCE", "ENTRIES", "AGE", "NEXT UPDATE")
	Printer.Println(strings.Repeat("-", 90))

	for _, ipset := range listResp.IPSets {
		nextUpdate := "Never"
		if !ipset.NextUpdate.IsZero() {
			nextUpdate = ipset.NextUpdate.Format("2006-01-02 15:04")
		}
		Printer.Printf("%-20s %-12s %-12s %-10d %-15s %s\n",
			ipset.Name, ipset.Type, ipset.Source, ipset.EntriesCount, ipset.Age, nextUpdate)
	}
}

func runIPSetShow(args []string) {
	flags := flag.NewFlagSet("ipset show", flag.ExitOnError)
	apiAddr := flags.String("api", "http://localhost:8080", "API server address")
	flags.Parse(args)

	if len(flags.Args()) < 1 {
		Printer.Fprintf(os.Stderr, "Error: IPSet name is required\n")
		Printer.Fprintf(os.Stderr, "Usage: firewall ipset show <name>\n")
		os.Exit(1)
	}

	name := flags.Arg(0)
	resp, err := http.Get(*apiAddr + "/api/ipsets/" + name)
	if err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to connect to API server: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		Printer.Fprintf(os.Stderr, "Error: IPSet '%s' not found\n", name)
		os.Exit(1)
	}

	if resp.StatusCode != http.StatusOK {
		Printer.Fprintf(os.Stderr, "Error: API server returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to read response: %v\n", err)
		os.Exit(1)
	}

	var ipset IPSetResponse
	if err := json.Unmarshal(body, &ipset); err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	Printer.Printf("IPSet Details:\n\n")
	Printer.Printf("Name:         %s\n", ipset.Name)
	Printer.Printf("Type:         %s\n", ipset.Type)
	Printer.Printf("Source:       %s\n", ipset.Source)
	if ipset.SourceURL != "" {
		Printer.Printf("Source URL:   %s\n", ipset.SourceURL)
	}
	Printer.Printf("Entries:      %d\n", ipset.EntriesCount)
	Printer.Printf("Last Update:  %s\n", ipset.LastUpdate.Format("2006-01-02 15:04:05"))
	Printer.Printf("Age:          %s\n", ipset.Age)
	if !ipset.NextUpdate.IsZero() {
		Printer.Printf("Next Update:  %s\n", ipset.NextUpdate.Format("2006-01-02 15:04:05"))
	}
}

func runIPSetUpdate(args []string) {
	flags := flag.NewFlagSet("ipset update", flag.ExitOnError)
	apiAddr := flags.String("api", "http://localhost:8080", "API server address")
	flags.Parse(args)

	if len(flags.Args()) < 1 {
		Printer.Fprintf(os.Stderr, "Error: IPSet name is required\n")
		Printer.Fprintf(os.Stderr, "Usage: firewall ipset update <name>\n")
		os.Exit(1)
	}

	name := flags.Arg(0)
	resp, err := http.Post(*apiAddr+"/api/ipsets/"+name+"/update", "application/json", nil)
	if err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to connect to API server: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		Printer.Fprintf(os.Stderr, "Error: IPSet '%s' not found\n", name)
		os.Exit(1)
	}

	if resp.StatusCode != http.StatusOK {
		Printer.Fprintf(os.Stderr, "Error: API server returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to read response: %v\n", err)
		os.Exit(1)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	if message, ok := result["message"].(string); ok {
		Printer.Println(message)
	}
}

func runIPSetRefresh(args []string) {
	flags := flag.NewFlagSet("ipset refresh", flag.ExitOnError)
	apiAddr := flags.String("api", "http://localhost:8080", "API server address")
	flags.Parse(args)

	if len(flags.Args()) < 1 {
		Printer.Fprintf(os.Stderr, "Error: IPSet name is required\n")
		Printer.Fprintf(os.Stderr, "Usage: firewall ipset refresh <name>\n")
		os.Exit(1)
	}

	name := flags.Arg(0)
	resp, err := http.Post(*apiAddr+"/api/ipsets/"+name+"/refresh", "application/json", nil)
	if err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to connect to API server: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		Printer.Fprintf(os.Stderr, "Error: IPSet '%s' not found\n", name)
		os.Exit(1)
	}

	if resp.StatusCode != http.StatusOK {
		Printer.Fprintf(os.Stderr, "Error: API server returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to read response: %v\n", err)
		os.Exit(1)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	if message, ok := result["message"].(string); ok {
		Printer.Println(message)
	}
}

func runIPSetCache(args []string) {
	if len(args) < 1 {
		Printer.Fprintf(os.Stderr, "Error: Cache command is required\n")
		Printer.Fprintf(os.Stderr, "Usage: firewall ipset cache <info|clear>\n")
		os.Exit(1)
	}

	switch args[0] {
	case "info":
		runIPSetCacheInfo(args[1:])
	case "clear":
		runIPSetCacheClear(args[1:])
	default:
		Printer.Fprintf(os.Stderr, "Unknown cache command: %s\n", args[0])
		Printer.Fprintf(os.Stderr, "Usage: firewall ipset cache <info|clear>\n")
		os.Exit(1)
	}
}

func runIPSetCacheInfo(args []string) {
	flags := flag.NewFlagSet("ipset cache info", flag.ExitOnError)
	apiAddr := flags.String("api", "http://localhost:8080", "API server address")
	flags.Parse(args)

	resp, err := http.Get(*apiAddr + "/api/ipsets/cache/info")
	if err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to connect to API server: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		Printer.Fprintf(os.Stderr, "Error: API server returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to read response: %v\n", err)
		os.Exit(1)
	}

	var cacheInfo CacheInfoResponse
	if err := json.Unmarshal(body, &cacheInfo); err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	Printer.Printf("IPSet Cache Information:\n\n")
	Printer.Printf("Cached Lists: %d\n", cacheInfo.CachedLists)
	Printer.Printf("Total Size:   %d bytes\n", cacheInfo.TotalSize)
	Printer.Printf("Cache Dir:    %s\n", cacheInfo.CacheDir)
}

func runIPSetCacheClear(args []string) {
	flags := flag.NewFlagSet("ipset cache clear", flag.ExitOnError)
	apiAddr := flags.String("api", "http://localhost:8080", "API server address")
	flags.Parse(args)

	req, err := http.NewRequest("DELETE", *apiAddr+"/api/ipsets/cache", nil)
	if err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to create request: %v\n", err)
		os.Exit(1)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to connect to API server: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		Printer.Fprintf(os.Stderr, "Error: API server returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to read response: %v\n", err)
		os.Exit(1)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		Printer.Fprintf(os.Stderr, "Error: Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	if message, ok := result["message"].(string); ok {
		Printer.Println(message)
	}
}

func printIPSetUsage() {
	Printer.Println(`IPSet Management Commands

Usage:
  firewall ipset list [--api :8080]                    List all IPSets
  firewall ipset show <name> [--api :8080]             Show IPSet details
  firewall ipset update <name> [--api :8080]          Force update an IPSet
  firewall ipset refresh <name> [--api :8080]         Refresh an IPSet
  firewall ipset cache info [--api :8080]              Show cache information
  firewall ipset cache clear [--api :8080]             Clear all cached data

Commands:
  list    List all configured IPSets with summary information
  show    Show detailed information for a specific IPSet
  update  Force an immediate update of an IPSet from its source
  refresh Refresh an IPSet (same as update, for future differentiation)
  cache   Manage cached FireHOL list data
    info  Show cache statistics and directory
    clear Clear all cached data

Flags:
  -api    API server address (default: http://localhost:8080)

Examples:
  firewall ipset list
  firewall ipset show firehol_level1
  firewall ipset update firehol_level1
  firewall ipset cache info
  firewall ipset cache clear`)
}
