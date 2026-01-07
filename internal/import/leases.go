package imports

import (
	"bufio"
	"fmt"
	"grimm.is/glacic/internal/clock"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Lease represents a DHCP lease from an external server.
type Lease struct {
	MAC        string
	IP         net.IP
	Hostname   string
	Expiry     time.Time
	ClientID   string
	LeaseStart time.Time
	BindState  string // For ISC DHCP: "active", "free", etc.
}

// ParseDnsmasqLeases parses a dnsmasq lease file.
// Format: <expiry-time> <mac> <ip> <hostname> <client-id>
func ParseDnsmasqLeases(path string) ([]Lease, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open lease file: %w", err)
	}
	defer file.Close()

	var leases []Lease
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue // Invalid line
		}

		// Parse expiry time (Unix timestamp)
		expiry, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}

		lease := Lease{
			MAC:      normalizeMACAddress(fields[1]),
			IP:       net.ParseIP(fields[2]),
			Hostname: fields[3],
			Expiry:   time.Unix(expiry, 0),
		}

		// Optional client-id
		if len(fields) > 4 {
			lease.ClientID = fields[4]
		}

		// Skip if hostname is "*" (no hostname)
		if lease.Hostname == "*" {
			lease.Hostname = ""
		}

		leases = append(leases, lease)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading lease file: %w", err)
	}

	return leases, nil
}

// ParseISCDHCPLeases parses an ISC DHCP server lease file.
// Format is more complex with multi-line lease blocks.
func ParseISCDHCPLeases(path string) ([]Lease, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open lease file: %w", err)
	}
	defer file.Close()

	var leases []Lease
	scanner := bufio.NewScanner(file)

	var currentLease *Lease
	inLease := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments
		if strings.HasPrefix(line, "#") {
			continue
		}

		// Start of lease block
		if strings.HasPrefix(line, "lease ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				ip := net.ParseIP(parts[1])
				if ip != nil {
					currentLease = &Lease{IP: ip}
					inLease = true
				}
			}
			continue
		}

		// End of lease block
		if line == "}" && inLease && currentLease != nil {
			leases = append(leases, *currentLease)
			currentLease = nil
			inLease = false
			continue
		}

		// Parse lease properties
		if inLease && currentLease != nil {
			// Remove trailing semicolon
			line = strings.TrimSuffix(line, ";")

			if strings.HasPrefix(line, "starts ") {
				// Format: starts 6 2024/01/15 10:30:45
				currentLease.LeaseStart = parseISCDateTime(line)
			} else if strings.HasPrefix(line, "ends ") {
				// Format: ends 6 2024/01/15 22:30:45
				currentLease.Expiry = parseISCDateTime(line)
			} else if strings.HasPrefix(line, "hardware ethernet ") {
				mac := strings.TrimPrefix(line, "hardware ethernet ")
				currentLease.MAC = normalizeMACAddress(mac)
			} else if strings.HasPrefix(line, "client-hostname ") {
				hostname := strings.TrimPrefix(line, "client-hostname ")
				currentLease.Hostname = strings.Trim(hostname, "\"")
			} else if strings.HasPrefix(line, "binding state ") {
				currentLease.BindState = strings.TrimPrefix(line, "binding state ")
			} else if strings.HasPrefix(line, "uid ") {
				currentLease.ClientID = strings.Trim(strings.TrimPrefix(line, "uid "), "\"")
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading lease file: %w", err)
	}

	// Filter to only active leases
	var activeLeases []Lease
	for _, l := range leases {
		if l.BindState == "" || l.BindState == "active" {
			activeLeases = append(activeLeases, l)
		}
	}

	return activeLeases, nil
}

// parseISCDateTime parses ISC DHCP date format: "starts 6 2024/01/15 10:30:45"
func parseISCDateTime(line string) time.Time {
	// Extract date/time parts
	parts := strings.Fields(line)
	if len(parts) < 4 {
		return time.Time{}
	}

	// Format: YYYY/MM/DD HH:MM:SS
	dateStr := parts[2] + " " + parts[3]
	t, err := time.Parse("2006/01/02 15:04:05", dateStr)
	if err != nil {
		return time.Time{}
	}
	return t
}

// FilterActiveLeases returns only leases that haven't expired.
func FilterActiveLeases(leases []Lease) []Lease {
	now := clock.Now()
	var active []Lease
	for _, l := range leases {
		if l.Expiry.IsZero() || l.Expiry.After(now) {
			active = append(active, l)
		}
	}
	return active
}

// MergeLeases combines leases from multiple sources, preferring newer entries.
func MergeLeases(sources ...[]Lease) []Lease {
	// Use MAC+IP as key
	leaseMap := make(map[string]Lease)

	for _, source := range sources {
		for _, l := range source {
			key := l.MAC + "|" + l.IP.String()
			existing, ok := leaseMap[key]
			if !ok || l.Expiry.After(existing.Expiry) {
				leaseMap[key] = l
			}
		}
	}

	var merged []Lease
	for _, l := range leaseMap {
		merged = append(merged, l)
	}
	return merged
}
