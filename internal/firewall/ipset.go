package firewall

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os/exec"
	"regexp"
	"strings"

	"grimm.is/glacic/internal/config"
)

var validSetNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func isValidSetName(name string) bool {
	return validSetNameRegex.MatchString(name)
}

// IPSetManager handles nftables set operations.
type IPSetManager struct {
	tableName string
	runner    CommandRunner
}

// NewIPSetManager creates a new IPSet manager.
func NewIPSetManager(tableName string) *IPSetManager {
	return &IPSetManager{
		tableName: tableName,
		runner:    DefaultCommandRunner,
	}
}

// SetRunner sets the command runner for testing.
func (m *IPSetManager) SetRunner(runner CommandRunner) {
	m.runner = runner
}

// SetType represents the type of elements in an nftables set.
type SetType string

const (
	SetTypeIPv4Addr    SetType = "ipv4_addr"
	SetTypeIPv6Addr    SetType = "ipv6_addr"
	SetTypeInetService SetType = "inet_service"
)

// CreateSet creates a new nftables set.
func (m *IPSetManager) CreateSet(name string, setType SetType, flags ...string) error {
	if !isValidSetName(name) {
		return fmt.Errorf("invalid set name: %s", name)
	}
	args := []string{"add", "set", "inet", m.tableName, name, "{", "type", string(setType) + ";"}
	if len(flags) > 0 {
		args = append(args, "flags", strings.Join(flags, ",")+";")
	}
	args = append(args, "}")
	return m.runNft(args...)
}

// CreateSetWithTimeout creates a set with automatic entry timeout.
func (m *IPSetManager) CreateSetWithTimeout(name string, setType SetType, timeout string) error {
	if !isValidSetName(name) {
		return fmt.Errorf("invalid set name: %s", name)
	}
	return m.runNft("add", "set", "inet", m.tableName, name, "{", "type", string(setType)+";", "flags", "timeout;", "timeout", timeout+";", "}")
}

// DeleteSet removes an nftables set.
func (m *IPSetManager) DeleteSet(name string) error {
	if !isValidSetName(name) {
		return fmt.Errorf("invalid set name: %s", name)
	}
	return m.runNft("delete", "set", "inet", m.tableName, name)
}

// FlushSet removes all elements from a set.
func (m *IPSetManager) FlushSet(name string) error {
	return m.runNft("flush", "set", "inet", m.tableName, name)
}

// AddElements adds elements to an existing set.
func (m *IPSetManager) AddElements(setName string, elements []string) error {
	if len(elements) == 0 {
		return nil
	}

	// Add in batches to avoid command line length limits
	batchSize := 100
	for i := 0; i < len(elements); i += batchSize {
		end := i + batchSize
		if end > len(elements) {
			end = len(elements)
		}
		batch := elements[i:end]

		// Prepare arguments
		args := []string{"add", "element", "inet", m.tableName, setName, "{"}

		// Add elements (handling logic similar to previous strings.Split behavior)
		for j, elem := range batch {
			token := elem
			if j < len(batch)-1 {
				token += ","
			}
			args = append(args, token)
		}
		args = append(args, "}")

		if err := m.runNft(args...); err != nil {
			return fmt.Errorf("failed to add elements batch %d: %w", i/batchSize, err)
		}
	}
	return nil
}

// RemoveElements removes elements from a set.
func (m *IPSetManager) RemoveElements(setName string, elements []string) error {
	if len(elements) == 0 {
		return nil
	}

	args := []string{"delete", "element", "inet", m.tableName, setName, "{"}
	for i, elem := range elements {
		token := elem
		if i < len(elements)-1 {
			token += ","
		}
		args = append(args, token)
	}
	args = append(args, "}")
	return m.runNft(args...)
}

// ReloadSet completely replaces a set's contents
func (m *IPSetManager) ReloadSet(name string, elements []string) error {
	// Security: Validate set name prevents script injection
	if !isValidSetName(name) {
		return fmt.Errorf("invalid set name: %s", name)
	}

	var sb strings.Builder
	// 1. Flush the set
	sb.WriteString(fmt.Sprintf("flush set inet %s %s\n", m.tableName, name))

	// 2. Add elements
	if len(elements) > 0 {
		// Batching for script readability
		batchSize := 500
		for i := 0; i < len(elements); i += batchSize {
			end := i + batchSize
			if end > len(elements) {
				end = len(elements)
			}
			batch := elements[i:end]
			sb.WriteString(fmt.Sprintf("add element inet %s %s { %s }\n",
				m.tableName, name, strings.Join(batch, ", ")))
		}
	}

	script := sb.String()

	// Apply atomically via nft -f
	return m.runner.RunInput(script, "nft", "-f", "-")
}

// ListSets returns all sets in the table.
func (m *IPSetManager) ListSets() ([]string, error) {
	out, err := m.runner.Output("nft", "-j", "list", "sets", "inet", m.tableName)
	if err != nil {
		return nil, err
	}

	// Simple parsing - look for set names
	var sets []string
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, `"set"`) && strings.Contains(line, `"name"`) {
			// Extract name from JSON
			start := strings.Index(line, `"name":"`)
			if start != -1 {
				start += 8
				end := strings.Index(line[start:], `"`)
				if end != -1 {
					sets = append(sets, line[start:start+end])
				}
			}
		}
	}
	return sets, nil
}

// GetSetElements returns all elements in a set.
func (m *IPSetManager) GetSetElements(setName string) ([]string, error) {
	out, err := m.runner.Output("nft", "list", "set", "inet", m.tableName, setName)
	if err != nil {
		return nil, err
	}

	var elements []string
	output := string(out)

	// Parse elements block: elements = { ip1, ip2, ... }
	start := strings.Index(output, "elements = {")
	if start == -1 {
		return elements, nil // Empty set
	}
	start += 12

	end := strings.Index(output[start:], "}")
	if end == -1 {
		return elements, nil
	}

	elemBlock := output[start : start+end]
	for _, elem := range strings.Split(elemBlock, ",") {
		elem = strings.TrimSpace(elem)
		if elem != "" {
			elements = append(elements, elem)
		}
	}

	return elements, nil
}

// CheckElement checks if a single element exists in a set using O(1) nft get element.
// Returns true if the element exists, false if not. Returns error on command failure.
func (m *IPSetManager) CheckElement(setName, element string) (bool, error) {
	if !isValidSetName(setName) {
		return false, fmt.Errorf("invalid set name: %s", setName)
	}

	// Use nft get element for O(1) lookup
	// Command: nft get element inet <table> <set> { <element> }
	args := []string{"get", "element", "inet", m.tableName, setName, "{", element, "}"}
	err := m.runNft(args...)
	if err != nil {
		// Exit code 1 with "element not found" means element doesn't exist
		if strings.Contains(err.Error(), "element not found") ||
			strings.Contains(err.Error(), "No such file or directory") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ApplyIPSet creates a set from config and populates it.
func (m *IPSetManager) ApplyIPSet(ipset config.IPSet) error {
	// Determine set type
	setType := SetTypeIPv4Addr
	if ipset.Type != "" {
		setType = SetType(ipset.Type)
	}

	// Create the set (ignore error if exists? No, we want to know if it fails)
	if err := m.CreateSet(ipset.Name, setType, "interval"); err != nil {
		// Log but continue? No, if create fails, add will fail
		return fmt.Errorf("failed to create set %s: %w", ipset.Name, err)
	}

	// Flush existing entries
	if err := m.FlushSet(ipset.Name); err != nil {
		return fmt.Errorf("failed to flush set %s: %w", ipset.Name, err)
	}

	// Add static entries
	if len(ipset.Entries) > 0 {
		if err := m.AddElements(ipset.Name, ipset.Entries); err != nil {
			return fmt.Errorf("failed to add entries to %s: %w", ipset.Name, err)
		}
	}

	return nil
}

// CreateBlockingRule creates a rule to block traffic matching the set.
func (m *IPSetManager) CreateBlockingRule(setName string, setType SetType, chainName, action string, matchSource, matchDest bool) error {

	addressFamily := "ip"
	if setType == SetTypeIPv6Addr {
		addressFamily = "ip6"
	}

	if matchSource {
		if err := m.runNft("add", "rule", "inet", m.tableName, chainName, addressFamily, "saddr", "@"+setName, action); err != nil {
			return err
		}
	}
	if matchDest {
		if err := m.runNft("add", "rule", "inet", m.tableName, chainName, addressFamily, "daddr", "@"+setName, action); err != nil {
			return err
		}
	}
	return nil
}

// ParseIPList parses a list of IPs/CIDRs from a reader.
// Handles comments (#) and empty lines.
func ParseIPList(r io.Reader) ([]string, error) {
	var ips []string
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// Handle inline comments
		if idx := strings.Index(line, "#"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}
		if idx := strings.Index(line, ";"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}

		// Validate IP or CIDR
		if isValidIPOrCIDR(line) {
			ips = append(ips, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return ips, nil
}

// isValidIPOrCIDR checks if a string is a valid IP address or CIDR.
func isValidIPOrCIDR(s string) bool {
	// Try as CIDR
	if strings.Contains(s, "/") {
		_, _, err := net.ParseCIDR(s)
		return err == nil
	}
	// Try as IP
	return net.ParseIP(s) != nil
}

func (m *IPSetManager) runNft(args ...string) error {
	return m.runner.Run("nft", args...)
}

// runNftScript runs multiple nft commands atomically.
func (m *IPSetManager) runNftScript(commands []string) error {
	script := strings.Join(commands, "\n")
	// We can't easily use CommandRunner for this because it requires Stdin.
	// For now, we'll stick with exec.Command, but this makes it harder to mock.
	// Ideally CommandRunner should support Stdin or Exec with options.
	// Since runNftScript is not used in the immediate P1 path (UpdateIPSet uses FlushSet/AddElements which use runNft),
	// we can leave it for now.
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	return cmd.Run()
}
