package api

import (
	"encoding/json"
	"io"
	"net/http"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
)

// --- Config CRUD Handlers ---

// applyConfigUpdate safely applies a config modification:
// 1. Clones current config
// 2. Applies the update function to the clone
// 3. Validates the modified config
// 4. Sends to control plane via RPC
// 5. Only updates local s.Config on RPC success
//
// Returns true if the update was successful, false otherwise.
// On validation errors, writes 400 Bad Request.
// On RPC errors, writes 500 Internal Server Error.
func (s *Server) applyConfigUpdate(w http.ResponseWriter, updateFn func(cfg *config.Config)) bool {
	s.configMu.RLock()
	// Deep clone via JSON (safe for nested structs)
	data, err := json.Marshal(s.Config)
	s.configMu.RUnlock()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Failed to clone config")
		return false
	}

	var cloned config.Config
	if err := json.Unmarshal(data, &cloned); err != nil {
		WriteError(w, http.StatusInternalServerError, "Failed to clone config")
		return false
	}

	// Apply the update to the clone
	updateFn(&cloned)

	// Validate the modified config
	if errs := cloned.Validate(); errs.HasErrors() {
		WriteError(w, http.StatusBadRequest, "Validation failed: "+errs.Error())
		return false
	}

	// Apply to control plane (if available)
	if s.client != nil {
		if err := s.client.ApplyConfig(&cloned); err != nil {
			WriteError(w, http.StatusInternalServerError, "Failed to apply config: "+err.Error())
			return false
		}
		// Save config to disk
		if _, err := s.client.SaveConfig(); err != nil {
			// Log warning but don't fail - config is applied
			logging.Warn("Failed to persist config: " + err.Error())
		}
	}

	// Only update local config after successful RPC
	s.configMu.Lock()
	updateFn(s.Config)
	s.configMu.Unlock()

	return true
}

// handleGetPolicies returns firewall policies
// handleGetPolicies returns firewall policies
func (s *Server) handleGetPolicies(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.Policies)
	}
}

// handleUpdatePolicies updates firewall policies
func (s *Server) handleUpdatePolicies(w http.ResponseWriter, r *http.Request) {
	var policies []config.Policy
	if !BindJSON(w, r, &policies) {
		return
	}
	if s.applyConfigUpdate(w, func(cfg *config.Config) {
		cfg.Policies = policies
	}) {
		SuccessResponse(w)
	}
}

// handleGetNAT returns NAT configuration
// handleGetNAT returns NAT configuration
func (s *Server) handleGetNAT(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.NAT)
	}
}

// handleUpdateNAT updates NAT configuration
func (s *Server) handleUpdateNAT(w http.ResponseWriter, r *http.Request) {
	var nat []config.NATRule
	if !BindJSON(w, r, &nat) {
		return
	}
	if s.applyConfigUpdate(w, func(cfg *config.Config) {
		cfg.NAT = nat
	}) {
		SuccessResponse(w)
	}
}

// handleGetMarkRules returns mark rules
// handleGetMarkRules returns mark rules
func (s *Server) handleGetMarkRules(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.MarkRules)
	}
}

// handleUpdateMarkRules updates mark rules
func (s *Server) handleUpdateMarkRules(w http.ResponseWriter, r *http.Request) {
	var rules []config.MarkRule
	if !BindJSON(w, r, &rules) {
		return
	}
	if s.applyConfigUpdate(w, func(cfg *config.Config) {
		cfg.MarkRules = rules
	}) {
		SuccessResponse(w)
	}
}

// handleGetUIDRouting returns UID routing rules
// handleGetUIDRouting returns UID routing rules
func (s *Server) handleGetUIDRouting(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.UIDRouting)
	}
}

// handleUpdateUIDRouting updates UID routing rules
func (s *Server) handleUpdateUIDRouting(w http.ResponseWriter, r *http.Request) {
	var rules []config.UIDRouting
	if !BindJSON(w, r, &rules) {
		return
	}
	if s.applyConfigUpdate(w, func(cfg *config.Config) {
		cfg.UIDRouting = rules
	}) {
		SuccessResponse(w)
	}
}

// handleGetPolicyRoutes returns policy routing rules
func (s *Server) handleGetPolicyRoutes(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.PolicyRoutes)
	}
}

// handleUpdatePolicyRoutes updates policy routing rules
func (s *Server) handleUpdatePolicyRoutes(w http.ResponseWriter, r *http.Request) {
	var rules []config.PolicyRoute
	if !BindJSON(w, r, &rules) {
		return
	}
	if s.applyConfigUpdate(w, func(cfg *config.Config) {
		cfg.PolicyRoutes = rules
	}) {
		SuccessResponse(w)
	}
}

// handleGetIPSets returns IPSet configuration
// handleGetIPSets returns IPSet configuration
func (s *Server) handleGetIPSets(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.IPSets)
	}
}

// handleUpdateIPSets updates IPSet configuration
func (s *Server) handleUpdateIPSets(w http.ResponseWriter, r *http.Request) {
	var ipsets []config.IPSet
	if !BindJSON(w, r, &ipsets) {
		return
	}
	if s.applyConfigUpdate(w, func(cfg *config.Config) {
		cfg.IPSets = ipsets
	}) {
		SuccessResponse(w)
	}
}

// handleGetDHCP returns DHCP server configuration
// handleGetDHCP returns DHCP server configuration
func (s *Server) handleGetDHCP(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.DHCP)
	}
}

// handleUpdateDHCP updates DHCP server configuration
func (s *Server) handleUpdateDHCP(w http.ResponseWriter, r *http.Request) {
	var dhcp config.DHCPServer
	if !BindJSON(w, r, &dhcp) {
		return
	}
	if s.applyConfigUpdate(w, func(cfg *config.Config) {
		cfg.DHCP = &dhcp
	}) {
		SuccessResponse(w)
	}
}

// handleGetDNS returns DNS server configuration
func (s *Server) handleGetDNS(w http.ResponseWriter, r *http.Request) {
	cfg := s.GetConfigSnapshot(w)
	if cfg == nil {
		return
	}

	// Return both for compatibility
	resp := struct {
		Old *config.DNSServer `json:"dns_server,omitempty"`
		New *config.DNS       `json:"dns,omitempty"`
	}{
		Old: cfg.DNSServer,
		New: cfg.DNS,
	}
	WriteJSON(w, http.StatusOK, resp)
}

// handleUpdateDNS updates DNS server configuration
func (s *Server) handleUpdateDNS(w http.ResponseWriter, r *http.Request) {
	// Try to decode as wrapped format first
	var req struct {
		Old *config.DNSServer `json:"dns_server,omitempty"`
		New *config.DNS       `json:"dns,omitempty"`
	}

	// Read body once
	body, err := io.ReadAll(r.Body)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Failed to read request body")
		return
	}

	// Determine which format was sent
	var updateFn func(cfg *config.Config)

	if err := json.Unmarshal(body, &req); err == nil && (req.Old != nil || req.New != nil) {
		updateFn = func(cfg *config.Config) {
			if req.Old != nil {
				cfg.DNSServer = req.Old
			}
			if req.New != nil {
				cfg.DNS = req.New
			}
			config.ApplyPostLoadMigrations(cfg)
		}
	} else {
		// Try legacy raw DNSServer format
		var old config.DNSServer
		if err := json.Unmarshal(body, &old); err == nil {
			updateFn = func(cfg *config.Config) {
				cfg.DNSServer = &old
				config.ApplyPostLoadMigrations(cfg)
			}
		} else {
			WriteError(w, http.StatusBadRequest, "Invalid DNS configuration format")
			return
		}
	}

	if s.applyConfigUpdate(w, updateFn) {
		WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
	}
}


// handleGetRoutes returns static route configuration
// handleGetRoutes returns static route configuration
func (s *Server) handleGetRoutes(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.Routes)
	}
}

// handleUpdateRoutes updates static route configuration
func (s *Server) handleUpdateRoutes(w http.ResponseWriter, r *http.Request) {
	var routes []config.Route
	if !BindJSON(w, r, &routes) {
		return
	}
	if s.applyConfigUpdate(w, func(cfg *config.Config) {
		cfg.Routes = routes
	}) {
		SuccessResponse(w)
	}
}

// handleGetZones returns zone configuration
// handleGetZones returns zone configuration
func (s *Server) handleGetZones(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.Zones)
	}
}

// handleUpdateZones updates zone configuration
func (s *Server) handleUpdateZones(w http.ResponseWriter, r *http.Request) {
	var zones []config.Zone
	if !BindJSON(w, r, &zones) {
		return
	}
	if s.applyConfigUpdate(w, func(cfg *config.Config) {
		cfg.Zones = zones
	}) {
		SuccessResponse(w)
	}
}

// handleGetProtections returns per-interface protection settings
// handleGetProtections returns per-interface protection settings
func (s *Server) handleGetProtections(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.Protections)
	}
}

// handleUpdateProtections updates per-interface protection settings
func (s *Server) handleUpdateProtections(w http.ResponseWriter, r *http.Request) {
	// Accept either array directly or wrapped in { protections: [...] }
	var wrapper struct {
		Protections []config.InterfaceProtection `json:"protections"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Failed to read request body")
		return
	}

	var updateFn func(cfg *config.Config)

	// Try parsing as wrapper
	if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.Protections != nil {
		updateFn = func(cfg *config.Config) {
			cfg.Protections = wrapper.Protections
		}
	} else {
		// Try parsing as array
		var protections []config.InterfaceProtection
		if err := json.Unmarshal(body, &protections); err != nil {
			WriteError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		updateFn = func(cfg *config.Config) {
			cfg.Protections = protections
		}
	}

	if s.applyConfigUpdate(w, updateFn) {
		WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
	}
}

// handleGetVPN returns the current VPN configuration
// handleGetVPN returns the current VPN configuration
func (s *Server) handleGetVPN(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.VPN)
	}
}

// handleUpdateVPN updates the VPN configuration
func (s *Server) handleUpdateVPN(w http.ResponseWriter, r *http.Request) {
	var newVPN config.VPNConfig
	if !BindJSON(w, r, &newVPN) {
		return
	}
	if s.applyConfigUpdate(w, func(cfg *config.Config) {
		cfg.VPN = &newVPN
	}) {
		SuccessResponse(w)
	}
}

// handleGetQoS returns QoS policies configuration
// handleGetQoS returns QoS policies configuration
func (s *Server) handleGetQoS(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.QoSPolicies)
	}
}

// handleUpdateQoS updates QoS policies configuration
func (s *Server) handleUpdateQoS(w http.ResponseWriter, r *http.Request) {
	// Accept either array directly or wrapped in { qos_policies: [...] }
	var wrapper struct {
		QoSPolicies []config.QoSPolicy `json:"qos_policies"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Failed to read request body")
		return
	}

	var updateFn func(cfg *config.Config)

	if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.QoSPolicies != nil {
		updateFn = func(cfg *config.Config) {
			cfg.QoSPolicies = wrapper.QoSPolicies
		}
	} else {
		var qos []config.QoSPolicy
		if err := json.Unmarshal(body, &qos); err != nil {
			WriteError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		updateFn = func(cfg *config.Config) {
			cfg.QoSPolicies = qos
		}
	}

	if s.applyConfigUpdate(w, updateFn) {
		WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
	}
}
