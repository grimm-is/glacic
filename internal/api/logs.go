package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"grimm.is/glacic/internal/ctlplane"
)

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.client == nil {
		http.Error(w, "Control plane not available", http.StatusServiceUnavailable)
		return
	}

	// Parse query parameters
	args := &ctlplane.GetLogsArgs{}

	if source := r.URL.Query().Get("source"); source != "" {
		args.Source = source
	}
	if level := r.URL.Query().Get("level"); level != "" {
		args.Level = level
	}
	if search := r.URL.Query().Get("search"); search != "" {
		args.Search = search
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil {
			args.Limit = n
		}
	}
	if since := r.URL.Query().Get("since"); since != "" {
		args.Since = since
	}
	if until := r.URL.Query().Get("until"); until != "" {
		args.Until = until
	}

	reply, err := s.client.GetLogs(args)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if reply.Error != "" {
		http.Error(w, reply.Error, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(reply.Entries)
}

func (s *Server) handleLogSources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.client == nil {
		http.Error(w, "Control plane not available", http.StatusServiceUnavailable)
		return
	}

	reply, err := s.client.GetLogSources()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(reply.Sources)
}

func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	if s.client == nil {
		http.Error(w, "Control plane not available", http.StatusServiceUnavailable)
		return
	}

	// Parse source filter
	source := r.URL.Query().Get("source")

	// Track the last seen timestamp to avoid sending duplicates
	var lastTimestamp time.Time

	// Send initial batch
	args := &ctlplane.GetLogsArgs{
		Source: source,
		Limit:  1000,
	}
	reply, _ := s.client.GetLogs(args)
	if reply != nil {
		for _, entry := range reply.Entries {
			data, _ := json.Marshal(entry)
			w.Write([]byte("data: " + string(data) + "\n\n"))
			// Track the latest timestamp
			if t, err := time.Parse(time.RFC3339, entry.Timestamp); err == nil {
				if t.After(lastTimestamp) {
					lastTimestamp = t
				}
			}
		}
	}
	flusher.Flush()

	// Keep connection open and poll for new logs
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			args := &ctlplane.GetLogsArgs{
				Source: source,
				Limit:  100,
			}
			// Only fetch logs since last seen timestamp
			if !lastTimestamp.IsZero() {
				args.Since = lastTimestamp.Format(time.RFC3339)
			}
			reply, err := s.client.GetLogs(args)
			if err != nil || reply == nil {
				continue
			}
			for _, entry := range reply.Entries {
				// Skip entries we've already sent
				if t, err := time.Parse(time.RFC3339, entry.Timestamp); err == nil {
					if !t.After(lastTimestamp) {
						continue
					}
					if t.After(lastTimestamp) {
						lastTimestamp = t
					}
				}
				data, _ := json.Marshal(entry)
				w.Write([]byte("data: " + string(data) + "\n\n"))
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleLogStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.client == nil {
		http.Error(w, "Control plane not available", http.StatusServiceUnavailable)
		return
	}

	reply, err := s.client.GetLogStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if reply.Error != "" {
		http.Error(w, reply.Error, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(reply.Stats)
}
