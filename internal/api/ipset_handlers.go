package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// IPSetResponse represents an IPSet in API responses.
type IPSetResponse struct {
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	Source       string    `json:"source"`
	SourceURL    string    `json:"source_url,omitempty"`
	LastUpdate   time.Time `json:"last_update"`
	NextUpdate   time.Time `json:"next_update,omitempty"`
	EntriesCount int       `json:"entries_count"`
	Age          string    `json:"age"`
}

// CacheInfoResponse represents cache information.
type CacheInfoResponse struct {
	CachedLists int    `json:"cached_lists"`
	TotalSize   int64  `json:"total_size"`
	CacheDir    string `json:"cache_dir"`
}

// handleIPSetList handles GET /api/ipsets
func (s *Server) handleIPSetList(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		http.Error(w, "Control Plane client not available", http.StatusServiceUnavailable)
		return
	}

	metadatas, err := s.client.ListIPSets()
	if err != nil {
		s.logger.Error("Failed to list IPSets", "error", err)
		http.Error(w, "Failed to list IPSets", http.StatusInternalServerError)
		return
	}

	// Convert to response format
	responses := make([]IPSetResponse, len(metadatas))
	for i, metadata := range metadatas {
		responses[i] = IPSetResponse{
			Name:         metadata.Name,
			Type:         metadata.Type,
			Source:       metadata.Source,
			SourceURL:    metadata.SourceURL,
			LastUpdate:   metadata.LastUpdate,
			NextUpdate:   metadata.NextUpdate,
			EntriesCount: metadata.EntriesCount,
			Age:          time.Since(metadata.LastUpdate).String(),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ipsets": responses,
		"count":  len(responses),
	})
}

// handleIPSetShow handles GET /api/ipsets/{name}
func (s *Server) handleIPSetShow(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		http.Error(w, "Control Plane client not available", http.StatusServiceUnavailable)
		return
	}

	// Extract name from URL path
	name := r.URL.Path[len("/api/ipsets/"):]
	if name == "" {
		http.Error(w, "IPSet name is required", http.StatusBadRequest)
		return
	}

	metadata, err := s.client.GetIPSet(name)
	if err != nil {
		s.logger.Error("Failed to get IPSet metadata", "error", err)
		http.Error(w, "IPSet not found", http.StatusNotFound)
		return
	}

	response := IPSetResponse{
		Name:         metadata.Name,
		Type:         metadata.Type,
		Source:       metadata.Source,
		SourceURL:    metadata.SourceURL,
		LastUpdate:   metadata.LastUpdate,
		NextUpdate:   metadata.NextUpdate,
		EntriesCount: metadata.EntriesCount,
		Age:          time.Since(metadata.LastUpdate).String(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleIPSetUpdate handles POST /api/ipsets/{name}/update
func (s *Server) handleIPSetUpdate(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		http.Error(w, "Control Plane client not available", http.StatusServiceUnavailable)
		return
	}

	// Extract name from URL path
	path := r.URL.Path
	name := strings.TrimPrefix(path, "/api/ipsets/")
	name = strings.TrimSuffix(name, "/update")

	if name == "" {
		http.Error(w, "IPSet name is required", http.StatusBadRequest)
		return
	}

	if err := s.client.RefreshIPSet(name); err != nil {
		s.logger.Error("Failed to update IPSet", "error", err)
		http.Error(w, fmt.Sprintf("Failed to update IPSet: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": fmt.Sprintf("IPSet %s updated successfully", name),
		"updated": true,
	})
}

// handleIPSetRefresh handles POST /api/ipsets/{name}/refresh
func (s *Server) handleIPSetRefresh(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		http.Error(w, "Control Plane client not available", http.StatusServiceUnavailable)
		return
	}

	// Extract name from URL path
	path := r.URL.Path
	name := strings.TrimPrefix(path, "/api/ipsets/")
	name = strings.TrimSuffix(name, "/refresh")

	if name == "" {
		http.Error(w, "IPSet name is required", http.StatusBadRequest)
		return
	}

	if err := s.client.RefreshIPSet(name); err != nil {
		s.logger.Error("Failed to refresh IPSet", "error", err)
		http.Error(w, fmt.Sprintf("Failed to refresh IPSet: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":   fmt.Sprintf("IPSet %s refreshed successfully", name),
		"refreshed": true,
	})
}

// handleIPSetCacheInfo handles GET /api/ipsets/cache/info
func (s *Server) handleIPSetCacheInfo(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		http.Error(w, "Control Plane client not available", http.StatusServiceUnavailable)
		return
	}

	info, err := s.client.GetIPSetCacheInfo()
	if err != nil {
		s.logger.Error("Failed to get cache info", "error", err)
		http.Error(w, "Failed to get cache info", http.StatusInternalServerError)
		return
	}

	// If info is nil, return empty response or specific message
	if info == nil {
		http.Error(w, "Cache info not available", http.StatusNotFound)
		return
	}

	// Helper to safely get int64
	getInt64 := func(v interface{}) int64 {
		switch i := v.(type) {
		case int:
			return int64(i)
		case int64:
			return i
		case float64:
			return int64(i)
		default:
			return 0
		}
	}
	getInt := func(v interface{}) int {
		return int(getInt64(v))
	}

	response := CacheInfoResponse{
		CachedLists: getInt(info["cached_lists"]),
		TotalSize:   getInt64(info["total_size"]),
		CacheDir:    fmt.Sprintf("%v", info["cache_dir"]),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleIPSetCacheClear handles DELETE /api/ipsets/cache
func (s *Server) handleIPSetCacheClear(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		http.Error(w, "Control Plane client not available", http.StatusServiceUnavailable)
		return
	}

	if err := s.client.ClearIPSetCache(); err != nil {
		s.logger.Error("Failed to clear cache", "error", err)
		http.Error(w, "Failed to clear cache", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Cache cleared successfully",
		"cleared": true,
	})
}
