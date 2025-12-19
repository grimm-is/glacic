package api

import (
	"encoding/json"
	"net/http"
)

// handleGetTopology returns the discovered network topology (LLDP neighbors)
func (s *Server) handleGetTopology(w http.ResponseWriter, r *http.Request) {
	neighbors, err := s.client.GetTopology()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"neighbors": neighbors,
	})
}

// handleGetNetworkDevices returns all discovered devices on the network
func (s *Server) handleGetNetworkDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.client.GetNetworkDevices()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"devices": devices,
	})
}
