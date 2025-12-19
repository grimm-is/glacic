package storage

import (
	"net"
	"strings"
)

// HasPermission checks if a key has a specific permission.
func (k *APIKey) HasPermission(required Permission) bool {
	for _, p := range k.Permissions {
		if p == PermAll {
			return true
		}
		if p == required {
			return true
		}
		// Check wildcards
		if p == PermReadAll && strings.HasSuffix(string(required), ":read") {
			return true
		}
		if p == PermWriteAll && strings.HasSuffix(string(required), ":write") {
			return true
		}
		if p == PermAdminAll && strings.HasPrefix(string(required), "admin:") {
			return true
		}
	}
	return false
}

// HasAnyPermission checks if a key has any of the specified permissions.
func (k *APIKey) HasAnyPermission(required ...Permission) bool {
	for _, r := range required {
		if k.HasPermission(r) {
			return true
		}
	}
	return false
}

// IsIPAllowed checks if the given IP is allowed for this key.
// Supports both exact IP matching and CIDR notation.
func (k *APIKey) IsIPAllowed(ip string) bool {
	if len(k.AllowedIPs) == 0 {
		return true // No restrictions
	}

	clientIP := net.ParseIP(ip)
	if clientIP == nil {
		return false // Invalid IP format
	}

	for _, allowed := range k.AllowedIPs {
		// Check for exact match first
		if allowed == ip {
			return true
		}

		// Check if it's a CIDR notation
		if strings.Contains(allowed, "/") {
			_, network, err := net.ParseCIDR(allowed)
			if err == nil && network.Contains(clientIP) {
				return true
			}
		} else {
			// Also try exact IP match via net.IP for normalization
			allowedIP := net.ParseIP(allowed)
			if allowedIP != nil && allowedIP.Equal(clientIP) {
				return true
			}
		}
	}
	return false
}

// IsPathAllowed checks if the given path is allowed for this key.
func (k *APIKey) IsPathAllowed(path string) bool {
	if len(k.AllowedPaths) == 0 {
		return true // No restrictions
	}
	for _, allowed := range k.AllowedPaths {
		if strings.HasPrefix(path, allowed) {
			return true
		}
	}
	return false
}
