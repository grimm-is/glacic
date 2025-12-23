package api

import (
	"encoding/json"
	"io"
	"net/http"

	"grimm.is/glacic/internal/config"
)

// --- Config CRUD Handlers ---

// handleGetPolicies returns firewall policies
func (s *Server) handleGetPolicies(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.Policies)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.Policies)
	}
}

// handleUpdatePolicies updates firewall policies
func (s *Server) handleUpdatePolicies(w http.ResponseWriter, r *http.Request) {
	var policies []config.Policy
	if err := json.NewDecoder(r.Body).Decode(&policies); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.Config.Policies = policies
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetNAT returns NAT configuration
func (s *Server) handleGetNAT(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.NAT)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.NAT)
	}
}

// handleUpdateNAT updates NAT configuration
func (s *Server) handleUpdateNAT(w http.ResponseWriter, r *http.Request) {
	var nat []config.NATRule
	if err := json.NewDecoder(r.Body).Decode(&nat); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.Config.NAT = nat
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetMarkRules returns mark rules
func (s *Server) handleGetMarkRules(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.MarkRules)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.MarkRules)
	}
}

// handleUpdateMarkRules updates mark rules
func (s *Server) handleUpdateMarkRules(w http.ResponseWriter, r *http.Request) {
	var rules []config.MarkRule
	if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.Config.MarkRules = rules
	if s.client != nil {
		cfg, _ := s.client.GetConfig()
		cfg.MarkRules = rules
		s.client.ApplyConfig(cfg)
		s.client.SaveConfig()
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetUIDRouting returns UID routing rules
func (s *Server) handleGetUIDRouting(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.UIDRouting)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.UIDRouting)
	}
}

// handleUpdateUIDRouting updates UID routing rules
func (s *Server) handleUpdateUIDRouting(w http.ResponseWriter, r *http.Request) {
	var rules []config.UIDRouting
	if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.Config.UIDRouting = rules
	if s.client != nil {
		cfg, _ := s.client.GetConfig()
		cfg.UIDRouting = rules
		s.client.ApplyConfig(cfg)
		s.client.SaveConfig()
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetIPSets returns IPSet configuration
func (s *Server) handleGetIPSets(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.IPSets)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.IPSets)
	}
}

// handleUpdateIPSets updates IPSet configuration
func (s *Server) handleUpdateIPSets(w http.ResponseWriter, r *http.Request) {
	var ipsets []config.IPSet
	if err := json.NewDecoder(r.Body).Decode(&ipsets); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.Config.IPSets = ipsets
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetDHCP returns DHCP server configuration
func (s *Server) handleGetDHCP(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.DHCP)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.DHCP)
	}
}

// handleUpdateDHCP updates DHCP server configuration
func (s *Server) handleUpdateDHCP(w http.ResponseWriter, r *http.Request) {
	var dhcp config.DHCPServer
	if err := json.NewDecoder(r.Body).Decode(&dhcp); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.Config.DHCP = &dhcp
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetDNS returns DNS server configuration
func (s *Server) handleGetDNS(w http.ResponseWriter, r *http.Request) {
	var cfg *config.Config
	var err error
	if s.client != nil {
		cfg, err = s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		cfg = s.Config
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
		if req.Old != nil {
			s.Config.DNSServer = req.Old
		}
		if req.New != nil {
			s.Config.DNS = req.New
		}
	} else {
		// Try legacy raw DNSServer format
		var old config.DNSServer
		if err := json.Unmarshal(body, &old); err == nil {
			s.Config.DNSServer = &old
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
func (s *Server) handleGetRoutes(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.Routes)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.Routes)
	}
}

// handleUpdateRoutes updates static route configuration
func (s *Server) handleUpdateRoutes(w http.ResponseWriter, r *http.Request) {
	var routes []config.Route
	if err := json.NewDecoder(r.Body).Decode(&routes); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.Config.Routes = routes
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetZones returns zone configuration
func (s *Server) handleGetZones(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.Zones)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.Zones)
	}
}

// handleUpdateZones updates zone configuration
func (s *Server) handleUpdateZones(w http.ResponseWriter, r *http.Request) {
	var zones []config.Zone
	if err := json.NewDecoder(r.Body).Decode(&zones); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.Config.Zones = zones
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetProtections returns per-interface protection settings
func (s *Server) handleGetProtections(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.Protections)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.Protections)
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
		s.Config.Protections = wrapper.Protections
	} else {
		// Try parsing as array
		var protections []config.InterfaceProtection
		if err := json.Unmarshal(body, &protections); err != nil {
			WriteError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		s.Config.Protections = protections
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetVPN returns the current VPN configuration
func (s *Server) handleGetVPN(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.VPN)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.VPN)
	}
}

// handleUpdateVPN updates the VPN configuration
func (s *Server) handleUpdateVPN(w http.ResponseWriter, r *http.Request) {
	var newVPN config.VPNConfig
	if err := json.NewDecoder(r.Body).Decode(&newVPN); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	s.Config.VPN = &newVPN
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetQoS returns QoS policies configuration
func (s *Server) handleGetQoS(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.QoSPolicies)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.QoSPolicies)
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
		s.Config.QoSPolicies = wrapper.QoSPolicies
	} else {
		var qos []config.QoSPolicy
		if err := json.Unmarshal(body, &qos); err != nil {
			WriteError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		s.Config.QoSPolicies = qos
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}
