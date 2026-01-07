package auth

import (
	"context"
	"net/http"
	"strings"
)

// ContextKey is used for storing user in request context
type ContextKey string

const UserContextKey ContextKey = "user"

// Middleware provides HTTP middleware for authentication
type Middleware struct {
	store AuthStore
}

// NewMiddleware creates a new auth middleware
func NewMiddleware(store AuthStore) *Middleware {
	return &Middleware{store: store}
}

// RequireAuth wraps a handler to require authentication
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := m.getUserFromRequest(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Add user to context
		ctx := context.WithValue(r.Context(), UserContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole wraps a handler to require a specific role level
func (m *Middleware) RequireRole(action string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := m.getUserFromRequest(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if !user.Role.CanAccess(action) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		ctx := context.WithValue(r.Context(), UserContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalAuth adds user to context if authenticated, but doesn't require it
func (m *Middleware) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := m.getUserFromRequest(r)
		if err == nil {
			ctx := context.WithValue(r.Context(), UserContextKey, user)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// getUserFromRequest extracts and validates user from request
func (m *Middleware) getUserFromRequest(r *http.Request) (*User, error) {
	// Try cookie first
	cookie, err := r.Cookie("session")
	if err == nil {
		return m.store.ValidateSession(cookie.Value)
	}

	// Try Authorization header (for API clients)
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		return m.store.ValidateSession(token)
	}

	return nil, err
}

// GetUserFromContext retrieves user from request context
func GetUserFromContext(ctx context.Context) *User {
	user, _ := ctx.Value(UserContextKey).(*User)
	return user
}

// SetSessionCookie sets the session cookie on a response
// Mitigation: OWASP A01:2021-Broken Access Control (CSRF prevention)
func SetSessionCookie(w http.ResponseWriter, r *http.Request, session *Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    session.Token,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode, // Strict = no cross-site requests
		Secure:   strings.HasPrefix(r.Referer(), "https://") || r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
}

// ClearSessionCookie clears the session cookie
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}
