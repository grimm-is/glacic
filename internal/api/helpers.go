package api

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"grimm.is/glacic/internal/i18n"
)

// getClientIP extracts the client IP from the request
// Respects X-Forwarded-For and X-Real-IP headers for proxy situations
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (comma-separated list, first is the client)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			ip := strings.TrimSpace(ips[0])
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		if net.ParseIP(xri) != nil {
			return xri
		}
	}

	// Fallback to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// ErrorResponse represents a standard API error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

// WriteError sends a JSON error response
func WriteError(w http.ResponseWriter, code int, message string, details ...string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	resp := ErrorResponse{Error: message}
	if len(details) > 0 {
		resp.Details = details[0]
	}
	json.NewEncoder(w).Encode(resp)
}

// WriteJSON sends a JSON success response
func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}
// WriteErrorCtx sends a localized JSON error response
func WriteErrorCtx(w http.ResponseWriter, r *http.Request, code int, format string, args ...interface{}) {
	p := i18n.GetPrinter(r.Context())
	msg := p.Sprintf(format, args...)
	WriteError(w, code, msg)
}
