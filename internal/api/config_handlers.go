package api

import (
	"encoding/json"
	"io"
	"net/http"

	"grimm.is/glacic/internal/config"
)

// --- Config CRUD Handlers ---

// handleGetPolicies returns firewall policies
// handleGetPolicies returns firewall policies
func (s *Server) handleGetPolicies(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.Policies)
	}
}

// handleUpdatePolicies updates firewall policies
// handleUpdatePolicies updates firewall policies
func (s *Server) handleUpdatePolicies(w http.ResponseWriter, r *http.Request) {
	var policies []config.Policy
	if !BindJSON(w, r, &policies) {
		return
	}
	s.configMu.Lock()
	s.Config.Policies = policies
	s.configMu.Unlock()
	SuccessResponse(w)
}

// handleGetNAT returns NAT configuration
// handleGetNAT returns NAT configuration
func (s *Server) handleGetNAT(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.NAT)
	}
}

// handleUpdateNAT updates NAT configuration
// handleUpdateNAT updates NAT configuration
func (s *Server) handleUpdateNAT(w http.ResponseWriter, r *http.Request) {
	var nat []config.NATRule
	if !BindJSON(w, r, &nat) {
		return
	}
	s.configMu.Lock()
	s.Config.NAT = nat
	s.configMu.Unlock()
	SuccessResponse(w)
}

// handleGetMarkRules returns mark rules
// handleGetMarkRules returns mark rules
func (s *Server) handleGetMarkRules(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.MarkRules)
	}
}

// handleUpdateMarkRules updates mark rules
// handleUpdateMarkRules updates mark rules
func (s *Server) handleUpdateMarkRules(w http.ResponseWriter, r *http.Request) {
	var rules []config.MarkRule
	if !BindJSON(w, r, &rules) {
		return
	}
	s.configMu.Lock()
	s.Config.MarkRules = rules
	s.configMu.Unlock()
	if s.client != nil {
		if cfg := s.GetConfigSnapshot(w); cfg != nil {
			cfg.MarkRules = rules
			s.client.ApplyConfig(cfg)
			s.client.SaveConfig()
		}
	}
	SuccessResponse(w)
}

// handleGetUIDRouting returns UID routing rules
// handleGetUIDRouting returns UID routing rules
func (s *Server) handleGetUIDRouting(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.UIDRouting)
	}
}

// handleUpdateUIDRouting updates UID routing rules
// handleUpdateUIDRouting updates UID routing rules
func (s *Server) handleUpdateUIDRouting(w http.ResponseWriter, r *http.Request) {
	var rules []config.UIDRouting
	if !BindJSON(w, r, &rules) {
		return
	}
	s.configMu.Lock()
	s.Config.UIDRouting = rules
	s.configMu.Unlock()
	if s.client != nil {
		if cfg := s.GetConfigSnapshot(w); cfg != nil {
			cfg.UIDRouting = rules
			s.client.ApplyConfig(cfg)
			s.client.SaveConfig()
		}
	}
	SuccessResponse(w)
}

// handleGetIPSets returns IPSet configuration
// handleGetIPSets returns IPSet configuration
func (s *Server) handleGetIPSets(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.IPSets)
	}
}

// handleUpdateIPSets updates IPSet configuration
// handleUpdateIPSets updates IPSet configuration
func (s *Server) handleUpdateIPSets(w http.ResponseWriter, r *http.Request) {
	var ipsets []config.IPSet
	if !BindJSON(w, r, &ipsets) {
		return
	}
	s.configMu.Lock()
	s.Config.IPSets = ipsets
	s.configMu.Unlock()
	SuccessResponse(w)
}

// handleGetDHCP returns DHCP server configuration
// handleGetDHCP returns DHCP server configuration
func (s *Server) handleGetDHCP(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.DHCP)
	}
}

// handleUpdateDHCP updates DHCP server configuration
// handleUpdateDHCP updates DHCP server configuration
func (s *Server) handleUpdateDHCP(w http.ResponseWriter, r *http.Request) {
	var dhcp config.DHCPServer
	if !BindJSON(w, r, &dhcp) {
		return
	}
	s.configMu.Lock()
	s.Config.DHCP = &dhcp
	s.configMu.Unlock()
	SuccessResponse(w)
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

	if err := json.Unmarshal(body, &req); err == nil && (req.Old != nil || req.New != nil) {
		s.configMu.Lock()
		if req.Old != nil {
			s.Config.DNSServer = req.Old
		}
		if req.New != nil {
			s.Config.DNS = req.New
		}
		s.configMu.Unlock()
	} else {
		// Try legacy raw DNSServer format
		var old config.DNSServer
		if err := json.Unmarshal(body, &old); err == nil {
			s.configMu.Lock()
			s.Config.DNSServer = &old
			s.configMu.Unlock()
		} else {
			WriteError(w, http.StatusBadRequest, "Invalid DNS configuration format")
			return
		}
	}

	// Run post-load migrations (e.g., DNS config migration)
	config.ApplyPostLoadMigrations(s.Config)

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetRoutes returns static route configuration
// handleGetRoutes returns static route configuration
func (s *Server) handleGetRoutes(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.Routes)
	}
}

// handleUpdateRoutes updates static route configuration
// handleUpdateRoutes updates static route configuration
func (s *Server) handleUpdateRoutes(w http.ResponseWriter, r *http.Request) {
	var routes []config.Route
	if !BindJSON(w, r, &routes) {
		return
	}
	s.configMu.Lock()
	s.Config.Routes = routes
	s.configMu.Unlock()
	SuccessResponse(w)
}

// handleGetZones returns zone configuration
// handleGetZones returns zone configuration
func (s *Server) handleGetZones(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.Zones)
	}
}

// handleUpdateZones updates zone configuration
// handleUpdateZones updates zone configuration
func (s *Server) handleUpdateZones(w http.ResponseWriter, r *http.Request) {
	var zones []config.Zone
	if !BindJSON(w, r, &zones) {
		return
	}
	s.configMu.Lock()
	s.Config.Zones = zones
	s.configMu.Unlock()
	SuccessResponse(w)
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
	body, _ := io.ReadAll(r.Body)
	// Try parsing as wrapper
	if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.Protections != nil {
		s.configMu.Lock()
		s.Config.Protections = wrapper.Protections
		s.configMu.Unlock()
	} else {
		// Try parsing as array
		var protections []config.InterfaceProtection
		if err := json.Unmarshal(body, &protections); err != nil {
			WriteError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		s.configMu.Lock()
		s.Config.Protections = protections
		s.configMu.Unlock()
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetVPN returns the current VPN configuration
// handleGetVPN returns the current VPN configuration
func (s *Server) handleGetVPN(w http.ResponseWriter, r *http.Request) {
	if cfg := s.GetConfigSnapshot(w); cfg != nil {
		HandleGetData(w, cfg.VPN)
	}
}

// handleUpdateVPN updates the VPN configuration
// handleUpdateVPN updates the VPN configuration
func (s *Server) handleUpdateVPN(w http.ResponseWriter, r *http.Request) {
	var newVPN config.VPNConfig
	// Note: previous code used "Invalid JSON" instead of "Invalid request body", defaulting to standard
	if !BindJSON(w, r, &newVPN) {
		return
	}
	s.configMu.Lock()
	s.Config.VPN = &newVPN
	s.configMu.Unlock()
	SuccessResponse(w)
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
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.QoSPolicies != nil {
		s.configMu.Lock()
		s.Config.QoSPolicies = wrapper.QoSPolicies
		s.configMu.Unlock()
	} else {
		var qos []config.QoSPolicy
		if err := json.Unmarshal(body, &qos); err != nil {
			WriteError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		s.configMu.Lock()
		s.Config.QoSPolicies = qos
		s.configMu.Unlock()
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}
