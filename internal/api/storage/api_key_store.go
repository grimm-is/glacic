package storage

import (
	"time"
)

// Permission represents an API permission scope.
type Permission string

const (
	// Read permissions
	PermReadConfig   Permission = "config:read"
	PermReadUplinks  Permission = "uplinks:read"
	PermReadFirewall Permission = "firewall:read"
	PermReadDHCP     Permission = "dhcp:read"
	PermReadDNS      Permission = "dns:read"
	PermReadLearning Permission = "learning:read"
	PermReadMetrics  Permission = "metrics:read"
	PermReadHealth   Permission = "health:read"
	PermReadLogs     Permission = "logs:read"
	PermReadAudit    Permission = "audit:read"

	// Write permissions
	PermWriteConfig   Permission = "config:write"
	PermWriteUplinks  Permission = "uplinks:write"
	PermWriteFirewall Permission = "firewall:write"
	PermWriteDHCP     Permission = "dhcp:write"
	PermWriteDNS      Permission = "dns:write"
	PermWriteLearning Permission = "learning:write"

	// Admin permissions
	PermAdminKeys   Permission = "admin:keys"   // Manage API keys
	PermAdminSystem Permission = "admin:system" // System operations (restart, etc.)
	PermAdminBackup Permission = "admin:backup" // Backup/restore

	// Wildcard permissions
	PermReadAll  Permission = "read:*"
	PermWriteAll Permission = "write:*"
	PermAdminAll Permission = "admin:*"
	PermAll      Permission = "*"
)

// APIKey represents an API key with its metadata and permissions.
type APIKey struct {
	ID          string       `json:"id"`          // Unique identifier (public)
	Name        string       `json:"name"`        // Human-readable name
	KeyHash     string       `json:"key_hash"`    // SHA-256 hash of the key (stored)
	KeyPrefix   string       `json:"key_prefix"`  // First 8 chars for identification
	Permissions []Permission `json:"permissions"` // Allowed permissions

	// Restrictions
	AllowedIPs   []string `json:"allowed_ips,omitempty"`   // IP allowlist (empty = any)
	AllowedPaths []string `json:"allowed_paths,omitempty"` // Path prefix allowlist
	RateLimit    int      `json:"rate_limit,omitempty"`    // Requests per minute (0 = unlimited)

	// Metadata
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Enabled     bool       `json:"enabled"`
	Description string     `json:"description,omitempty"`
	CreatedBy   string     `json:"created_by,omitempty"` // Key ID that created this key

	// Usage tracking
	UsageCount int64 `json:"usage_count"`
}

// APIKeyStore manages API keys.
type APIKeyStore interface {
	Get(id string) (*APIKey, error)
	GetByHash(hash string) (*APIKey, error)
	List() ([]*APIKey, error)
	Create(key *APIKey) error
	Update(key *APIKey) error
	Delete(id string) error
	UpdateLastUsed(id string) error
}
