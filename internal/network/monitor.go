//go:build linux
// +build linux

package network

import (
	"context"
	"log"
	"net"
	"sync"

	"github.com/vishvananda/netlink"
)

// InterfaceChange represents a change to an interface's IP configuration.
type InterfaceChange struct {
	Interface string
	Type      string // "add", "del"
	Address   net.IPNet
	Gateway   net.IP // For route changes
}

// InterfaceMonitor watches for IP address changes on interfaces.
type InterfaceMonitor struct {
	interfaces map[string]bool // Interfaces to monitor
	callbacks  []func(InterfaceChange)
	mu         sync.RWMutex
	cancel     context.CancelFunc
	running    bool
}

// NewInterfaceMonitor creates a new interface monitor.
func NewInterfaceMonitor() *InterfaceMonitor {
	return &InterfaceMonitor{
		interfaces: make(map[string]bool),
	}
}

// AddInterface adds an interface to monitor.
func (m *InterfaceMonitor) AddInterface(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.interfaces[name] = true
}

// RemoveInterface removes an interface from monitoring.
func (m *InterfaceMonitor) RemoveInterface(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.interfaces, name)
}

// OnChange registers a callback for interface changes.
func (m *InterfaceMonitor) OnChange(callback func(InterfaceChange)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbacks = append(m.callbacks, callback)
}

// Start begins monitoring for interface changes.
func (m *InterfaceMonitor) Start() error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}
	m.running = true
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	// Subscribe to address updates
	addrUpdates := make(chan netlink.AddrUpdate)
	if err := netlink.AddrSubscribe(addrUpdates, ctx.Done()); err != nil {
		return err
	}

	// Subscribe to route updates (for gateway changes)
	routeUpdates := make(chan netlink.RouteUpdate)
	if err := netlink.RouteSubscribe(routeUpdates, ctx.Done()); err != nil {
		// Route subscription is optional
		log.Printf("[Monitor] Warning: could not subscribe to route updates: %v", err)
	}

	go m.processUpdates(ctx, addrUpdates, routeUpdates)

	log.Printf("[Monitor] Started interface monitoring")
	return nil
}

// Stop stops monitoring.
func (m *InterfaceMonitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancel != nil {
		m.cancel()
	}
	m.running = false
	log.Printf("[Monitor] Stopped interface monitoring")
}

func (m *InterfaceMonitor) processUpdates(ctx context.Context, addrUpdates chan netlink.AddrUpdate, routeUpdates chan netlink.RouteUpdate) {
	for {
		select {
		case <-ctx.Done():
			return

		case update, ok := <-addrUpdates:
			if !ok {
				return
			}
			m.handleAddrUpdate(update)

		case update, ok := <-routeUpdates:
			if !ok {
				continue // Route subscription might not be available
			}
			m.handleRouteUpdate(update)
		}
	}
}

func (m *InterfaceMonitor) handleAddrUpdate(update netlink.AddrUpdate) {
	// Get interface name
	link, err := netlink.LinkByIndex(update.LinkIndex)
	if err != nil {
		return
	}
	ifaceName := link.Attrs().Name

	// Check if we're monitoring this interface
	m.mu.RLock()
	monitoring := m.interfaces[ifaceName]
	callbacks := m.callbacks
	m.mu.RUnlock()

	if !monitoring {
		return
	}

	changeType := "add"
	if !update.NewAddr {
		changeType = "del"
	}

	change := InterfaceChange{
		Interface: ifaceName,
		Type:      changeType,
		Address:   update.LinkAddress,
	}

	log.Printf("[Monitor] Interface %s: %s address %s", ifaceName, changeType, update.LinkAddress)

	// Notify callbacks
	for _, cb := range callbacks {
		cb(change)
	}
}

func (m *InterfaceMonitor) handleRouteUpdate(update netlink.RouteUpdate) {
	// Only care about default route changes
	if update.Route.Dst != nil {
		return // Not a default route
	}

	// Get interface name
	link, err := netlink.LinkByIndex(update.Route.LinkIndex)
	if err != nil {
		return
	}
	ifaceName := link.Attrs().Name

	// Check if we're monitoring this interface
	m.mu.RLock()
	monitoring := m.interfaces[ifaceName]
	callbacks := m.callbacks
	m.mu.RUnlock()

	if !monitoring {
		return
	}

	changeType := "add"
	if update.Type == 3 { // RTM_DELROUTE
		changeType = "del"
	}

	change := InterfaceChange{
		Interface: ifaceName,
		Type:      changeType,
		Gateway:   update.Route.Gw,
	}

	log.Printf("[Monitor] Interface %s: %s default route via %s", ifaceName, changeType, update.Route.Gw)

	// Notify callbacks
	for _, cb := range callbacks {
		cb(change)
	}
}

// GetCurrentAddresses returns current IP addresses for a monitored interface.
func (m *InterfaceMonitor) GetCurrentAddresses(ifaceName string) ([]net.IPNet, error) {
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return nil, err
	}

	addrs, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		return nil, err
	}

	var result []net.IPNet
	for _, addr := range addrs {
		if addr.IPNet != nil {
			result = append(result, *addr.IPNet)
		}
	}

	return result, nil
}

// GetDefaultGateway returns the default gateway for an interface.
func (m *InterfaceMonitor) GetDefaultGateway(ifaceName string) (net.IP, error) {
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return nil, err
	}

	routes, err := netlink.RouteList(link, netlink.FAMILY_V4)
	if err != nil {
		return nil, err
	}

	for _, route := range routes {
		if route.Dst == nil && route.Gw != nil {
			return route.Gw, nil
		}
	}

	return nil, nil
}
