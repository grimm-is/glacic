package api

import (
	"fmt"
	"net/http"

	"grimm.is/glacic/internal/logging"
)

// auditMiddleware logs all state-changing operations
func (s *Server) auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only audit mutating methods
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: 200}

		next.ServeHTTP(wrapped, r)

		// Extract user identity
		var identity string
		key := GetAPIKey(r.Context())
		if key != nil {
			identity = fmt.Sprintf("%s (%s)", key.Name, key.ID)
		} else {
			identity = "unknown"
		}

		// Log audit event
		logging.Audit(r.Method, r.URL.Path, map[string]any{
			"user":       identity,
			"ip":         getClientIP(r),
			"status":     wrapped.statusCode,
			"user_agent": r.UserAgent(),
		})
	})
}
