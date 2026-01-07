package api

import (
	"net/http"
	"strings"
	"time"

	"grimm.is/glacic/internal/auth"
)

// --- Session / Auth Handlers ---

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Rate limiting: 5 attempts per minute per IP
	clientIP := getClientIP(r)
	if !s.rateLimiter.Allow(clientIP, 5, time.Minute) {
		s.logger.Warn("Rate limit exceeded for login", "ip", clientIP)
		WriteErrorCtx(w, r, http.StatusTooManyRequests, "Too many login attempts. Please try again later.")
		return
	}

	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if !BindJSON(w, r, &creds) {
		return
	}

	if s.authStore == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Auth not configured")
		return
	}

	sess, err := s.authStore.Authenticate(creds.Username, creds.Password)
	if err != nil {
		s.logger.Warn("Failed login attempt", "username", creds.Username, "ip", clientIP)

		// Record failed attempt for Fail2Ban-style blocking
		// Auto-block after 5 failed attempts in 5 minutes
		if s.security != nil {
			if blockErr := s.security.RecordFailedAttempt(clientIP, "failed_login", 5, 5*time.Minute); blockErr != nil {
				s.logger.Warn("Failed to record attempt", "error", blockErr)
			}
		}

		WriteErrorCtx(w, r, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	// Successful login - log it
	s.logger.Info("Successful login", "username", creds.Username, "ip", clientIP)

	// Reset rate limit on successful login
	s.rateLimiter.Reset(clientIP)

	// Generate CSRF token for the session
	csrfToken, err := s.csrfManager.GenerateToken(sess.Token)
	if err != nil {
		s.logger.Error("Failed to generate CSRF token", "error", err)
		// Continue anyway - CSRF is defense in depth
	}

	// Set session cookie
		auth.SetSessionCookie(w, r, sess)

	// Get user for response
	user, _ := s.authStore.GetUser(creds.Username)

	// Return session info with CSRF token
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated": true,
		"username":      user.Username,
		"role":          user.Role,
		"csrf_token":    csrfToken,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	cookie, err := r.Cookie("session")
	if err == nil && s.authStore != nil {
		// Clean up CSRF token
		s.csrfManager.DeleteToken(cookie.Value)

		// Logout session
		s.authStore.Logout(cookie.Value)
	}

	auth.ClearSessionCookie(w)

	SuccessResponse(w)
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated":  false,
			"setup_required": true,
		})
		return
	}

	// Check if user is authenticated
	cookie, err := r.Cookie("session")
	if err != nil {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated":  false,
			"setup_required": !s.authStore.HasUsers(),
		})
		return
	}

	user, err := s.authStore.ValidateSession(cookie.Value)
	if err != nil {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated":  false,
			"setup_required": !s.authStore.HasUsers(),
		})
		return
	}

	// Get or generate CSRF token
	csrfToken, exists := s.csrfManager.GetToken(cookie.Value)
	if !exists {
		var err error
		csrfToken, err = s.csrfManager.GenerateToken(cookie.Value)
		if err != nil {
			s.logger.Error("Failed to generate CSRF token", "error", err)
		}
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated":  true,
		"username":       user.Username,
		"role":           user.Role,
		"setup_required": false,
		"csrf_token":     csrfToken,
	})
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	needsSetup := s.authStore == nil || !s.authStore.HasUsers()

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"needs_setup": needsSetup,
	})
}

func (s *Server) handleCreateAdmin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Rate limiting: 3 attempts per minute per IP for setup
	clientIP := getClientIP(r)
	if !s.rateLimiter.Allow("setup:"+clientIP, 3, time.Minute) {
		s.logger.Warn("Rate limit exceeded for admin creation", "ip", clientIP)
		WriteErrorCtx(w, r, http.StatusTooManyRequests, "Too many setup attempts. Please try again later.")
		return
	}

	// Critical Section: Prevent race conditions in admin creation
	s.adminCreationMu.Lock()
	defer s.adminCreationMu.Unlock()

	// Only allow if no users exist
	if s.authStore != nil && s.authStore.HasUsers() {
		WriteErrorCtx(w, r, http.StatusForbidden, "Admin already exists")
		return
	}

	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if !BindJSON(w, r, &creds) {
		return
	}

	if creds.Username == "" || creds.Password == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Username and password required")
		return
	}

	// Validate password using new password policy
	policy := auth.DefaultPasswordPolicy()
	if err := auth.ValidatePassword(creds.Password, policy, creds.Username); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// Create auth store if it doesn't exist
	if s.authStore == nil {
		store, err := auth.NewStore("")
		if err != nil {
			WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to initialize auth")
			return
		}
		s.authStore = store
		s.authMw = auth.NewMiddleware(store)
	}

	if err := s.authStore.CreateUser(creds.Username, creds.Password, auth.RoleAdmin); err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.logger.Info("Admin user created", "username", creds.Username, "ip", clientIP)

	// Auto-login the new admin
	session, _ := s.authStore.Authenticate(creds.Username, creds.Password)
	if session != nil {
		auth.SetSessionCookie(w, r, session)

		// Generate CSRF token for the new session
		csrfToken, _ := s.csrfManager.GenerateToken(session.Token)

		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":       true,
			"authenticated": true,
			"username":      creds.Username,
			"csrf_token":    csrfToken,
			"role":          auth.RoleAdmin,
		})
	} else {
		WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
	}
}

// handleGetUsers returns all users
func (s *Server) handleGetUsers(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		WriteJSON(w, http.StatusOK, []interface{}{})
		return
	}
	users := s.authStore.ListUsers()
	WriteJSON(w, http.StatusOK, users)
}

// handleCreateUser creates a new user
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Auth not configured")
		return
	}
	var req struct {
		Username string    `json:"username"`
		Password string    `json:"password"`
		Role     auth.Role `json:"role"`
	}
	if !BindJSON(w, r, &req) {
		return
	}
	if err := s.authStore.CreateUser(req.Username, req.Password, req.Role); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, err.Error())
		return
	}
	SuccessResponse(w)
}

// handleGetUser returns a single user
func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Auth not configured")
		return
	}
	username := strings.TrimPrefix(r.URL.Path, "/api/users/")
	if username == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Username required")
		return
	}

	user, err := s.authStore.GetUser(username)
	if err != nil {
		WriteErrorCtx(w, r, http.StatusNotFound, "User not found")
		return
	}
	// Don't expose password hash
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"username":   user.Username,
		"role":       user.Role,
		"created_at": user.CreatedAt,
		"updated_at": user.UpdatedAt,
	})
}

// handleUpdateUser updates a user
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Auth not configured")
		return
	}
	username := strings.TrimPrefix(r.URL.Path, "/api/users/")
	if username == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Username required")
		return
	}

	var req struct {
		Password string    `json:"password,omitempty"`
		Role     auth.Role `json:"role,omitempty"`
	}
	if !BindJSONCustomErr(w, r, &req, "Invalid request") {
		return
	}

	// Update password if provided
	if req.Password != "" {
		if err := s.authStore.UpdatePassword(username, req.Password); err != nil {
			WriteErrorCtx(w, r, http.StatusBadRequest, err.Error())
			return
		}
	}

	// Update role if provided
	if req.Role != "" {
		if err := s.authStore.UpdateRole(username, req.Role); err != nil {
			WriteErrorCtx(w, r, http.StatusBadRequest, err.Error())
			return
		}
	}

	SuccessResponse(w)
}

// handleDeleteUser deletes a user
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Auth not configured")
		return
	}
	username := strings.TrimPrefix(r.URL.Path, "/api/users/")
	if username == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Username required")
		return
	}

	// Prevent deleting admin user
	if username == "admin" {
		WriteErrorCtx(w, r, http.StatusForbidden, "Cannot delete admin user")
		return
	}
	if err := s.authStore.DeleteUser(username); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, err.Error())
		return
	}
	SuccessResponse(w)
}
