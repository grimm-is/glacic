// Package device provides unified device identity and discovery integration.
package device

import (
	"sync"
	"time"

	"grimm.is/glacic/internal/services/discovery"
	"grimm.is/glacic/internal/state"
)

// UnifiedLookup implements the api.DeviceLookup interface by merging
// user-defined DeviceIdentity with auto-discovered SeenDevice info.
// It maintains a reverse IP→MAC index for efficient lookups.
type UnifiedLookup struct {
	manager   *Manager
	discovery *discovery.Collector
	leases    *state.DHCPBucket

	// Reverse index: IP → MAC (built on demand)
	ipIndex     map[string]string
	ipIndexTime time.Time
	mu          sync.RWMutex
}

// NewUnifiedLookup creates a unified device lookup adapter.
// All parameters are optional - nil values disable that source.
func NewUnifiedLookup(mgr *Manager, disc *discovery.Collector, leases *state.DHCPBucket) *UnifiedLookup {
	return &UnifiedLookup{
		manager:   mgr,
		discovery: disc,
		leases:    leases,
		ipIndex:   make(map[string]string),
	}
}

// FindByIP implements api.DeviceLookup.
// It returns the best available name and match type for an IP address.
// Priority: identity (user-defined) > hostname (DHCP) > vendor (OUI) > reverse (DNS)
func (u *UnifiedLookup) FindByIP(ip string) (name string, matchType string, found bool) {
	mac := u.lookupMAC(ip)
	if mac == "" {
		return "", "", false
	}

	// 1. Check user-defined DeviceIdentity
	if u.manager != nil {
		info := u.manager.GetDevice(mac)
		if info.Device != nil && info.Device.Alias != "" {
			return info.Device.Alias, "identity", true
		}
		// OUI vendor as fallback
		if info.Vendor != "" {
			// Don't return vendor alone - continue checking for hostname
		}
	}

	// 2. Check discovered hostname (DHCP/mDNS)
	if u.discovery != nil {
		if seen := u.discovery.GetDevice(mac); seen != nil {
			if seen.Hostname != "" {
				return seen.Hostname, "hostname", true
			}
			if seen.Alias != "" {
				return seen.Alias, "hostname", true
			}
			if seen.Vendor != "" {
				return seen.Vendor, "vendor", true
			}
		}
	}

	// 3. Check DHCP lease hostname
	if u.leases != nil {
		if lease, err := u.leases.GetByIP(ip); err == nil && lease != nil {
			if lease.Hostname != "" {
				return lease.Hostname, "hostname", true
			}
		}
	}

	// 4. Final fallback - check manager for vendor only
	if u.manager != nil {
		info := u.manager.GetDevice(mac)
		if info.Vendor != "" {
			return info.Vendor, "vendor", true
		}
	}

	return "", "", false
}

// lookupMAC finds the MAC address for an IP by checking various sources.
func (u *UnifiedLookup) lookupMAC(ip string) string {
	// Check cache first (refreshed every 30s)
	u.mu.RLock()
	if mac, ok := u.ipIndex[ip]; ok && time.Since(u.ipIndexTime) < 30*time.Second {
		u.mu.RUnlock()
		return mac
	}
	u.mu.RUnlock()

	// Rebuild index
	u.rebuildIPIndex()

	u.mu.RLock()
	mac := u.ipIndex[ip]
	u.mu.RUnlock()
	return mac
}

// rebuildIPIndex builds the IP→MAC reverse index from all sources.
func (u *UnifiedLookup) rebuildIPIndex() {
	u.mu.Lock()
	defer u.mu.Unlock()

	// Skip if recently rebuilt
	if time.Since(u.ipIndexTime) < 5*time.Second {
		return
	}

	u.ipIndex = make(map[string]string)

	// 1. From discovery collector
	if u.discovery != nil {
		for _, dev := range u.discovery.GetDevices() {
			for _, ip := range dev.IPs {
				u.ipIndex[ip] = dev.MAC
			}
		}
	}

	// 2. From DHCP leases
	if u.leases != nil {
		if leases, err := u.leases.List(); err == nil {
			for _, lease := range leases {
				u.ipIndex[lease.IP] = lease.MAC
			}
		}
	}

	u.ipIndexTime = time.Now()
}

// InvalidateCache forces a rebuild of the IP index on next lookup.
func (u *UnifiedLookup) InvalidateCache() {
	u.mu.Lock()
	u.ipIndexTime = time.Time{}
	u.mu.Unlock()
}
