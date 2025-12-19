package device

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"grimm.is/glacic/internal/state"
)

const (
	BucketIdentities = "device_identities"
	BucketLinks      = "device_mac_links"
)

// DeviceIdentity represents a logical device (e.g. "Bobby's iPad")
type DeviceIdentity struct {
	ID        string    `json:"id"`    // UUID
	Alias     string    `json:"alias"` // User-defined name
	Owner     string    `json:"owner"` // User-defined owner
	Type      string    `json:"type"`  // phone, laptop, iot, etc.
	Tags      []string  `json:"tags"`  // User-defined tags (e.g. "kids", "iot")
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DeviceInfo is the enriched view returned to consumers
type DeviceInfo struct {
	MAC    string          `json:"mac"`
	Vendor string          `json:"vendor"`
	Device *DeviceIdentity `json:"device,omitempty"` // Linked identity if any
}

// Manager handles device identity and OUI lookups
type Manager struct {
	store     state.Store
	mu        sync.RWMutex
	ouiLookup func(string) string

	// In-memory cache
	identities map[string]*DeviceIdentity // ID -> Identity
	macLinks   map[string]string          // MAC -> IdentityID
}

// NewManager creates a new DeviceManager
func NewManager(store state.Store, ouiLookup func(string) string) (*Manager, error) {
	m := &Manager{
		store:      store,
		ouiLookup:  ouiLookup,
		identities: make(map[string]*DeviceIdentity),
		macLinks:   make(map[string]string),
	}

	// Initialize buckets
	if err := store.CreateBucket(BucketIdentities); err != nil && err != state.ErrBucketExists {
		return nil, fmt.Errorf("failed to create identities bucket: %w", err)
	}
	if err := store.CreateBucket(BucketLinks); err != nil && err != state.ErrBucketExists {
		return nil, fmt.Errorf("failed to create links bucket: %w", err)
	}

	// Load state
	if err := m.loadState(); err != nil {
		return nil, fmt.Errorf("failed to load device state: %w", err)
	}

	return m, nil
}

func (m *Manager) loadState() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Load Identities
	idList, err := m.store.List(BucketIdentities)
	if err != nil {
		return err
	}
	for _, data := range idList {
		var id DeviceIdentity
		if err := json.Unmarshal(data, &id); err != nil {
			log.Printf("[Device] Warning: failed to unmarshal identity: %v", err)
			continue
		}
		m.identities[id.ID] = &id
	}

	// Load Links
	linkList, err := m.store.List(BucketLinks)
	if err != nil {
		return err
	}
	for mac, data := range linkList {
		var idStr string
		if err := json.Unmarshal(data, &idStr); err != nil {
			log.Printf("[Device] Warning: failed to unmarshal link for %s: %v", mac, err)
			continue
		}
		m.macLinks[mac] = idStr
	}

	log.Printf("[Device] Loaded %d identities and %d MAC links", len(m.identities), len(m.macLinks))
	return nil
}

// GetDevice returns enriched device info for a MAC address
func (m *Manager) GetDevice(mac string) DeviceInfo {
	vendor := ""
	if m.ouiLookup != nil {
		vendor = m.ouiLookup(mac)
	}

	info := DeviceInfo{
		MAC:    mac,
		Vendor: vendor,
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if idStr, ok := m.macLinks[mac]; ok {
		if identity, ok := m.identities[idStr]; ok {
			// Return valid linked identity
			// We return a copy to avoid external mutation affecting cache
			idCopy := *identity
			info.Device = &idCopy
		}
	}

	return info
}

// GetIdentity returns a specific identity by ID
func (m *Manager) GetIdentity(id string) *DeviceIdentity {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if identity, ok := m.identities[id]; ok {
		cpy := *identity
		return &cpy
	}
	return nil
}

// CreateIdentity creates a new device identity
func (m *Manager) CreateIdentity(alias, owner, devType string) (*DeviceIdentity, error) {
	id := &DeviceIdentity{
		ID:        uuid.New().String(),
		Alias:     alias,
		Owner:     owner,
		Type:      devType,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := m.saveIdentity(id); err != nil {
		return nil, err
	}

	return id, nil
}

// UpdateIdentity updates an existing identity
func (m *Manager) UpdateIdentity(idStr string, alias, owner, devType *string, tags []string) (*DeviceIdentity, error) {
	m.mu.Lock()
	identity, ok := m.identities[idStr]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("identity not found: %s", idStr)
	}

	// Create copy for modification
	updated := *identity
	m.mu.Unlock()

	if alias != nil {
		updated.Alias = *alias
	}
	if owner != nil {
		updated.Owner = *owner
	}
	if devType != nil {
		updated.Type = *devType
	}
	if tags != nil {
		updated.Tags = tags
	}
	updated.UpdatedAt = time.Now()

	// Persist
	if err := m.store.SetJSON(BucketIdentities, updated.ID, updated); err != nil {
		return nil, err
	}

	// Update cache
	m.mu.Lock()
	m.identities[updated.ID] = &updated
	m.mu.Unlock()

	return &updated, nil
}

// LinkMAC links a MAC address to an identity
func (m *Manager) LinkMAC(mac, identityID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.identities[identityID]; !ok {
		return fmt.Errorf("identity not found: %s", identityID)
	}

	// Persist
	// We store just the ID string as value
	if err := m.store.SetJSON(BucketLinks, mac, identityID); err != nil {
		return err
	}

	m.macLinks[mac] = identityID
	return nil
}

// UnlinkMAC removes a MAC link
func (m *Manager) UnlinkMAC(mac string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.store.Delete(BucketLinks, mac); err != nil {
		return err
	}

	delete(m.macLinks, mac)
	return nil
}

// Internal save helper (Locks map)
func (m *Manager) saveIdentity(id *DeviceIdentity) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.store.SetJSON(BucketIdentities, id.ID, id); err != nil {
		return err
	}
	m.identities[id.ID] = id
	return nil
}
