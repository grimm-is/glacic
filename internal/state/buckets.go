package state

import (
	"encoding/json"
	"fmt"
	"grimm.is/glacic/internal/clock"
	"net"
	"time"
)

// Standard bucket names
const (
	BucketDHCPLeases   = "dhcp_leases"
	BucketDHCPReserved = "dhcp_reserved"
	BucketDNSCache     = "dns_cache"
	BucketDNSBlocked   = "dns_blocked"
	BucketSessions     = "sessions"
	BucketConntrack    = "conntrack"
	BucketMetrics      = "metrics"
	BucketConfig       = "config"        // Runtime config overrides
	BucketLearnedRules = "learned_rules" // Auto-learned firewall rules
	BucketStats        = "stats"         // Traffic statistics
)

// DHCPLease represents a DHCP lease in the state store.
type DHCPLease struct {
	MAC        string    `json:"mac"`
	IP         string    `json:"ip"`
	Hostname   string    `json:"hostname"`
	Interface  string    `json:"interface"`
	LeaseStart time.Time `json:"lease_start"`
	LeaseEnd   time.Time `json:"lease_end"`
	ClientID   string    `json:"client_id,omitempty"`
	VendorID   string    `json:"vendor_id,omitempty"`
}

// DHCPBucket provides typed access to DHCP leases.
type DHCPBucket struct {
	store  Store
	bucket string
}

// NewDHCPBucket creates a new DHCP bucket accessor.
func NewDHCPBucket(store Store) (*DHCPBucket, error) {
	// Ensure bucket exists
	if err := store.CreateBucket(BucketDHCPLeases); err != nil && err != ErrBucketExists {
		return nil, err
	}
	return &DHCPBucket{store: store, bucket: BucketDHCPLeases}, nil
}

// Get retrieves a lease by MAC address.
func (b *DHCPBucket) Get(mac string) (*DHCPLease, error) {
	var lease DHCPLease
	if err := b.store.GetJSON(b.bucket, normalizeMAC(mac), &lease); err != nil {
		return nil, err
	}
	return &lease, nil
}

// Set stores a lease.
func (b *DHCPBucket) Set(lease *DHCPLease) error {
	ttl := time.Until(lease.LeaseEnd)
	if ttl <= 0 {
		// Lease already expired, don't store
		return nil
	}
	return b.store.SetJSONWithTTL(b.bucket, normalizeMAC(lease.MAC), lease, ttl)
}

// Delete removes a lease.
func (b *DHCPBucket) Delete(mac string) error {
	return b.store.Delete(b.bucket, normalizeMAC(mac))
}

// List returns all active leases.
func (b *DHCPBucket) List() ([]*DHCPLease, error) {
	data, err := b.store.List(b.bucket)
	if err != nil {
		return nil, err
	}

	leases := make([]*DHCPLease, 0, len(data))
	for _, v := range data {
		var lease DHCPLease
		if err := unmarshalJSON(v, &lease); err != nil {
			continue
		}
		leases = append(leases, &lease)
	}
	return leases, nil
}

// GetByIP finds a lease by IP address.
func (b *DHCPBucket) GetByIP(ip string) (*DHCPLease, error) {
	leases, err := b.List()
	if err != nil {
		return nil, err
	}
	for _, lease := range leases {
		if lease.IP == ip {
			return lease, nil
		}
	}
	return nil, ErrNotFound
}

// normalizeMAC normalizes a MAC address to lowercase with colons.
func normalizeMAC(mac string) string {
	hw, err := net.ParseMAC(mac)
	if err != nil {
		return mac
	}
	return hw.String()
}

// DNSCacheEntry represents a cached DNS record.
type DNSCacheEntry struct {
	Name      string    `json:"name"`
	Type      uint16    `json:"type"`
	Class     uint16    `json:"class"`
	TTL       uint32    `json:"ttl"`
	Data      []byte    `json:"data"`
	CachedAt  time.Time `json:"cached_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// DNSBucket provides typed access to DNS cache.
type DNSBucket struct {
	store  Store
	bucket string
}

// NewDNSBucket creates a new DNS bucket accessor.
func NewDNSBucket(store Store) (*DNSBucket, error) {
	if err := store.CreateBucket(BucketDNSCache); err != nil && err != ErrBucketExists {
		return nil, err
	}
	return &DNSBucket{store: store, bucket: BucketDNSCache}, nil
}

// cacheKey generates a unique key for a DNS record.
func (b *DNSBucket) cacheKey(name string, qtype uint16) string {
	return fmt.Sprintf("%s:%d", name, qtype)
}

// Get retrieves a cached DNS entry.
func (b *DNSBucket) Get(name string, qtype uint16) (*DNSCacheEntry, error) {
	var entry DNSCacheEntry
	if err := b.store.GetJSON(b.bucket, b.cacheKey(name, qtype), &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// Set stores a DNS cache entry.
func (b *DNSBucket) Set(entry *DNSCacheEntry) error {
	ttl := time.Until(entry.ExpiresAt)
	if ttl <= 0 {
		return nil
	}
	return b.store.SetJSONWithTTL(b.bucket, b.cacheKey(entry.Name, entry.Type), entry, ttl)
}

// Delete removes a cached entry.
func (b *DNSBucket) Delete(name string, qtype uint16) error {
	return b.store.Delete(b.bucket, b.cacheKey(name, qtype))
}

// Session represents an authenticated user session.
type Session struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
}

// SessionBucket provides typed access to sessions.
type SessionBucket struct {
	store  Store
	bucket string
}

// NewSessionBucket creates a new session bucket accessor.
func NewSessionBucket(store Store) (*SessionBucket, error) {
	if err := store.CreateBucket(BucketSessions); err != nil && err != ErrBucketExists {
		return nil, err
	}
	return &SessionBucket{store: store, bucket: BucketSessions}, nil
}

// Get retrieves a session by ID.
func (b *SessionBucket) Get(id string) (*Session, error) {
	var session Session
	if err := b.store.GetJSON(b.bucket, id, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

// Set stores a session.
func (b *SessionBucket) Set(session *Session) error {
	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		return nil
	}
	return b.store.SetJSONWithTTL(b.bucket, session.ID, session, ttl)
}

// Delete removes a session.
func (b *SessionBucket) Delete(id string) error {
	return b.store.Delete(b.bucket, id)
}

// ListByUser returns all sessions for a user.
func (b *SessionBucket) ListByUser(username string) ([]*Session, error) {
	data, err := b.store.List(b.bucket)
	if err != nil {
		return nil, err
	}

	var sessions []*Session
	for _, v := range data {
		var session Session
		if err := unmarshalJSON(v, &session); err != nil {
			continue
		}
		if session.Username == username {
			sessions = append(sessions, &session)
		}
	}
	return sessions, nil
}

// DeleteByUser removes all sessions for a user.
func (b *SessionBucket) DeleteByUser(username string) error {
	sessions, err := b.ListByUser(username)
	if err != nil {
		return err
	}
	for _, s := range sessions {
		b.Delete(s.ID)
	}
	return nil
}

// unmarshalJSON is a helper to unmarshal JSON bytes.
func unmarshalJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// =============================================================================
// Runtime Config Bucket - stores live config overrides not yet persisted to HCL
// =============================================================================

// RuntimeConfigBucket provides typed access to runtime configuration overrides.
// This stores the "live" version of config that may differ from the HCL file.
type RuntimeConfigBucket struct {
	store  Store
	bucket string
}

// NewRuntimeConfigBucket creates a new runtime config bucket accessor.
func NewRuntimeConfigBucket(store Store) (*RuntimeConfigBucket, error) {
	if err := store.CreateBucket(BucketConfig); err != nil && err != ErrBucketExists {
		return nil, err
	}
	return &RuntimeConfigBucket{store: store, bucket: BucketConfig}, nil
}

// Config keys for runtime overrides
const (
	ConfigKeyFullConfig    = "full_config"    // Complete merged config (HCL + runtime)
	ConfigKeyLearnedRules  = "learned_rules"  // Rules learned from traffic
	ConfigKeyTempPolicies  = "temp_policies"  // Temporary policies not yet saved
	ConfigKeyActiveVPNs    = "active_vpns"    // Currently active VPN connections
	ConfigKeyDHCPOverrides = "dhcp_overrides" // Runtime DHCP scope changes
)

// GetFullConfig retrieves the full merged configuration.
func (b *RuntimeConfigBucket) GetFullConfig(v interface{}) error {
	return b.store.GetJSON(b.bucket, ConfigKeyFullConfig, v)
}

// SetFullConfig stores the full merged configuration.
func (b *RuntimeConfigBucket) SetFullConfig(v interface{}) error {
	return b.store.SetJSON(b.bucket, ConfigKeyFullConfig, v)
}

// GetValue retrieves a runtime config value by key.
func (b *RuntimeConfigBucket) GetValue(key string, v interface{}) error {
	return b.store.GetJSON(b.bucket, key, v)
}

// SetValue stores a runtime config value.
func (b *RuntimeConfigBucket) SetValue(key string, v interface{}) error {
	return b.store.SetJSON(b.bucket, key, v)
}

// DeleteValue removes a runtime config value.
func (b *RuntimeConfigBucket) DeleteValue(key string) error {
	return b.store.Delete(b.bucket, key)
}

// =============================================================================
// Learned Rules Bucket - auto-learned firewall rules from traffic analysis
// =============================================================================

// LearnedRule represents an automatically learned firewall rule.
type LearnedRule struct {
	ID          string    `json:"id"`
	SrcZone     string    `json:"src_zone"`
	DestZone    string    `json:"dest_zone"`
	Protocol    string    `json:"protocol"`
	DestPort    int       `json:"dest_port,omitempty"`
	SrcIP       string    `json:"src_ip,omitempty"`
	DestIP      string    `json:"dest_ip,omitempty"`
	Action      string    `json:"action"` // "accept", "drop", "pending"
	HitCount    uint64    `json:"hit_count"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	Approved    bool      `json:"approved"`    // User approved this rule
	ApprovedAt  time.Time `json:"approved_at"` // When approved
	ApprovedBy  string    `json:"approved_by"` // Who approved
	Permanent   bool      `json:"permanent"`   // Saved to HCL config
	Description string    `json:"description"`
}

// LearnedRulesBucket provides typed access to learned firewall rules.
type LearnedRulesBucket struct {
	store  Store
	bucket string
}

// NewLearnedRulesBucket creates a new learned rules bucket accessor.
func NewLearnedRulesBucket(store Store) (*LearnedRulesBucket, error) {
	if err := store.CreateBucket(BucketLearnedRules); err != nil && err != ErrBucketExists {
		return nil, err
	}
	return &LearnedRulesBucket{store: store, bucket: BucketLearnedRules}, nil
}

// Get retrieves a learned rule by ID.
func (b *LearnedRulesBucket) Get(id string) (*LearnedRule, error) {
	var rule LearnedRule
	if err := b.store.GetJSON(b.bucket, id, &rule); err != nil {
		return nil, err
	}
	return &rule, nil
}

// Set stores a learned rule.
func (b *LearnedRulesBucket) Set(rule *LearnedRule) error {
	return b.store.SetJSON(b.bucket, rule.ID, rule)
}

// Delete removes a learned rule.
func (b *LearnedRulesBucket) Delete(id string) error {
	return b.store.Delete(b.bucket, id)
}

// List returns all learned rules.
func (b *LearnedRulesBucket) List() ([]*LearnedRule, error) {
	data, err := b.store.List(b.bucket)
	if err != nil {
		return nil, err
	}

	rules := make([]*LearnedRule, 0, len(data))
	for _, v := range data {
		var rule LearnedRule
		if err := unmarshalJSON(v, &rule); err != nil {
			continue
		}
		rules = append(rules, &rule)
	}
	return rules, nil
}

// ListPending returns rules awaiting approval.
func (b *LearnedRulesBucket) ListPending() ([]*LearnedRule, error) {
	rules, err := b.List()
	if err != nil {
		return nil, err
	}

	var pending []*LearnedRule
	for _, r := range rules {
		if !r.Approved && !r.Permanent {
			pending = append(pending, r)
		}
	}
	return pending, nil
}

// ListApproved returns approved but not yet permanent rules.
func (b *LearnedRulesBucket) ListApproved() ([]*LearnedRule, error) {
	rules, err := b.List()
	if err != nil {
		return nil, err
	}

	var approved []*LearnedRule
	for _, r := range rules {
		if r.Approved && !r.Permanent {
			approved = append(approved, r)
		}
	}
	return approved, nil
}

// Approve marks a rule as approved.
func (b *LearnedRulesBucket) Approve(id, approvedBy string) error {
	rule, err := b.Get(id)
	if err != nil {
		return err
	}
	rule.Approved = true
	rule.ApprovedAt = clock.Now()
	rule.ApprovedBy = approvedBy
	return b.Set(rule)
}

// MarkPermanent marks a rule as saved to HCL config.
func (b *LearnedRulesBucket) MarkPermanent(id string) error {
	rule, err := b.Get(id)
	if err != nil {
		return err
	}
	rule.Permanent = true
	return b.Set(rule)
}

// IncrementHitCount updates the hit count for a rule.
func (b *LearnedRulesBucket) IncrementHitCount(id string) error {
	rule, err := b.Get(id)
	if err != nil {
		return err
	}
	rule.HitCount++
	rule.LastSeen = clock.Now()
	return b.Set(rule)
}
