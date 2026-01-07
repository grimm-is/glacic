package discovery

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"encoding/json"
	"grimm.is/glacic/internal/logging"
	"os"
	"os/exec"
)

// SeenDevice represents a device observed on the network
type SeenDevice struct {
	MAC       string    `json:"mac"`       // Primary key
	IPs       []string  `json:"ips"`       // All IPs seen for this MAC
	Interface string    `json:"interface"` // Source interface name
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Hostname  string    `json:"hostname,omitempty"` // From DHCP or mDNS
	Vendor    string    `json:"vendor,omitempty"`   // OUI lookup
	Alias     string    `json:"alias,omitempty"`    // User-assigned
	HopCount  int       `json:"hop_count"`          // TTL-derived: 0=direct, 1+=behind switch/router
	Flags     []string  `json:"flags,omitempty"`    // "new", "gateway", "needs_scan", etc.
	IsGateway bool      `json:"is_gateway"`         // True if this MAC is a known gateway/router

	// Packet stats
	PacketCount int64 `json:"packet_count"`

	// mDNS profiling - collected from multicast DNS announcements
	MDNSServices   []string          `json:"mdns_services,omitempty"`  // Service types: ["_googlecast._tcp", "_airplay._tcp"]
	MDNSHostname   string            `json:"mdns_hostname,omitempty"`  // Announced hostname
	MDNSTXTRecords map[string]string `json:"mdns_txt,omitempty"`       // TXT record key-values

	// DHCP profiling - collected from DHCP requests
	DHCPFingerprint string           `json:"dhcp_fingerprint,omitempty"`  // Option 55: "1,3,6,15,31,33..."
	DHCPVendorClass string           `json:"dhcp_vendor_class,omitempty"` // Option 60: "android-dhcp-13"
	DHCPHostname    string           `json:"dhcp_hostname,omitempty"`     // Option 12
	DHCPClientID    string           `json:"dhcp_client_id,omitempty"`    // Option 61
	DHCPOptions     map[uint8]string `json:"dhcp_options,omitempty"`      // All observed options (hex-encoded)

	// Device classification (derived from mDNS/DHCP when known)
	DeviceType  string `json:"device_type,omitempty"`  // "chromecast", "apple_tv", "printer"
	DeviceModel string `json:"device_model,omitempty"` // "Chromecast Ultra", "iPhone 15"
}

// PacketEvent represents a packet observation (interface to avoid import cycle)
type PacketEvent struct {
	Timestamp time.Time
	HwAddr    string // Source MAC address
	SrcIP     string
	DstIP     string
	InDev     string // Interface index string
	InDevName string // Interface name (e.g., "eth0")
	Protocol  string // "TCP", "UDP", "ICMP", etc.
	SrcPort   uint16
	DstPort   uint16
}

// MDNSEvent represents an mDNS announcement from a device
type MDNSEvent struct {
	Timestamp  time.Time
	SrcMAC     string            // Source MAC address
	SrcIP      string            // Source IP
	Interface  string            // Interface name
	Hostname   string            // Announced hostname
	Services   []string          // Service types: ["_googlecast._tcp", "_airplay._tcp"]
	TXTRecords map[string]string // TXT record key-values
}

// DHCPEvent represents a DHCP request from a device
type DHCPEvent struct {
	Timestamp   time.Time
	ClientMAC   string           // Client MAC address
	Interface   string           // Interface received on
	Hostname    string           // Option 12: Hostname
	Fingerprint string           // Option 55: Parameter Request List
	VendorClass string           // Option 60: Vendor Class Identifier
	ClientID    string           // Option 61: Client Identifier
	Options     map[uint8]string // All DHCP options (hex-encoded values)
}

// EnrichFunc is called to enrich a device with additional data
type EnrichFunc func(dev *SeenDevice)

// Collector subscribes to packet events and maintains seen devices
type Collector struct {
	mu         sync.RWMutex
	devices    map[string]*SeenDevice // MAC -> Device
	enrichFunc EnrichFunc
	logger     *logging.Logger

	// Enrichment queue
	enrichQueue chan string     // MAC addresses to enrich
	enrichedSet map[string]bool // Track what's been enriched

	// Event channels
	events     chan PacketEvent
	mdnsEvents chan MDNSEvent  // mDNS announcements
	dhcpEvents chan DHCPEvent  // DHCP requests

	storagePath string // Path to persistence file
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewCollector creates a new device collector
func NewCollector(enrichFunc EnrichFunc, storagePath string) *Collector {
	ctx, cancel := context.WithCancel(context.Background())
	return &Collector{
		devices:     make(map[string]*SeenDevice),
		enrichFunc:  enrichFunc,
		logger:      logging.WithComponent("discovery"),
		storagePath: storagePath,
		enrichQueue: make(chan string, 1000),
		enrichedSet: make(map[string]bool),
		events:      make(chan PacketEvent, 1000),
		mdnsEvents:  make(chan MDNSEvent, 100),
		dhcpEvents:  make(chan DHCPEvent, 100),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Events returns the channel to send packet events to
func (c *Collector) Events() chan<- PacketEvent {
	return c.events
}

// MDNSEvents returns the channel to send mDNS events to
func (c *Collector) MDNSEvents() chan<- MDNSEvent {
	return c.mdnsEvents
}

// DHCPEvents returns the channel to send DHCP events to
func (c *Collector) DHCPEvents() chan<- DHCPEvent {
	return c.dhcpEvents
}

// Start begins processing packet events
func (c *Collector) Start() {
	// Load persisted data
	if err := c.Load(); err != nil {
		c.logger.Error("failed to load discovery data", "error", err)
	}

	// Start persistence worker
	go c.persistenceWorker()

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
			case event := <-c.mdnsEvents:
				c.processMDNSEvent(event)
			case event := <-c.dhcpEvents:
				c.processDHCPEvent(event)
			}
		}
	}()

	c.logger.Info("collector started")
}

// Stop stops the collector
func (c *Collector) Stop() {
	c.cancel()
	// Save on exit
	c.Save()
	c.logger.Info("collector stopped")
}

// processEvent handles a single packet event
func (c *Collector) processEvent(event PacketEvent) {
	// Skip events without MAC
	if event.HwAddr == "" || event.HwAddr == "00:00:00:00:00:00" {
		return
	}

	// Normalize MAC to lowercase
	mac := strings.ToLower(event.HwAddr)

	// Determine interface name (prefer name over index)
	ifaceName := event.InDevName
	if ifaceName == "" {
		ifaceName = event.InDev
	}

	// Check if this looks like a gateway (public IP on what's likely a WAN interface)
	isPublicIP := event.SrcIP != "" && !isPrivateIP(event.SrcIP)

	c.mu.Lock()
	defer c.mu.Unlock()

	dev, exists := c.devices[mac]
	if !exists {
		// New device discovered
		dev = &SeenDevice{
			MAC:       mac,
			IPs:       []string{},
			Interface: ifaceName,
			FirstSeen: event.Timestamp,
			LastSeen:  event.Timestamp,
			Flags:     []string{"new"},
		}
		c.devices[mac] = dev

		// Queue for enrichment
		c.queueEnrichment(mac)

		c.logger.Info("new device", "mac", mac, "ip", event.SrcIP, "interface", ifaceName)
	}

	// Update existing device
	dev.LastSeen = event.Timestamp
	dev.PacketCount++

	// Detect gateway: if we see public IPs from this MAC, it's likely a gateway
	// Don't accumulate IPs for gateways (they're just the next hop)
	if isPublicIP && !dev.IsGateway {
		dev.IsGateway = true
		dev.Flags = append(dev.Flags, "gateway")
		dev.IPs = []string{} // Clear accumulated IPs - they're destinations, not the gateway's IP
		c.logger.Info("detected gateway", "mac", mac, "interface", ifaceName)
	}

	// Only add IPs for non-gateway devices
	if !dev.IsGateway && event.SrcIP != "" && !containsString(dev.IPs, event.SrcIP) {
		dev.IPs = append(dev.IPs, event.SrcIP)
	}

	// Update interface if different (device may have moved)
	if ifaceName != "" && dev.Interface != ifaceName {
		dev.Interface = ifaceName
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

// isPrivateIP checks if an IP address string is in private/reserved ranges
func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	// Check RFC 1918 private ranges
	privateBlocks := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",    // Loopback
		"169.254.0.0/16", // Link-local
		"fc00::/7",       // IPv6 unique local
		"fe80::/10",      // IPv6 link-local
	}

	for _, block := range privateBlocks {
		_, cidr, err := net.ParseCIDR(block)
		if err == nil && cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// processMDNSEvent updates a device with mDNS profile data
func (c *Collector) processMDNSEvent(event MDNSEvent) {
	if event.SrcMAC == "" {
		// Try to resolve MAC via ARP probe
		go c.probeAndRetry(event)
		return
	}

	mac := strings.ToLower(event.SrcMAC)

	c.mu.Lock()
	defer c.mu.Unlock()

	dev, exists := c.devices[mac]
	if !exists {
		// Create new device entry
		now := time.Now()
		dev = &SeenDevice{
			MAC:       mac,
			Interface: event.Interface,
			FirstSeen: now,
			LastSeen:  now,
		}
		c.devices[mac] = dev
		c.logger.Info("new device from mDNS", "mac", mac, "hostname", event.Hostname)
	}

	dev.LastSeen = event.Timestamp

	// Update hostname if provided
	if event.Hostname != "" {
		dev.MDNSHostname = event.Hostname
		if dev.Hostname == "" {
			dev.Hostname = event.Hostname
		}
	}

	// Merge services (avoid duplicates)
	for _, svc := range event.Services {
		if !containsString(dev.MDNSServices, svc) {
			dev.MDNSServices = append(dev.MDNSServices, svc)
		}
	}

	// Merge TXT records
	if event.TXTRecords != nil {
		if dev.MDNSTXTRecords == nil {
			dev.MDNSTXTRecords = make(map[string]string)
		}
		for k, v := range event.TXTRecords {
			dev.MDNSTXTRecords[k] = v
		}

		// Enrich from TXT records
		// fn = Friendly Name (e.g. "Bedroom TV")
		if fn, ok := event.TXTRecords["fn"]; ok && fn != "" {
			if dev.Alias == "" {
				dev.Alias = fn
			}
		}
		// md = Model Description (e.g. "Chromecast Ultra")
		if md, ok := event.TXTRecords["md"]; ok && md != "" {
			dev.DeviceModel = md
		} else if model, ok := event.TXTRecords["model"]; ok && model != "" {
			dev.DeviceModel = model
		}
	}

	// Infer device type from mDNS services
	dev.DeviceType = inferDeviceTypeFromMDNS(dev.MDNSServices)
}

// processDHCPEvent updates a device with DHCP fingerprint data
func (c *Collector) processDHCPEvent(event DHCPEvent) {
	if event.ClientMAC == "" {
		return
	}

	mac := strings.ToLower(event.ClientMAC)

	c.mu.Lock()
	defer c.mu.Unlock()

	dev, exists := c.devices[mac]
	if !exists {
		// Create new device entry
		now := time.Now()
		dev = &SeenDevice{
			MAC:       mac,
			Interface: event.Interface,
			FirstSeen: now,
			LastSeen:  now,
		}
		c.devices[mac] = dev
		c.logger.Info("new device from DHCP", "mac", mac, "hostname", event.Hostname)
	}

	dev.LastSeen = event.Timestamp

	// Update DHCP fields
	if event.Fingerprint != "" {
		dev.DHCPFingerprint = event.Fingerprint
	}
	if event.VendorClass != "" {
		dev.DHCPVendorClass = event.VendorClass
	}
	if event.Hostname != "" {
		dev.DHCPHostname = event.Hostname
		if dev.Hostname == "" {
			dev.Hostname = event.Hostname
		}
	}
	if event.ClientID != "" {
		dev.DHCPClientID = event.ClientID
	}

	// Merge all options
	if event.Options != nil {
		if dev.DHCPOptions == nil {
			dev.DHCPOptions = make(map[uint8]string)
		}
		for k, v := range event.Options {
			dev.DHCPOptions[k] = v
		}
	}
}

// inferDeviceTypeFromMDNS derives device type from observed mDNS services
func inferDeviceTypeFromMDNS(services []string) string {
	for _, svc := range services {
		switch svc {
		case "_googlecast._tcp", "_googlecast._tcp.local":
			return "chromecast"
		case "_airplay._tcp", "_airplay._tcp.local":
			return "apple_tv"
		case "_raop._tcp", "_raop._tcp.local":
			return "airplay_speaker"
		case "_spotify-connect._tcp", "_spotify-connect._tcp.local":
			return "spotify_device"
		case "_printer._tcp", "_ipp._tcp", "_pdl-datastream._tcp":
			return "printer"
		case "_smb._tcp", "_afpovertcp._tcp":
			return "file_server"
		case "_ssh._tcp":
			return "ssh_server"
		case "_http._tcp", "_https._tcp":
			return "web_server"
		case "_homekit._tcp", "_hap._tcp":
			return "homekit"
		case "_companion-link._tcp", "_companion-link._tcp.local":
			return "apple_device"
		case "_device-info._tcp", "_device-info._tcp.local":
			return "computer"
		}
	}
	return ""
}

// Save persists the devices to disk
func (c *Collector) Save() error {
	if c.storagePath == "" {
		return nil
	}

	c.mu.RLock()
	devices := make([]SeenDevice, 0, len(c.devices))
	for _, d := range c.devices {
		devices = append(devices, *d)
	}
	c.mu.RUnlock()

	data, err := json.MarshalIndent(devices, "", "  ")
	if err != nil {
		return err
	}

	// Atomic write
	tmp := c.storagePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, c.storagePath)
}

// Load reads devices from disk
func (c *Collector) Load() error {
	if c.storagePath == "" {
		return nil
	}

	data, err := os.ReadFile(c.storagePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var devices []SeenDevice
	if err := json.Unmarshal(data, &devices); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	retention := 30 * 24 * time.Hour // 30 days

	count := 0
	for _, d := range devices {
		// Expiration check
		if now.Sub(d.LastSeen) > retention {
			continue
		}

		dev := d // copy
		// Ensure slices are initialized if nil
		if dev.IPs == nil {
			dev.IPs = []string{}
		}
		if dev.Flags == nil {
			dev.Flags = []string{}
		}
		// Maps don't need init unless used, but safer to init if we access them
		if dev.MDNSServices == nil { dev.MDNSServices = []string{} }
		if dev.MDNSTXTRecords == nil { dev.MDNSTXTRecords = make(map[string]string) }
		if dev.DHCPOptions == nil { dev.DHCPOptions = make(map[uint8]string) }

		c.devices[strings.ToLower(dev.MAC)] = &dev
		count++
	}
	c.logger.Info("loaded persisted devices", "count", count, "pruned", len(devices)-count)
	return nil
}

func (c *Collector) persistenceWorker() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if err := c.Save(); err != nil {
				c.logger.Error("failed to save discovery data", "error", err)
			}
		}
	}
}

// probeAndRetry attempts to trigger ARP resolution by pinging the IP, then retries MAC lookup
func (c *Collector) probeAndRetry(event MDNSEvent) {
	// 1. Send Unicast Ping to trigger ARP resolution
	// Use short timeout (1s) to avoid blocking too long
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// We don't care about success of Ping (firewall might drop it), only that it triggers ARP
	_ = exec.CommandContext(ctx, "ping", "-c", "1", "-W", "1", event.SrcIP).Run()

	// 2. Retry lookup
	resolved := getMACFromARP(event.SrcIP)
	if resolved != "" {
		event.SrcMAC = resolved
		c.logger.Info("Resolved MAC via ARP probe", "ip", event.SrcIP, "mac", resolved)

		// Re-submit to channel to be processed safely in main loop
		select {
		case c.mdnsEvents <- event:
		default:
			c.logger.Warn("Failed to re-queue mDNS event after probe (channel full)")
		}
	} else {
		c.logger.Warn("Still could not resolve MAC after probe", "ip", event.SrcIP)
	}
}
