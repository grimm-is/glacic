package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRoutingBaselines checks that endpoints return 405 for unsupported methods.
// This confirms that our refactor (or current implementation) handles methods correctly.
func TestRoutingBaselines(t *testing.T) {
	s := &Server{
		mux: http.NewServeMux(),
		// client: nil (handlers checking s.client will return 503, which is fine, we just want to avoid 404 or 405 inconsistencies)
	}
	s.initRoutes()

	tests := []struct {
		method string
		path   string
		code   int
	}{
		// existing legacy routes might handle 405 manually
		{"DELETE", "/api/config/vpn", http.StatusMethodNotAllowed},
		{"PUT", "/api/auth/login", http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)

		if w.Code != tt.code {
			t.Errorf("%s %s: expected %d, got %d", tt.method, tt.path, tt.code, w.Code)
		}
	}
}
