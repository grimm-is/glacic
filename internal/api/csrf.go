package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"grimm.is/glacic/internal/clock"
)

// CSRFManager manages CSRF tokens for session protection
type CSRFManager struct {
	tokens map[string]*csrfToken // sessionID -> token
	mu     sync.RWMutex
}

type csrfToken struct {
	value     string
	createdAt time.Time
}

// NewCSRFManager creates a new CSRF token manager
func NewCSRFManager() *CSRFManager {
	mgr := &CSRFManager{
		tokens: make(map[string]*csrfToken),
	}

	// Start cleanup goroutine for expired tokens
	go mgr.cleanupExpiredTokens()

	return mgr
}

// GenerateToken generates a new CSRF token for a session
func (m *CSRFManager) GenerateToken(sessionID string) (string, error) {
	// Generate 32 random bytes
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate CSRF token: %w", err)
	}

	token := hex.EncodeToString(tokenBytes)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.tokens[sessionID] = &csrfToken{
		value:     token,
		createdAt: clock.Now(),
	}

	return token, nil
}

// GetToken retrieves an existing CSRF token for a session
func (m *CSRFManager) GetToken(sessionID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	storedToken, exists := m.tokens[sessionID]
	if !exists {
		return "", false
	}

	// Check if token has expired (24 hours)
	if time.Since(storedToken.createdAt) > 24*time.Hour {
		return "", false
	}

	return storedToken.value, true
}

// ValidateToken validates a CSRF token for a session
func (m *CSRFManager) ValidateToken(sessionID, token string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	storedToken, exists := m.tokens[sessionID]
	if !exists {
		return false
	}

	// Check if token has expired (24 hours)
	if time.Since(storedToken.createdAt) > 24*time.Hour {
		return false
	}

	return storedToken.value == token
}

// DeleteToken removes a CSRF token (e.g., on logout)
func (m *CSRFManager) DeleteToken(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, sessionID)
}

// cleanupExpiredTokens periodically removes expired tokens
func (m *CSRFManager) cleanupExpiredTokens() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.Lock()
		for sessionID, token := range m.tokens {
			if time.Since(token.createdAt) > 24*time.Hour {
				delete(m.tokens, sessionID)
			}
		}
		m.mu.Unlock()
	}
}

// CSRFMiddleware creates middleware that validates CSRF tokens on state-changing requests
// Mitigation: OWASP A01:2021-Broken Access Control (CSRF prevention)
func CSRFMiddleware(manager *CSRFManager, authStore interface{}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only check CSRF on state-changing methods
			if r.Method != http.MethodPost && r.Method != http.MethodPut &&
				r.Method != http.MethodDelete && r.Method != http.MethodPatch {
				next.ServeHTTP(w, r)
				return
			}

			// Skip CSRF check for public endpoints (login, setup)
			publicEndpoints := []string{
				"/api/auth/login",
				"/api/setup/create-admin",
			}

			for _, endpoint := range publicEndpoints {
				if r.URL.Path == endpoint {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Skip CSRF check for API key authenticated requests
			// API keys require explicit Authorization header, so can't be CSRF'd
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" && (strings.HasPrefix(authHeader, "Bearer gfw_") ||
				strings.HasPrefix(authHeader, "ApiKey ")) {
				next.ServeHTTP(w, r)
				return
			}

			// Also check X-API-Key header
			if r.Header.Get("X-API-Key") != "" {
				next.ServeHTTP(w, r)
				return
			}

			// Extract session ID from cookie
			sessionCookie, err := r.Cookie("session")
			if err != nil {
				http.Error(w, "CSRF validation failed: no session", http.StatusForbidden)
				return
			}

			// Get CSRF token from header
			csrfToken := r.Header.Get("X-CSRF-Token")
			if csrfToken == "" {
				http.Error(w, "CSRF validation failed: missing token", http.StatusForbidden)
				return
			}

			// Validate token
			if !manager.ValidateToken(sessionCookie.Value, csrfToken) {
				http.Error(w, "CSRF validation failed: invalid token", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
