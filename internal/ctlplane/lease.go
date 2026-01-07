package ctlplane

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// ensure Server implements LeaseListener
// Change to use a type assertion check in a test or blank identifier?
// For now, I'll just implement the method.

// LeaseHandler handles DHCP lease events
// We integrate this directly into Server for access to managers

// seenMACs tracks recently seen MACs to prevent notification spam
// We use a sync.Map for thread safety: keys are MAC strings, values are time.Time
var seenMACs sync.Map

// OnLease handles a DHCP lease event
func (s *Server) OnLease(mac string, ip net.IP, hostname string) {
	// 1. Lookup Device
	if s.deviceManager == nil {
		return
	}
	info := s.deviceManager.GetDevice(mac)

	// 2. Handle Known Devices (Linked to Identity)
	if info.Device != nil {
		// Sync Tags to IPSets
		if s.ipsetService != nil && len(info.Device.Tags) > 0 {
			for _, tag := range info.Device.Tags {
				// Ensure IPSet exists (auto-create and persist if needed)
				setName, err := s.EnsureTagIPSet(tag)
				if err != nil {
					log.Printf("[CTL] Failed to ensure IPSet for tag %s: %v", tag, err)
					continue
				}

				if err := s.ipsetService.AddEntry(setName, ip.String()); err != nil {
					// Log debug
					// log.Printf("[CTL] Failed to add IP to tagged IPSet %s: %v", setName, err)
				}
			}
		}
		return
	}

	// 3. Handle New/Unknown Devices
	// Check/Update "Last Seen" to deduplicate notifications
	lastSeen, loaded := seenMACs.LoadOrStore(mac, time.Now())
	if loaded {
		// If seen recently (e.g. < 24 hours), skip notification
		if time.Since(lastSeen.(time.Time)) < 24*time.Hour {
			return
		}
		// Update timestamp
		seenMACs.Store(mac, time.Now())
	}

	// Emit Notification
	notifyTitle := "New Device Found"
	notifyMsg := fmt.Sprintf("A new device (%s) has joined the network.", mac)
	if hostname != "" {
		notifyMsg = fmt.Sprintf("A new device (%s / %s) has joined the network.", hostname, mac)
	}
	if info.Vendor != "" {
		notifyMsg += fmt.Sprintf(" Vendor: %s.", info.Vendor)
	}

	s.Notify(NotifyInfo, notifyTitle, notifyMsg)
}
