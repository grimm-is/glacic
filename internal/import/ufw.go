package imports

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// UFWConfig holds parsed UFW configuration.
type UFWConfig struct {
	Rules      []UFWRule
	DefaultIn  string // allow, deny, reject
	DefaultOut string
	DefaultFwd string
	Enabled    bool
	IPv6       bool
	Warnings   []string
}

// UFWRule represents a UFW firewall rule.
type UFWRule struct {
	Action    string // allow, deny, reject, limit
	Direction string // in, out
	Interface string // on <interface>
	Protocol  string // tcp, udp, any
	FromIP    string
	FromPort  string
	ToIP      string
	ToPort    string
	App       string // Application profile name
	Comment   string
	V6        bool // IPv6 rule
	Disabled  bool
	RuleNum   int
}

// ParseUFWRules parses UFW rules from 'ufw status verbose' output or /lib/ufw/user.rules.
func ParseUFWRules(path string) (*UFWConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open UFW rules: %w", err)
	}
	defer file.Close()

	cfg := &UFWConfig{
		DefaultIn:  "deny",
		DefaultOut: "allow",
		DefaultFwd: "deny",
	}

	scanner := bufio.NewScanner(file)
	ruleNum := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse defaults from user.rules format
		if strings.HasPrefix(line, "-A ufw") || strings.HasPrefix(line, "*filter") {
			// iptables format - parse differently
			rule := parseIPTablesRule(line)
			if rule != nil {
				ruleNum++
				rule.RuleNum = ruleNum
				cfg.Rules = append(cfg.Rules, *rule)
			}
			continue
		}

		// Parse UFW status format: "To                         Action      From"
		// Example: "22/tcp                     ALLOW       Anywhere"
		if strings.Contains(line, "ALLOW") || strings.Contains(line, "DENY") ||
			strings.Contains(line, "REJECT") || strings.Contains(line, "LIMIT") {
			rule := parseUFWStatusLine(line)
			if rule != nil {
				ruleNum++
				rule.RuleNum = ruleNum
				cfg.Rules = append(cfg.Rules, *rule)
			}
			continue
		}

		// Parse default policies
		if strings.HasPrefix(line, "Default:") {
			parseUFWDefaults(cfg, line)
		}
	}

	return cfg, scanner.Err()
}

// parseUFWStatusLine parses a line from 'ufw status' output.
func parseUFWStatusLine(line string) *UFWRule {
	// Format: "22/tcp                     ALLOW       Anywhere"
	// Or:     "Anywhere                   ALLOW       192.168.1.0/24"
	// Or:     "22/tcp on eth0             ALLOW IN    Anywhere"

	rule := &UFWRule{
		Direction: "in", // Default
	}

	// Detect action
	var action string
	var actionIdx int
	for _, a := range []string{"ALLOW", "DENY", "REJECT", "LIMIT"} {
		if idx := strings.Index(line, a); idx >= 0 {
			action = strings.ToLower(a)
			actionIdx = idx
			break
		}
	}
	if action == "" {
		return nil
	}
	rule.Action = action

	// Check for direction
	afterAction := line[actionIdx:]
	if strings.Contains(afterAction, " IN") {
		rule.Direction = "in"
	} else if strings.Contains(afterAction, " OUT") {
		rule.Direction = "out"
	} else if strings.Contains(afterAction, " FWD") {
		rule.Direction = "forward"
	}

	// Parse To (before action)
	toPart := strings.TrimSpace(line[:actionIdx])

	// Check for interface
	if strings.Contains(toPart, " on ") {
		parts := strings.Split(toPart, " on ")
		toPart = strings.TrimSpace(parts[0])
		rule.Interface = strings.TrimSpace(parts[1])
	}

	// Parse port/protocol from To
	if strings.Contains(toPart, "/") {
		parts := strings.Split(toPart, "/")
		rule.ToPort = parts[0]
		rule.Protocol = parts[1]
	} else if toPart != "Anywhere" && toPart != "" {
		rule.ToIP = toPart
	}

	// Parse From (after action)
	fromPart := strings.TrimSpace(afterAction)
	// Remove action and direction
	for _, s := range []string{"ALLOW", "DENY", "REJECT", "LIMIT", " IN", " OUT", " FWD"} {
		fromPart = strings.ReplaceAll(fromPart, s, "")
	}
	fromPart = strings.TrimSpace(fromPart)

	if fromPart != "" && fromPart != "Anywhere" {
		if strings.Contains(fromPart, "/") && !strings.Contains(fromPart, ".") {
			// It's a port/protocol
			parts := strings.Split(fromPart, "/")
			rule.FromPort = parts[0]
		} else {
			rule.FromIP = fromPart
		}
	}

	// Check for IPv6
	if strings.Contains(line, "(v6)") {
		rule.V6 = true
	}

	return rule
}

// parseIPTablesRule parses iptables-style rules from user.rules.
func parseIPTablesRule(line string) *UFWRule {
	// Format: -A ufw-user-input -p tcp --dport 22 -j ACCEPT
	if !strings.HasPrefix(line, "-A ") {
		return nil
	}

	rule := &UFWRule{}

	// Parse chain for direction
	if strings.Contains(line, "input") {
		rule.Direction = "in"
	} else if strings.Contains(line, "output") {
		rule.Direction = "out"
	} else if strings.Contains(line, "forward") {
		rule.Direction = "forward"
	}

	// Parse protocol
	protoRe := regexp.MustCompile(`-p\s+(\w+)`)
	if m := protoRe.FindStringSubmatch(line); len(m) > 1 {
		rule.Protocol = m[1]
	}

	// Parse destination port
	dportRe := regexp.MustCompile(`--dport\s+(\S+)`)
	if m := dportRe.FindStringSubmatch(line); len(m) > 1 {
		rule.ToPort = m[1]
	}

	// Parse source port
	sportRe := regexp.MustCompile(`--sport\s+(\S+)`)
	if m := sportRe.FindStringSubmatch(line); len(m) > 1 {
		rule.FromPort = m[1]
	}

	// Parse source IP
	srcRe := regexp.MustCompile(`-s\s+(\S+)`)
	if m := srcRe.FindStringSubmatch(line); len(m) > 1 {
		rule.FromIP = m[1]
	}

	// Parse destination IP
	dstRe := regexp.MustCompile(`-d\s+(\S+)`)
	if m := dstRe.FindStringSubmatch(line); len(m) > 1 {
		rule.ToIP = m[1]
	}

	// Parse interface
	ifaceRe := regexp.MustCompile(`-i\s+(\S+)`)
	if m := ifaceRe.FindStringSubmatch(line); len(m) > 1 {
		rule.Interface = m[1]
	}

	// Parse action
	if strings.Contains(line, "-j ACCEPT") {
		rule.Action = "allow"
	} else if strings.Contains(line, "-j DROP") {
		rule.Action = "deny"
	} else if strings.Contains(line, "-j REJECT") {
		rule.Action = "reject"
	} else if strings.Contains(line, "-j LIMIT") || strings.Contains(line, "limit") {
		rule.Action = "limit"
	} else {
		return nil // Unknown action
	}

	// Parse comment
	commentRe := regexp.MustCompile(`--comment\s+"([^"]+)"`)
	if m := commentRe.FindStringSubmatch(line); len(m) > 1 {
		rule.Comment = m[1]
	}

	return rule
}

func parseUFWDefaults(cfg *UFWConfig, line string) {
	// Format: "Default: deny (incoming), allow (outgoing), deny (routed)"
	line = strings.TrimPrefix(line, "Default:")
	line = strings.TrimSpace(line)

	parts := strings.Split(line, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "incoming") {
			if strings.Contains(part, "allow") {
				cfg.DefaultIn = "allow"
			} else if strings.Contains(part, "reject") {
				cfg.DefaultIn = "reject"
			} else {
				cfg.DefaultIn = "deny"
			}
		} else if strings.Contains(part, "outgoing") {
			if strings.Contains(part, "deny") {
				cfg.DefaultOut = "deny"
			} else if strings.Contains(part, "reject") {
				cfg.DefaultOut = "reject"
			} else {
				cfg.DefaultOut = "allow"
			}
		} else if strings.Contains(part, "routed") {
			if strings.Contains(part, "allow") {
				cfg.DefaultFwd = "allow"
			} else if strings.Contains(part, "reject") {
				cfg.DefaultFwd = "reject"
			} else {
				cfg.DefaultFwd = "deny"
			}
		}
	}
}

// ToImportResult converts UFW config to ImportResult.
func (c *UFWConfig) ToImportResult() *ImportResult {
	result := &ImportResult{}

	// UFW is typically for a single host, so create a simple zone model
	result.Interfaces = []ImportedInterface{
		{
			OriginalName: "default",
			Zone:         "LOCAL",
			NeedsMapping: true,
			SuggestedIf:  "eth0",
		},
	}

	// Convert rules
	for _, r := range c.Rules {
		if r.Disabled {
			continue
		}

		rule := ImportedFilterRule{
			Description: r.Comment,
			Protocol:    r.Protocol,
			DestPort:    r.ToPort,
			SourcePort:  r.FromPort,
			Source:      r.FromIP,
			Destination: r.ToIP,
			Interface:   r.Interface,
			CanImport:   true,
		}

		if rule.Description == "" {
			rule.Description = fmt.Sprintf("UFW rule %d", r.RuleNum)
		}

		switch r.Action {
		case "allow":
			rule.Action = "accept"
		case "deny":
			rule.Action = "drop"
		case "reject":
			rule.Action = "reject"
		case "limit":
			rule.Action = "accept"
			rule.ImportNotes = append(rule.ImportNotes, "Rate-limited rule - configure rate limiting")
		}

		// Direction determines policy
		switch r.Direction {
		case "in":
			rule.SuggestedPolicy = "input_policy"
		case "out":
			rule.SuggestedPolicy = "output_policy"
		case "forward":
			rule.SuggestedPolicy = "forward_policy"
		}

		if r.V6 {
			rule.ImportNotes = append(rule.ImportNotes, "IPv6 rule")
		}

		if r.App != "" {
			rule.ImportNotes = append(rule.ImportNotes, "Application profile: "+r.App)
		}

		result.FilterRules = append(result.FilterRules, rule)
	}

	// Add guidance
	result.ManualSteps = append(result.ManualSteps,
		"UFW rules are host-based - adapt for router use",
		"Create appropriate zones for your network topology",
		"Review INPUT rules - may need to become policies",
	)

	if c.DefaultIn == "deny" {
		result.ManualSteps = append(result.ManualSteps,
			"Default incoming policy is DENY - ensure needed services are allowed")
	}

	return result
}

// ParseUFWApps parses UFW application profiles from /etc/ufw/applications.d/.
func ParseUFWApps(path string) (map[string]UFWApp, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	apps := make(map[string]UFWApp)
	scanner := bufio.NewScanner(file)

	var currentApp *UFWApp
	var currentName string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Section header [AppName]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if currentApp != nil && currentName != "" {
				apps[currentName] = *currentApp
			}
			currentName = strings.Trim(line, "[]")
			currentApp = &UFWApp{Name: currentName}
			continue
		}

		if currentApp == nil {
			continue
		}

		// Parse key=value
		if idx := strings.Index(line, "="); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])

			switch strings.ToLower(key) {
			case "title":
				currentApp.Title = value
			case "description":
				currentApp.Description = value
			case "ports":
				currentApp.Ports = parseUFWPorts(value)
			}
		}
	}

	// Save last app
	if currentApp != nil && currentName != "" {
		apps[currentName] = *currentApp
	}

	return apps, scanner.Err()
}

// UFWApp represents a UFW application profile.
type UFWApp struct {
	Name        string
	Title       string
	Description string
	Ports       []UFWPort
}

// UFWPort represents a port specification.
type UFWPort struct {
	Port     int
	EndPort  int    // For ranges
	Protocol string // tcp, udp
}

func parseUFWPorts(value string) []UFWPort {
	var ports []UFWPort

	// Format: "80,443/tcp" or "22/tcp|22/udp" or "6000:6063/tcp"
	parts := strings.Split(value, "|")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Split port/protocol
		var portPart, proto string
		if idx := strings.Index(part, "/"); idx > 0 {
			portPart = part[:idx]
			proto = part[idx+1:]
		} else {
			portPart = part
			proto = "tcp"
		}

		// Handle multiple ports (comma-separated)
		portNums := strings.Split(portPart, ",")
		for _, pn := range portNums {
			pn = strings.TrimSpace(pn)

			// Handle ranges
			if strings.Contains(pn, ":") {
				rangeParts := strings.Split(pn, ":")
				if len(rangeParts) == 2 {
					start, _ := strconv.Atoi(rangeParts[0])
					end, _ := strconv.Atoi(rangeParts[1])
					ports = append(ports, UFWPort{Port: start, EndPort: end, Protocol: proto})
				}
			} else {
				port, _ := strconv.Atoi(pn)
				if port > 0 {
					ports = append(ports, UFWPort{Port: port, Protocol: proto})
				}
			}
		}
	}

	return ports
}
