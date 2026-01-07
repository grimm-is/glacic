package api

import (
	"net/http"

	"grimm.is/glacic/internal/metrics"
)

// ==============================================================================
// Monitoring Handlers
// ==============================================================================

// MonitoringOverview is the combined monitoring data response.
type MonitoringOverview struct {
	Timestamp  int64                              `json:"timestamp"`
	System     *metrics.SystemStats               `json:"system"`
	Interfaces map[string]*metrics.InterfaceStats `json:"interfaces"`
	Policies   map[string]*metrics.PolicyStats    `json:"policies"`
	Services   *metrics.ServiceStats              `json:"services"`
	Conntrack  *metrics.ConntrackStats            `json:"conntrack"`
}

// handleMonitoringOverview returns all monitoring data in one request.
func (s *Server) handleMonitoringOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	overview := MonitoringOverview{
		Timestamp:  s.collector.GetLastUpdate().Unix(),
		System:     s.collector.GetSystemStats(),
		Interfaces: s.collector.GetInterfaceStats(),
		Policies:   s.collector.GetPolicyStats(),
		Services:   s.collector.GetServiceStats(),
		Conntrack:  s.collector.GetConntrackStats(),
	}

	WriteJSON(w, http.StatusOK, overview)
}

// handleMonitoringInterfaces returns interface traffic statistics.
func (s *Server) handleMonitoringInterfaces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	stats := s.collector.GetInterfaceStats()

	// Convert to sorted slice for consistent ordering
	type ifaceWithStats struct {
		*metrics.InterfaceStats
	}
	result := make([]ifaceWithStats, 0, len(stats))
	for _, stat := range stats {
		result = append(result, ifaceWithStats{stat})
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"timestamp":  s.collector.GetLastUpdate().Unix(),
		"interfaces": result,
	})
}

// handleMonitoringPolicies returns firewall policy statistics.
func (s *Server) handleMonitoringPolicies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	stats := s.collector.GetPolicyStats()

	// Convert to slice
	result := make([]*metrics.PolicyStats, 0, len(stats))
	for _, stat := range stats {
		result = append(result, stat)
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"timestamp": s.collector.GetLastUpdate().Unix(),
		"policies":  result,
	})
}

// handleMonitoringServices returns service statistics (DHCP, DNS).
func (s *Server) handleMonitoringServices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	stats := s.collector.GetServiceStats()

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"timestamp": s.collector.GetLastUpdate().Unix(),
		"services":  stats,
	})
}

// handleMonitoringSystem returns system statistics (CPU, memory, load).
func (s *Server) handleMonitoringSystem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	stats := s.collector.GetSystemStats()

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"timestamp": s.collector.GetLastUpdate().Unix(),
		"system":    stats,
	})
}

// handleMonitoringConntrack returns connection tracking statistics.
func (s *Server) handleMonitoringConntrack(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	stats := s.collector.GetConntrackStats()

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"timestamp": s.collector.GetLastUpdate().Unix(),
		"conntrack": stats,
	})
}
