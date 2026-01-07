package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"grimm.is/glacic/internal/clock"
	"net/http"
	"strings"
	"sync"
	"time"

	"grimm.is/glacic/internal/api/storage"
)

// APIKeyManager handles API key operations.
type APIKeyManager struct {
	store storage.APIKeyStore
}

// NewAPIKeyManager creates a new API key manager.
func NewAPIKeyManager(store storage.APIKeyStore) *APIKeyManager {
	return &APIKeyManager{store: store}
}

// GenerateKey creates a new API key with the given permissions.
// Returns the full key (only shown once) and the stored APIKey metadata.
func (m *APIKeyManager) GenerateKey(name string, permissions []storage.Permission, opts ...KeyOption) (string, *storage.APIKey, error) {
	// Generate random key: 32 bytes = 64 hex chars
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return "", nil, fmt.Errorf("failed to generate key: %w", err)
	}

	// Format: prefix_randomhex (e.g., "gfw_a1b2c3d4...")
	fullKey := fmt.Sprintf("gfw_%s", hex.EncodeToString(keyBytes))

	// Hash for storage
	hash := sha256.Sum256([]byte(fullKey))
	keyHash := hex.EncodeToString(hash[:])

	// Generate ID
	idBytes := make([]byte, 8)
	rand.Read(idBytes)
	id := hex.EncodeToString(idBytes)

	key := &storage.APIKey{
		ID:          id,
		Name:        name,
		KeyHash:     keyHash,
		KeyPrefix:   fullKey[:12], // "gfw_" + first 8 chars
		Permissions: permissions,
		CreatedAt:   clock.Now(),
		Enabled:     true,
	}

	// Apply options
	for _, opt := range opts {
		opt(key)
	}

	if err := m.store.Create(key); err != nil {
		return "", nil, err
	}

	return fullKey, key, nil
}

// KeyOption is a functional option for key creation.
type KeyOption func(*storage.APIKey)

// WithExpiry sets an expiration time for the key.
func WithExpiry(t time.Time) KeyOption {
	return func(k *storage.APIKey) {
		k.ExpiresAt = &t
	}
}

// WithExpiryDuration sets an expiration duration from now.
func WithExpiryDuration(d time.Duration) KeyOption {
	return func(k *storage.APIKey) {
		t := clock.Now().Add(d)
		k.ExpiresAt = &t
	}
}

// WithAllowedIPs restricts the key to specific IPs.
func WithAllowedIPs(ips []string) KeyOption {
	return func(k *storage.APIKey) {
		k.AllowedIPs = ips
	}
}

// WithAllowedPaths restricts the key to specific path prefixes.
func WithAllowedPaths(paths []string) KeyOption {
	return func(k *storage.APIKey) {
		k.AllowedPaths = paths
	}
}

// WithRateLimit sets requests per minute limit.
func WithRateLimit(rpm int) KeyOption {
	return func(k *storage.APIKey) {
		k.RateLimit = rpm
	}
}

// WithDescription adds a description.
func WithDescription(desc string) KeyOption {
	return func(k *storage.APIKey) {
		k.Description = desc
	}
}

// WithCreatedBy records which key created this key.
func WithCreatedBy(keyID string) KeyOption {
	return func(k *storage.APIKey) {
		k.CreatedBy = keyID
	}
}

// ValidateKey checks if a key is valid and returns the APIKey if so.
func (m *APIKeyManager) ValidateKey(fullKey string) (*storage.APIKey, error) {
	// Hash the provided key
	hash := sha256.Sum256([]byte(fullKey))
	keyHash := hex.EncodeToString(hash[:])

	// Look up by hash
	key, err := m.store.GetByHash(keyHash)
	if err != nil {
		return nil, fmt.Errorf("invalid API key")
	}

	// Check if enabled
	if !key.Enabled {
		return nil, fmt.Errorf("API key is disabled")
	}

	// Check expiry
	if key.ExpiresAt != nil && clock.Now().After(*key.ExpiresAt) {
		return nil, fmt.Errorf("API key has expired")
	}

	// Update last used
	m.store.UpdateLastUsed(key.ID)

	return key, nil
}

// AuthMiddleware creates HTTP middleware for API key authentication.
type AuthMiddleware struct {
	manager      *APIKeyManager
	skipPaths    []string // Paths that don't require auth
	rateLimiters map[string]*rateLimiter
	security     *SecurityManager // For fail2ban-style blocking
	mu           sync.RWMutex
}

// NewAuthMiddleware creates a new auth middleware.
func NewAuthMiddleware(manager *APIKeyManager) *AuthMiddleware {
	return &AuthMiddleware{
		manager:      manager,
		skipPaths:    []string{"/health", "/ready", "/metrics"},
		rateLimiters: make(map[string]*rateLimiter),
	}
}

// SkipPaths adds paths that don't require authentication.
func (m *AuthMiddleware) SkipPaths(paths ...string) *AuthMiddleware {
	m.skipPaths = append(m.skipPaths, paths...)
	return m
}

// SetSecurityManager injects a SecurityManager for fail2ban-style blocking.
func (m *AuthMiddleware) SetSecurityManager(sm *SecurityManager) *AuthMiddleware {
	m.security = sm
	return m
}

// Wrap wraps an http.Handler with authentication.
func (m *AuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if path should skip auth
		for _, skip := range m.skipPaths {
			if strings.HasPrefix(r.URL.Path, skip) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Extract API key from header
		authHeader := r.Header.Get("Authorization")
		var apiKey string

		if strings.HasPrefix(authHeader, "Bearer ") {
			apiKey = strings.TrimPrefix(authHeader, "Bearer ")
		} else if strings.HasPrefix(authHeader, "ApiKey ") {
			apiKey = strings.TrimPrefix(authHeader, "ApiKey ")
		} else {
			// Also check X-API-Key header
			apiKey = r.Header.Get("X-API-Key")
		}

		if apiKey == "" {
			writeAuthError(w, http.StatusUnauthorized, "API key required")
			return
		}

		// Validate key
		key, err := m.manager.ValidateKey(apiKey)
		if err != nil {
			// Get client IP for blocking
			clientIP := getClientIP(r)

			// Record failed attempt for Fail2Ban-style blocking
			// Auto-block after 10 failed API key attempts in 5 minutes
			if m.security != nil {
				if blockErr := m.security.RecordFailedAttempt(clientIP, "invalid_api_key", 10, 5*time.Minute); blockErr != nil {
					// Log but don't fail the request
				}
			}

			writeAuthError(w, http.StatusUnauthorized, err.Error())
			return
		}

		// Check IP restriction
		clientIP := getClientIP(r)
		if !key.IsIPAllowed(clientIP) {
			writeAuthError(w, http.StatusForbidden, "IP not allowed")
			return
		}

		// Check path restriction
		if !key.IsPathAllowed(r.URL.Path) {
			writeAuthError(w, http.StatusForbidden, "path not allowed")
			return
		}

		// Check rate limit
		if key.RateLimit > 0 {
			if !m.checkRateLimit(key.ID, key.RateLimit) {
				writeAuthError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
		}

		// Store key in context for handlers to use
		ctx := r.Context()
		ctx = WithAPIKey(ctx, key)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}

// RequirePermission creates middleware that requires a specific permission.
func RequirePermission(perm storage.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := GetAPIKey(r.Context())
			if key == nil {
				writeAuthError(w, http.StatusUnauthorized, "authentication required")
				return
			}

			if !key.HasPermission(perm) {
				writeAuthError(w, http.StatusForbidden, fmt.Sprintf("permission denied: %s required", perm))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAnyPermission creates middleware that requires any of the specified permissions.
func RequireAnyPermission(perms ...storage.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := GetAPIKey(r.Context())
			if key == nil {
				writeAuthError(w, http.StatusUnauthorized, "authentication required")
				return
			}

			if !key.HasAnyPermission(perms...) {
				writeAuthError(w, http.StatusForbidden, "permission denied")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Simple rate limiter
type rateLimiter struct {
	tokens    int
	lastReset time.Time
	mu        sync.Mutex
}

func (m *AuthMiddleware) checkRateLimit(keyID string, limit int) bool {
	m.mu.Lock()
	rl, ok := m.rateLimiters[keyID]
	if !ok {
		rl = &rateLimiter{tokens: limit, lastReset: clock.Now()}
		m.rateLimiters[keyID] = rl
	}
	m.mu.Unlock()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Reset tokens every minute
	if time.Since(rl.lastReset) >= time.Minute {
		rl.tokens = limit
		rl.lastReset = clock.Now()
	}

	if rl.tokens <= 0 {
		return false
	}

	rl.tokens--
	return true
}

func writeAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// Context key for API key
type contextKey string

const apiKeyContextKey contextKey = "apiKey"

// WithAPIKey adds an API key to the context.
func WithAPIKey(ctx context.Context, key *storage.APIKey) context.Context {
	return context.WithValue(ctx, apiKeyContextKey, key)
}

// GetAPIKey retrieves the API key from the context.
func GetAPIKey(ctx context.Context) *storage.APIKey {
	key, _ := ctx.Value(apiKeyContextKey).(*storage.APIKey)
	return key
}

// --- API Key Management Handlers ---

// APIKeyHandlers provides HTTP handlers for API key management.
type APIKeyHandlers struct {
	manager *APIKeyManager
}

// NewAPIKeyHandlers creates new API key handlers.
func NewAPIKeyHandlers(manager *APIKeyManager) *APIKeyHandlers {
	return &APIKeyHandlers{manager: manager}
}

// RegisterRoutes registers API key management routes.
func (h *APIKeyHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/keys", h.ListKeys)
	mux.HandleFunc("POST /api/keys", h.CreateKey)
	mux.HandleFunc("GET /api/keys/{id}", h.GetKey)
	mux.HandleFunc("DELETE /api/keys/{id}", h.DeleteKey)
	mux.HandleFunc("POST /api/keys/{id}/disable", h.DisableKey)
	mux.HandleFunc("POST /api/keys/{id}/enable", h.EnableKey)
	mux.HandleFunc("POST /api/keys/{id}/rotate", h.RotateKey)
}

// CreateKeyRequest represents a request to create an API key.
type CreateKeyRequest struct {
	Name         string               `json:"name"`
	Permissions  []storage.Permission `json:"permissions"`
	Description  string               `json:"description,omitempty"`
	ExpiresIn    string               `json:"expires_in,omitempty"` // Duration string (e.g., "24h", "30d")
	AllowedIPs   []string             `json:"allowed_ips,omitempty"`
	AllowedPaths []string             `json:"allowed_paths,omitempty"`
	RateLimit    int                  `json:"rate_limit,omitempty"`
}

// CreateKeyResponse is returned when creating a key.
type CreateKeyResponse struct {
	Key     string          `json:"key"`      // Full key (only shown once!)
	KeyInfo *storage.APIKey `json:"key_info"` // Metadata (without the actual key)
	Warning string          `json:"warning"`  // Reminder to save the key
}

// ListKeys returns all API keys (without the actual key values).
func (h *APIKeyHandlers) ListKeys(w http.ResponseWriter, r *http.Request) {
	// Check permission
	key := GetAPIKey(r.Context())
	if key == nil || !key.HasPermission(storage.PermAdminKeys) {
		writeAuthError(w, http.StatusForbidden, "admin:keys permission required")
		return
	}

	keys, err := h.manager.store.List()
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	WriteJSON(w, http.StatusOK, keys)
}

// CreateKey creates a new API key.
func (h *APIKeyHandlers) CreateKey(w http.ResponseWriter, r *http.Request) {
	// Check permission
	callerKey := GetAPIKey(r.Context())
	if callerKey == nil || !callerKey.HasPermission(storage.PermAdminKeys) {
		writeAuthError(w, http.StatusForbidden, "admin:keys permission required")
		return
	}

	var req CreateKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Name == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	if len(req.Permissions) == 0 {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one permission is required"})
		return
	}

	// Validate that caller can only grant permissions they have
	// (unless they have full admin)
	if !callerKey.HasPermission(storage.PermAll) {
		for _, perm := range req.Permissions {
			if !callerKey.HasPermission(perm) {
				WriteJSON(w, http.StatusForbidden, map[string]string{
					"error": fmt.Sprintf("cannot grant permission you don't have: %s", perm),
				})
				return
			}
		}
	}

	// Build options
	var opts []KeyOption
	opts = append(opts, WithCreatedBy(callerKey.ID))

	if req.Description != "" {
		opts = append(opts, WithDescription(req.Description))
	}

	if req.ExpiresIn != "" {
		duration, err := parseDuration(req.ExpiresIn)
		if err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid expires_in: %v", err)})
			return
		}
		opts = append(opts, WithExpiryDuration(duration))
	}

	if len(req.AllowedIPs) > 0 {
		opts = append(opts, WithAllowedIPs(req.AllowedIPs))
	}

	if len(req.AllowedPaths) > 0 {
		opts = append(opts, WithAllowedPaths(req.AllowedPaths))
	}

	if req.RateLimit > 0 {
		opts = append(opts, WithRateLimit(req.RateLimit))
	}

	// Generate the key
	fullKey, keyInfo, err := h.manager.GenerateKey(req.Name, req.Permissions, opts...)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	WriteJSON(w, http.StatusCreated, CreateKeyResponse{
		Key:     fullKey,
		KeyInfo: keyInfo,
		Warning: "Save this key now! It will not be shown again.",
	})
}

// GetKey returns a specific API key's metadata.
func (h *APIKeyHandlers) GetKey(w http.ResponseWriter, r *http.Request) {
	callerKey := GetAPIKey(r.Context())
	if callerKey == nil || !callerKey.HasPermission(storage.PermAdminKeys) {
		writeAuthError(w, http.StatusForbidden, "admin:keys permission required")
		return
	}

	id := r.PathValue("id")
	key, err := h.manager.store.Get(id)
	if err != nil {
		WriteJSON(w, http.StatusNotFound, map[string]string{"error": "key not found"})
		return
	}

	WriteJSON(w, http.StatusOK, key)
}

// DeleteKey deletes an API key.
func (h *APIKeyHandlers) DeleteKey(w http.ResponseWriter, r *http.Request) {
	callerKey := GetAPIKey(r.Context())
	if callerKey == nil || !callerKey.HasPermission(storage.PermAdminKeys) {
		writeAuthError(w, http.StatusForbidden, "admin:keys permission required")
		return
	}

	id := r.PathValue("id")

	// Prevent deleting your own key
	if id == callerKey.ID {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot delete your own key"})
		return
	}

	if err := h.manager.store.Delete(id); err != nil {
		WriteJSON(w, http.StatusNotFound, map[string]string{"error": "key not found"})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]string{"message": "key deleted"})
}

// DisableKey disables an API key.
func (h *APIKeyHandlers) DisableKey(w http.ResponseWriter, r *http.Request) {
	callerKey := GetAPIKey(r.Context())
	if callerKey == nil || !callerKey.HasPermission(storage.PermAdminKeys) {
		writeAuthError(w, http.StatusForbidden, "admin:keys permission required")
		return
	}

	id := r.PathValue("id")

	key, err := h.manager.store.Get(id)
	if err != nil {
		WriteJSON(w, http.StatusNotFound, map[string]string{"error": "key not found"})
		return
	}

	key.Enabled = false
	if err := h.manager.store.Update(key); err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]string{"message": "key disabled"})
}

// EnableKey enables an API key.
func (h *APIKeyHandlers) EnableKey(w http.ResponseWriter, r *http.Request) {
	callerKey := GetAPIKey(r.Context())
	if callerKey == nil || !callerKey.HasPermission(storage.PermAdminKeys) {
		writeAuthError(w, http.StatusForbidden, "admin:keys permission required")
		return
	}

	id := r.PathValue("id")

	key, err := h.manager.store.Get(id)
	if err != nil {
		WriteJSON(w, http.StatusNotFound, map[string]string{"error": "key not found"})
		return
	}

	key.Enabled = true
	if err := h.manager.store.Update(key); err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]string{"message": "key enabled"})
}

// RotateKey generates a new key value while keeping the same permissions.
func (h *APIKeyHandlers) RotateKey(w http.ResponseWriter, r *http.Request) {
	callerKey := GetAPIKey(r.Context())
	if callerKey == nil || !callerKey.HasPermission(storage.PermAdminKeys) {
		writeAuthError(w, http.StatusForbidden, "admin:keys permission required")
		return
	}

	id := r.PathValue("id")

	oldKey, err := h.manager.store.Get(id)
	if err != nil {
		WriteJSON(w, http.StatusNotFound, map[string]string{"error": "key not found"})
		return
	}

	// Generate new key with same settings
	opts := []KeyOption{
		WithCreatedBy(callerKey.ID),
	}
	if oldKey.Description != "" {
		opts = append(opts, WithDescription(oldKey.Description))
	}
	if oldKey.ExpiresAt != nil {
		opts = append(opts, WithExpiry(*oldKey.ExpiresAt))
	}
	if len(oldKey.AllowedIPs) > 0 {
		opts = append(opts, WithAllowedIPs(oldKey.AllowedIPs))
	}
	if len(oldKey.AllowedPaths) > 0 {
		opts = append(opts, WithAllowedPaths(oldKey.AllowedPaths))
	}
	if oldKey.RateLimit > 0 {
		opts = append(opts, WithRateLimit(oldKey.RateLimit))
	}

	// Delete old key
	h.manager.store.Delete(id)

	// Create new key
	fullKey, keyInfo, err := h.manager.GenerateKey(oldKey.Name, oldKey.Permissions, opts...)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	WriteJSON(w, http.StatusOK, CreateKeyResponse{
		Key:     fullKey,
		KeyInfo: keyInfo,
		Warning: "Save this new key now! The old key is no longer valid.",
	})
}

// parseDuration parses duration strings like "24h", "7d", "30d"
func parseDuration(s string) (time.Duration, error) {
	// Handle days specially
	if strings.HasSuffix(s, "d") {
		days := strings.TrimSuffix(s, "d")
		var d int
		if _, err := fmt.Sscanf(days, "%d", &d); err != nil {
			return 0, err
		}
		return time.Duration(d) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// --- Predefined Permission Sets ---

// ReadOnlyPermissions returns permissions for read-only access.
func ReadOnlyPermissions() []storage.Permission {
	return []storage.Permission{storage.PermReadAll}
}

// UplinkControlPermissions returns permissions for uplink management.
func UplinkControlPermissions() []storage.Permission {
	return []storage.Permission{
		storage.PermReadUplinks,
		storage.PermWriteUplinks,
		storage.PermReadHealth,
	}
}

// FullAdminPermissions returns all permissions.
func FullAdminPermissions() []storage.Permission {
	return []storage.Permission{storage.PermAll}
}

// MonitoringPermissions returns permissions for monitoring.
func MonitoringPermissions() []storage.Permission {
	return []storage.Permission{
		storage.PermReadMetrics,
		storage.PermReadHealth,
		storage.PermReadLogs,
	}
}

// AddKey adds an existing key to the manager (e.g. from config).
func (m *APIKeyManager) AddKey(key *storage.APIKey) error {
	// Ensure key hash is set if missing BUT key hash usually computed from full key.
	// If config provides "Key" (plaintext), we must hash it.
	// But APIKey struct stores KeyHash.
	// If config provides plaintext "Key", we need to hash it here?
	// Config struct has "Key" string.
	// We need a method "ImportKey(name, plaintext, perms...)"
	return m.store.Create(key)
}

// ImportKey imports a key with a known plaintext value.
func (m *APIKeyManager) ImportKey(name string, plaintextKey string, permissions []storage.Permission, opts ...KeyOption) error {
	// Hash the key
	hash := sha256.Sum256([]byte(plaintextKey))
	keyHash := hex.EncodeToString(hash[:])

	// Generate ID if deterministic based on key? Or random?
	// Consistent ID across restarts is better for static config.
	// Use hash of key as ID?
	id := keyHash[:16]

	// Compute key prefix (up to 12 chars, or full key if shorter)
	keyPrefix := plaintextKey
	if len(plaintextKey) > 12 {
		keyPrefix = plaintextKey[:12]
	}

	key := &storage.APIKey{
		ID:          id,
		Name:        name,
		KeyHash:     keyHash,
		KeyPrefix:   keyPrefix,
		Permissions: permissions,
		CreatedAt:   clock.Now(),
		Enabled:     true,
		CreatedBy:   "config",
	}

	for _, opt := range opts {
		opt(key)
	}

	return m.store.Create(key)
}
