// Package events provides a unified pub/sub event bus for Glacic.
// All observability data (DHCP, DNS, NFT, discovery) flows through this hub.
package events

import "time"

// EventType identifies the category of event.
type EventType string

// Event types for all observability sources.
const (
	// DHCP events
	EventDHCPLease  EventType = "dhcp.lease"
	EventDHCPExpire EventType = "dhcp.expire"

	// DNS events
	EventDNSQuery EventType = "dns.query"
	EventDNSBlock EventType = "dns.block"

	// NFT (firewall) events
	EventNFTMatch   EventType = "nft.match"   // Log match (from nflog)
	EventNFTCounter EventType = "nft.counter" // Counter update

	// Device/discovery events
	EventDeviceSeen    EventType = "device.seen"
	EventDeviceNew     EventType = "device.new"
	EventDeviceOffline EventType = "device.offline"

	// Learning engine events
	EventFlowNew      EventType = "flow.new"
	EventFlowApproved EventType = "flow.approved"
	EventFlowBlocked  EventType = "flow.blocked"
)

// Event is the core message passed through the event bus.
type Event struct {
	Type      EventType   `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Source    string      `json:"source"` // Component that emitted: "dhcp", "dns", "nft", etc.
	Data      interface{} `json:"data"`   // Type-specific payload
}

// ──────────────────────────────────────────────────────────────────────────────
// Type-Specific Payloads
// ──────────────────────────────────────────────────────────────────────────────

// DHCPLeaseData is the payload for EventDHCPLease/EventDHCPExpire.
type DHCPLeaseData struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname,omitempty"`
	Vendor   string `json:"vendor,omitempty"`
}

// DNSQueryData is the payload for EventDNSQuery/EventDNSBlock.
type DNSQueryData struct {
	ClientIP string `json:"client_ip"`
	Domain   string `json:"domain"`
	Type     string `json:"type"` // "A", "AAAA", "CNAME", etc.
	Blocked  bool   `json:"blocked,omitempty"`
	Reason   string `json:"reason,omitempty"` // Block reason if blocked
}

// NFTMatchData is the payload for EventNFTMatch (from nflog).
type NFTMatchData struct {
	RuleID   string `json:"rule_id"`
	SrcIP    string `json:"src_ip"`
	DstIP    string `json:"dst_ip"`
	SrcPort  uint16 `json:"src_port,omitempty"`
	DstPort  uint16 `json:"dst_port,omitempty"`
	Protocol string `json:"protocol"`
	Action   string `json:"action"` // "accept", "drop", "reject"
	Bytes    uint64 `json:"bytes,omitempty"`
}

// NFTCounterData is the payload for EventNFTCounter (periodic counter poll).
type NFTCounterData struct {
	RuleID  string `json:"rule_id"`
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

// DeviceSeenData is the payload for device discovery events.
type DeviceSeenData struct {
	MAC       string `json:"mac"`
	IP        string `json:"ip,omitempty"`
	Hostname  string `json:"hostname,omitempty"`
	Vendor    string `json:"vendor,omitempty"`
	Interface string `json:"interface,omitempty"`
	Method    string `json:"method"` // "arp", "dhcp", "lldp", "mdns"
}

// FlowData is the payload for learning engine flow events.
type FlowData struct {
	SrcMAC    string `json:"src_mac"`
	SrcIP     string `json:"src_ip"`
	DstIP     string `json:"dst_ip"`
	DstPort   uint16 `json:"dst_port"`
	Protocol  string `json:"protocol"`
	Domain    string `json:"domain,omitempty"`    // Resolved from DNS/SNI
	SNI       string `json:"sni,omitempty"`       // TLS Server Name
	Encrypted bool   `json:"encrypted,omitempty"` // Was TLS?
}
