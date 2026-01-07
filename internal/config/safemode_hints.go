package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"grimm.is/glacic/internal/brand"
)

// SafeModeHints contains cached metadata for safe mode boot.
// This is written on every successful config load and read during
// safe mode boot to understand the network topology.
type SafeModeHints struct {
	// Timestamp of when hints were last updated
	UpdatedAt time.Time `json:"updated_at"`

	// Interface configurations
	Interfaces []SafeModeInterface `json:"interfaces"`

	// Zone mappings (interface -> zone type)
	Zones map[string]string `json:"zones"` // e.g., "eth0": "WAN", "eth1": "LAN"

	// API configuration
	APIEnabled   bool   `json:"api_enabled"`
	APIListen    string `json:"api_listen"`
	APITLSListen string `json:"api_tls_listen"`
}

// SafeModeInterface contains per-interface hints for safe mode.
type SafeModeInterface struct {
	Name        string   `json:"name"`
	DHCP        bool     `json:"dhcp"`        // True if interface uses DHCP
	StaticIPv4  []string `json:"static_ipv4"` // Static IPs if not DHCP
	Gateway     string   `json:"gateway"`     // Default gateway (for WAN)
	Zone        string   `json:"zone"`        // Zone type: WAN, LAN, DMZ, etc.
	Management  bool     `json:"management"`  // True if management access allowed
	Description string   `json:"description"`
}

// safeModeHintsFile returns the path to the hints file.
func safeModeHintsFile() string {
	return filepath.Join(brand.GetStateDir(), "safe_mode_hints.json")
}

// SaveSafeModeHints extracts and persists safe mode hints from a config.
// This should be called after every successful config load.
func SaveSafeModeHints(cfg *Config) error {
	hints := &SafeModeHints{
		UpdatedAt: time.Now(),
		Zones:     make(map[string]string),
	}

	// Extract zone mappings
	for _, zone := range cfg.Zones {
		for _, iface := range zone.Interfaces {
			hints.Zones[iface] = zone.Name
		}
		if zone.Interface != "" {
			hints.Zones[zone.Interface] = zone.Name
		}
	}

	// Extract interface configurations
	for _, iface := range cfg.Interfaces {
		hint := SafeModeInterface{
			Name:        iface.Name,
			DHCP:        iface.DHCP,
			StaticIPv4:  iface.IPv4,
			Gateway:     iface.Gateway,
			Zone:        iface.Zone,
			Description: iface.Description,
		}

		// Check if zone name from interface or zone lookup
		if hint.Zone == "" {
			hint.Zone = hints.Zones[iface.Name]
		}

		// Determine if management is allowed
		if iface.Management != nil {
			hint.Management = iface.Management.Web || iface.Management.API || iface.Management.SSH
		}

		// Also check zone-level management
		for _, zone := range cfg.Zones {
			if zone.Interface == iface.Name || containsString(zone.Interfaces, iface.Name) {
				if zone.Management != nil {
					if zone.Management.Web || zone.Management.API || zone.Management.SSH {
						hint.Management = true
					}
				}
			}
		}

		hints.Interfaces = append(hints.Interfaces, hint)
	}

	// API settings
	if cfg.API != nil {
		hints.APIEnabled = cfg.API.Enabled
		hints.APIListen = cfg.API.Listen
		hints.APITLSListen = cfg.API.TLSListen
	} else {
		hints.APIEnabled = true // Default
		hints.APIListen = ":8080"
		hints.APITLSListen = ":8443"
	}

	// Write to disk
	data, err := json.MarshalIndent(hints, "", "  ")
	if err != nil {
		return err
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(safeModeHintsFile()), 0755); err != nil {
		return err
	}

	return os.WriteFile(safeModeHintsFile(), data, 0644)
}

// LoadSafeModeHints reads cached safe mode hints from disk.
// Returns nil if hints don't exist or can't be read.
func LoadSafeModeHints() *SafeModeHints {
	data, err := os.ReadFile(safeModeHintsFile())
	if err != nil {
		return nil
	}

	var hints SafeModeHints
	if err := json.Unmarshal(data, &hints); err != nil {
		return nil
	}

	return &hints
}

// GetDHCPInterfaces returns the list of interfaces that use DHCP.
func (h *SafeModeHints) GetDHCPInterfaces() []string {
	var result []string
	for _, iface := range h.Interfaces {
		if iface.DHCP {
			result = append(result, iface.Name)
		}
	}
	return result
}

// GetManagementInterfaces returns interfaces that allow management access.
func (h *SafeModeHints) GetManagementInterfaces() []string {
	var result []string
	for _, iface := range h.Interfaces {
		if iface.Management {
			result = append(result, iface.Name)
		}
	}
	return result
}

// GetWANInterfaces returns interfaces in WAN-like zones.
func (h *SafeModeHints) GetWANInterfaces() []string {
	var result []string
	wanZones := map[string]bool{"WAN": true, "wan": true, "Internet": true, "internet": true}

	for _, iface := range h.Interfaces {
		if wanZones[iface.Zone] {
			result = append(result, iface.Name)
		}
	}
	return result
}

// GetLANInterfaces returns interfaces NOT in WAN-like zones.
func (h *SafeModeHints) GetLANInterfaces() []string {
	var result []string
	wanZones := map[string]bool{"WAN": true, "wan": true, "Internet": true, "internet": true}

	for _, iface := range h.Interfaces {
		if !wanZones[iface.Zone] {
			result = append(result, iface.Name)
		}
	}
	return result
}
