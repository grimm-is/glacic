package api

import (
	"encoding/json"
	"io"
	"net/http"

	"grimm.is/glacic/internal/config"
)

// --- Config CRUD Handlers ---

// checkPendingStatus compares staged config with running config
func (s *Server) checkPendingStatus() bool {
	s.configMu.RLock()
	staged := s.Config
	s.configMu.RUnlock()

	if staged == nil || s.client == nil {
		return false
	}

	running, err := s.client.GetConfig()
	if err != nil {
		// If we can't reach control plane, assume no pending changes to avoid UI noise
		return false
	}

	// Compare serialized versions to detect deep differences
	stagedJSON, _ := json.Marshal(staged)
	runningJSON, _ := json.Marshal(running)

	return string(stagedJSON) != string(runningJSON)
}

// broadcastPendingStatus notifies subscribers of pending status change
func (s *Server) broadcastPendingStatus() {
	if s.wsManager == nil {
		return
	}
	s.wsManager.TriggerStatusUpdate()

	// Deprecated: For backwards compatibility, still publish the old format for now.
	// We can remove this once ui/src/lib/stores/app.ts fully relies on status topic.
	hasPending := s.checkPendingStatus()
	s.wsManager.Publish("pending_status", map[string]bool{
		"has_pending": hasPending,
	})
}

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
func (s *Server) applyConfigUpdate(w http.ResponseWriter, r *http.Request, updateFn func(cfg *config.Config)) bool {
	s.configMu.RLock()
	// Deep clone via JSON (safe for nested structs)
	data, err := json.Marshal(s.Config)
	s.configMu.RUnlock()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to clone config")
		return false
	}

	var cloned config.Config
	if err := json.Unmarshal(data, &cloned); err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to clone config")
		return false
	}

	// Apply the update to the clone
	updateFn(&cloned)

	// Validate the modified config
	if errs := cloned.Validate(); errs.HasErrors() {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Validation failed: "+errs.Error())
		return false
	}

	// Staging Logic:
	// We do NOT call s.client.ApplyConfig here. We only update the local s.Config (Staged).
	// The user must explicitly hit POST /config/apply to commit changes to the Control Plane.

	// Update local config (Staged)
	s.configMu.Lock()
	updateFn(s.Config)
	s.configMu.Unlock()

	// Notify UI of pending changes
	go s.broadcastPendingStatus()

	return true
}

// handleGetPolicies returns firewall policies
// handleGetPolicies returns firewall policies
func (s *Server) handleGetPolicies(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w, r); cfg != nil {
		HandleGetData(w, cfg.Policies)
	}
}

// handleUpdatePolicies updates firewall policies
func (s *Server) handleUpdatePolicies(w http.ResponseWriter, r *http.Request) {
	var policies []config.Policy
	if !BindJSONLenient(w, r, &policies) {
		return
	}
	if s.applyConfigUpdate(w, r, func(cfg *config.Config) {
		cfg.Policies = policies
	}) {
		SuccessResponse(w)
	}
}

// handleGetNAT returns NAT configuration
// handleGetNAT returns NAT configuration
func (s *Server) handleGetNAT(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w, r); cfg != nil {
		HandleGetData(w, cfg.NAT)
	}
}

// handleUpdateNAT updates NAT configuration
func (s *Server) handleUpdateNAT(w http.ResponseWriter, r *http.Request) {
	var nat []config.NATRule
	if !BindJSONLenient(w, r, &nat) {
		return
	}
	if s.applyConfigUpdate(w, r, func(cfg *config.Config) {
		cfg.NAT = nat
	}) {
		SuccessResponse(w)
	}
}

// handleGetMarkRules returns mark rules
// handleGetMarkRules returns mark rules
func (s *Server) handleGetMarkRules(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w, r); cfg != nil {
		HandleGetData(w, cfg.MarkRules)
	}
}

// handleUpdateMarkRules updates mark rules
func (s *Server) handleUpdateMarkRules(w http.ResponseWriter, r *http.Request) {
	var rules []config.MarkRule
	if !BindJSONLenient(w, r, &rules) {
		return
	}
	if s.applyConfigUpdate(w, r, func(cfg *config.Config) {
		cfg.MarkRules = rules
	}) {
		SuccessResponse(w)
	}
}

// handleGetUIDRouting returns UID routing rules
// handleGetUIDRouting returns UID routing rules
func (s *Server) handleGetUIDRouting(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w, r); cfg != nil {
		HandleGetData(w, cfg.UIDRouting)
	}
}

// handleUpdateUIDRouting updates UID routing rules
func (s *Server) handleUpdateUIDRouting(w http.ResponseWriter, r *http.Request) {
	var rules []config.UIDRouting
	if !BindJSONLenient(w, r, &rules) {
		return
	}
	if s.applyConfigUpdate(w, r, func(cfg *config.Config) {
		cfg.UIDRouting = rules
	}) {
		SuccessResponse(w)
	}
}

// handleGetPolicyRoutes returns policy routing rules
func (s *Server) handleGetPolicyRoutes(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w, r); cfg != nil {
		HandleGetData(w, cfg.PolicyRoutes)
	}
}

// handleUpdatePolicyRoutes updates policy routing rules
func (s *Server) handleUpdatePolicyRoutes(w http.ResponseWriter, r *http.Request) {
	var rules []config.PolicyRoute
	if !BindJSONLenient(w, r, &rules) {
		return
	}
	if s.applyConfigUpdate(w, r, func(cfg *config.Config) {
		cfg.PolicyRoutes = rules
	}) {
		SuccessResponse(w)
	}
}

// handleGetIPSets returns IPSet configuration
// handleGetIPSets returns IPSet configuration
func (s *Server) handleGetIPSets(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w, r); cfg != nil {
		HandleGetData(w, cfg.IPSets)
	}
}

// handleUpdateIPSets updates IPSet configuration
func (s *Server) handleUpdateIPSets(w http.ResponseWriter, r *http.Request) {
	var ipsets []config.IPSet
	if !BindJSONLenient(w, r, &ipsets) {
		return
	}
	if s.applyConfigUpdate(w, r, func(cfg *config.Config) {
		cfg.IPSets = ipsets
	}) {
		SuccessResponse(w)
	}
}

// handleGetDHCP returns DHCP server configuration
// handleGetDHCP returns DHCP server configuration
func (s *Server) handleGetDHCP(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w, r); cfg != nil {
		HandleGetData(w, cfg.DHCP)
	}
}

// handleUpdateDHCP updates DHCP server configuration
func (s *Server) handleUpdateDHCP(w http.ResponseWriter, r *http.Request) {
	var dhcp config.DHCPServer
	if !BindJSONLenient(w, r, &dhcp) {
		return
	}
	if s.applyConfigUpdate(w, r, func(cfg *config.Config) {
		cfg.DHCP = &dhcp
	}) {
		SuccessResponse(w)
	}
}

// handleGetDNS returns DNS server configuration
func (s *Server) handleGetDNS(w http.ResponseWriter, r *http.Request) {
	cfg := s.GetConfigSnapshot(w, r)
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

	// Limit request body to prevent memory exhaustion (10MB max for config)
	const maxConfigSize = 10 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(r.Body, maxConfigSize))
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to read request body")
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
			WriteErrorCtx(w, r, http.StatusBadRequest, "Invalid DNS configuration format")
			return
		}
	}

	if s.applyConfigUpdate(w, r, updateFn) {
		WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
	}
}

// handleGetRoutes returns static route configuration
// handleGetRoutes returns static route configuration
func (s *Server) handleGetRoutes(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w, r); cfg != nil {
		HandleGetData(w, cfg.Routes)
	}
}

// handleUpdateRoutes updates static route configuration
func (s *Server) handleUpdateRoutes(w http.ResponseWriter, r *http.Request) {
	var routes []config.Route
	if !BindJSONLenient(w, r, &routes) {
		return
	}
	if s.applyConfigUpdate(w, r, func(cfg *config.Config) {
		cfg.Routes = routes
	}) {
		SuccessResponse(w)
	}
}

// handleGetZones returns zone configuration
// handleGetZones returns zone configuration
func (s *Server) handleGetZones(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w, r); cfg != nil {
		HandleGetData(w, cfg.Zones)
	}
}

// handleUpdateZones updates zone configuration
func (s *Server) handleUpdateZones(w http.ResponseWriter, r *http.Request) {
	var zones []config.Zone
	if !BindJSONLenient(w, r, &zones) {
		return
	}
	if s.applyConfigUpdate(w, r, func(cfg *config.Config) {
		cfg.Zones = zones
	}) {
		SuccessResponse(w)
	}
}

// handleGetProtections returns per-interface protection settings
// handleGetProtections returns per-interface protection settings
func (s *Server) handleGetProtections(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w, r); cfg != nil {
		HandleGetData(w, cfg.Protections)
	}
}

// handleUpdateProtections updates per-interface protection settings
func (s *Server) handleUpdateProtections(w http.ResponseWriter, r *http.Request) {
	// Accept either array directly or wrapped in { protections: [...] }
	var wrapper struct {
		Protections []config.InterfaceProtection `json:"protections"`
	}
	const maxConfigSize = 10 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(r.Body, maxConfigSize))
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to read request body")
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
			WriteErrorCtx(w, r, http.StatusBadRequest, "Invalid request body")
			return
		}
		updateFn = func(cfg *config.Config) {
			cfg.Protections = protections
		}
	}

	if s.applyConfigUpdate(w, r, updateFn) {
		WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
	}
}

// handleGetVPN returns the current VPN configuration
// handleGetVPN returns the current VPN configuration
func (s *Server) handleGetVPN(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w, r); cfg != nil {
		HandleGetData(w, cfg.VPN)
	}
}

// handleUpdateVPN updates the VPN configuration
func (s *Server) handleUpdateVPN(w http.ResponseWriter, r *http.Request) {
	var newVPN config.VPNConfig
	if !BindJSONLenient(w, r, &newVPN) {
		return
	}
	if s.applyConfigUpdate(w, r, func(cfg *config.Config) {
		cfg.VPN = &newVPN
	}) {
		SuccessResponse(w)
	}
}

// handleGetQoS returns QoS policies configuration
// handleGetQoS returns QoS policies configuration
func (s *Server) handleGetQoS(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w, r); cfg != nil {
		HandleGetData(w, cfg.QoSPolicies)
	}
}

// handleUpdateQoS updates QoS policies configuration
func (s *Server) handleUpdateQoS(w http.ResponseWriter, r *http.Request) {
	// Accept either array directly or wrapped in { qos_policies: [...] }
	var wrapper struct {
		QoSPolicies []config.QoSPolicy `json:"qos_policies"`
	}
	const maxConfigSize = 10 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(r.Body, maxConfigSize))
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to read request body")
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
			WriteErrorCtx(w, r, http.StatusBadRequest, "Invalid request body")
			return
		}
		updateFn = func(cfg *config.Config) {
			cfg.QoSPolicies = qos
		}
	}

	if s.applyConfigUpdate(w, r, updateFn) {
		WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
	}
}
