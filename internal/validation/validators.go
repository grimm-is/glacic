package validation

import (
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strings"
)

// Interface name validation
var (
	// Valid interface name: alphanumeric, dash, underscore, dot (for VLANs), max 15 chars
	interfaceNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,15}$`)

	// Valid identifier: alphanumeric, dash, underscore
	identifierRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

	// Dangerous characters that should never appear in identifiers
	dangerousChars = []string{";", "|", "&", "$", "`", "(", ")", "<", ">", "\\", "\"", "'", "\n", "\r"}
)

// ValidateInterfaceName validates a network interface name
func ValidateInterfaceName(name string) error {
	if name == "" {
		return fmt.Errorf("interface name cannot be empty")
	}

	if len(name) > 15 {
		return fmt.Errorf("interface name too long (max 15 characters): %s", name)
	}

	if !interfaceNameRegex.MatchString(name) {
		return fmt.Errorf("invalid interface name: %s (must be alphanumeric with -_.)", name)
	}

	// Check for dangerous characters
	for _, char := range dangerousChars {
		if strings.Contains(name, char) {
			return fmt.Errorf("interface name contains dangerous character: %s", char)
		}
	}

	return nil
}

// ValidateIdentifier validates a general identifier (policy names, zone names, etc.)
func ValidateIdentifier(id string) error {
	if id == "" {
		return fmt.Errorf("identifier cannot be empty")
	}

	if len(id) > 255 {
		return fmt.Errorf("identifier too long (max 255 characters)")
	}

	if !identifierRegex.MatchString(id) {
		return fmt.Errorf("invalid identifier: %s (must be alphanumeric with -_)", id)
	}

	// Check for dangerous characters
	for _, char := range dangerousChars {
		if strings.Contains(id, char) {
			return fmt.Errorf("identifier contains dangerous character: %s", char)
		}
	}

	return nil
}

// ValidatePath validates a file path against an allowlist of permitted directories
func ValidatePath(path string, allowedDirs []string) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	// Clean the path to normalize it
	cleanPath := filepath.Clean(path)

	// Reject absolute paths outside allowlist
	if filepath.IsAbs(cleanPath) {
		allowed := false
		for _, allowedDir := range allowedDirs {
			if strings.HasPrefix(cleanPath, filepath.Clean(allowedDir)) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("path not in allowed directories: %s", cleanPath)
		}
	}

	// Reject path traversal attempts
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed: %s", path)
	}

	// Check for null bytes
	if strings.Contains(path, "\x00") {
		return fmt.Errorf("null byte in path")
	}

	return nil
}

// ValidateIPOrCIDR validates an IP address or CIDR range
func ValidateIPOrCIDR(s string) error {
	if s == "" {
		return fmt.Errorf("IP/CIDR cannot be empty")
	}

	// Try parsing as CIDR first
	if strings.Contains(s, "/") {
		_, _, err := net.ParseCIDR(s)
		if err != nil {
			return fmt.Errorf("invalid CIDR: %w", err)
		}
		return nil
	}

	// Try parsing as IP
	ip := net.ParseIP(s)
	if ip == nil {
		return fmt.Errorf("invalid IP address: %s", s)
	}

	return nil
}

// ValidateAllowlist checks if a value is in an allowed list
func ValidateAllowlist(value string, allowed []string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return fmt.Errorf("value not in allowlist: %s", value)
}

// ValidatePortNumber validates a port number
func ValidatePortNumber(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port number: %d (must be 1-65535)", port)
	}
	return nil
}

// ValidateProtocol validates a protocol name
func ValidateProtocol(proto string) error {
	validProtocols := []string{"tcp", "udp", "icmp", "icmpv6", "ah", "esp", "gre", "all"}
	proto = strings.ToLower(proto)

	for _, valid := range validProtocols {
		if proto == valid {
			return nil
		}
	}

	return fmt.Errorf("invalid protocol: %s (must be one of: %s)", proto, strings.Join(validProtocols, ", "))
}

// SanitizeString removes dangerous characters from a string (for display purposes)
func SanitizeString(s string) string {
	for _, char := range dangerousChars {
		s = strings.ReplaceAll(s, char, "")
	}
	return s
}
