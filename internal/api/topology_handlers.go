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
	json.NewEncoder(w).Encode(neighbors)
}

// handleGetNetworkDevices returns all discovered devices on the network
// Query Parameters:
//
//	details=full: Include mDNS/DHCP profiling data (default: summary only)
func (s *Server) handleGetNetworkDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.client.GetNetworkDevices()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter details if not requested
	if r.URL.Query().Get("details") != "full" {
		for i := range devices {
			devices[i].MDNSServices = nil
			devices[i].MDNSTXTRecords = nil
			devices[i].DHCPOptions = nil
			devices[i].DHCPFingerprint = ""
			devices[i].DHCPVendorClass = ""
			devices[i].DHCPClientID = ""
			// Keep Hostname, Vendor, DeviceType, DeviceModel as they are useful summary info
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"devices": devices,
	})
}
