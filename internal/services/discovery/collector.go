package discovery

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

// SeenDevice represents a device observed on the network
type SeenDevice struct {
	MAC       string    `json:"mac"`       // Primary key
	IPs       []string  `json:"ips"`       // All IPs seen for this MAC
	Interface string    `json:"interface"` // Source interface
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Hostname  string    `json:"hostname,omitempty"` // From DHCP or mDNS
	Vendor    string    `json:"vendor,omitempty"`   // OUI lookup
	Alias     string    `json:"alias,omitempty"`    // User-assigned
	HopCount  int       `json:"hop_count"`          // TTL-derived: 0=direct, 1+=behind switch/router
	Flags     []string  `json:"flags,omitempty"`    // "new", "needs_scan", etc.

	// Packet stats
	PacketCount int64 `json:"packet_count"`
}

// PacketEvent represents a packet observation (interface to avoid import cycle)
type PacketEvent struct {
	Timestamp time.Time
	HwAddr    string // MAC address
	SrcIP     string
	InDev     string // Interface
}

// EnrichFunc is called to enrich a device with additional data
type EnrichFunc func(dev *SeenDevice)

// Collector subscribes to packet events and maintains seen devices
type Collector struct {
	mu         sync.RWMutex
	devices    map[string]*SeenDevice // MAC -> Device
	enrichFunc EnrichFunc

	// Enrichment queue
	enrichQueue chan string     // MAC addresses to enrich
	enrichedSet map[string]bool // Track what's been enriched

	// Event channel
	events chan PacketEvent

	ctx    context.Context
	cancel context.CancelFunc
}

// NewCollector creates a new device collector
func NewCollector(enrichFunc EnrichFunc) *Collector {
	ctx, cancel := context.WithCancel(context.Background())
	return &Collector{
		devices:     make(map[string]*SeenDevice),
		enrichFunc:  enrichFunc,
		enrichQueue: make(chan string, 1000),
		enrichedSet: make(map[string]bool),
		events:      make(chan PacketEvent, 1000),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Events returns the channel to send packet events to
func (c *Collector) Events() chan<- PacketEvent {
	return c.events
}

// Start begins processing packet events
func (c *Collector) Start() {
	// Start enrichment worker
	go c.enrichmentWorker()

	// Start event processor
	go func() {
		for {
			select {
			case <-c.ctx.Done():
				return
			case event := <-c.events:
				c.processEvent(event)
			}
		}
	}()

	log.Println("[Discovery] Collector started")
}

// Stop stops the collector
func (c *Collector) Stop() {
	c.cancel()
	log.Println("[Discovery] Collector stopped")
}

// processEvent handles a single packet event
func (c *Collector) processEvent(event PacketEvent) {
	// Skip events without MAC
	if event.HwAddr == "" || event.HwAddr == "00:00:00:00:00:00" {
		return
	}

	// Normalize MAC to lowercase
	mac := strings.ToLower(event.HwAddr)

	c.mu.Lock()
	defer c.mu.Unlock()

	dev, exists := c.devices[mac]
	if !exists {
		// New device discovered
		dev = &SeenDevice{
			MAC:       mac,
			IPs:       []string{},
			Interface: event.InDev,
			FirstSeen: event.Timestamp,
			LastSeen:  event.Timestamp,
			Flags:     []string{"new"},
		}
		c.devices[mac] = dev

		// Queue for enrichment
		c.queueEnrichment(mac)

		log.Printf("[Discovery] New device: %s from %s", mac, event.SrcIP)
	}

	// Update existing device
	dev.LastSeen = event.Timestamp
	dev.PacketCount++

	// Add IP if not already seen
	if event.SrcIP != "" && !containsString(dev.IPs, event.SrcIP) {
		dev.IPs = append(dev.IPs, event.SrcIP)
	}

	// Update interface if different (device may have moved)
	if event.InDev != "" && dev.Interface != event.InDev {
		dev.Interface = event.InDev
	}
}

// queueEnrichment adds a MAC to the enrichment queue
func (c *Collector) queueEnrichment(mac string) {
	if c.enrichedSet[mac] {
		return // Already enriched
	}

	select {
	case c.enrichQueue <- mac:
	default:
		// Queue full, skip
	}
}

// enrichmentWorker processes the enrichment queue
func (c *Collector) enrichmentWorker() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case mac := <-c.enrichQueue:
			c.mu.Lock()
			dev, exists := c.devices[mac]
			if exists && c.enrichFunc != nil {
				c.enrichFunc(dev)
				c.enrichedSet[mac] = true

				// Remove "new" flag after enrichment
				dev.Flags = removeString(dev.Flags, "new")
			}
			c.mu.Unlock()
		}
	}
}

// GetDevices returns a copy of all seen devices
func (c *Collector) GetDevices() []SeenDevice {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]SeenDevice, 0, len(c.devices))
	for _, dev := range c.devices {
		result = append(result, *dev)
	}
	return result
}

// GetDevice returns a specific device by MAC
func (c *Collector) GetDevice(mac string) *SeenDevice {
	c.mu.RLock()
	defer c.mu.RUnlock()

	mac = strings.ToLower(mac)
	if dev, exists := c.devices[mac]; exists {
		copy := *dev
		return &copy
	}
	return nil
}

// UpdateDevice updates device info (e.g., hostname from DHCP)
func (c *Collector) UpdateDevice(mac, hostname, alias string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	mac = strings.ToLower(mac)
	if dev, exists := c.devices[mac]; exists {
		if hostname != "" {
			dev.Hostname = hostname
		}
		if alias != "" {
			dev.Alias = alias
		}
	}
}

// Helper functions
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}
