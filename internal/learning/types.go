package learning

import (
	"net"
	"time"

	"grimm.is/glacic/internal/config"
)

// LearnedPacket represents a packet captured by the learning system
type LearnedPacket struct {
	Timestamp time.Time `json:"timestamp"`
	Policy    string    `json:"policy"` // Extracted from log prefix (e.g., "lan_wan")
	SrcIP     net.IP    `json:"src_ip"`
	DstIP     net.IP    `json:"dst_ip"`
	SrcPort   uint16    `json:"src_port"`
	DstPort   uint16    `json:"dst_port"`
	Protocol  string    `json:"protocol"`  // tcp, udp, icmp
	Interface string    `json:"interface"` // Input interface
	Length    uint16    `json:"length"`
	Flags     string    `json:"flags"` // TCP flags if applicable
}

// PendingRule represents a rule waiting for approval
type PendingRule struct {
	ID     string `json:"id"`
	Policy string `json:"policy"`

	// Match criteria (may be wildcarded)
	SrcNetwork string `json:"src_network"` // "192.168.1.0/24" or "192.168.1.50/32"
	DstNetwork string `json:"dst_network"` // "0.0.0.0/0" for any
	DstPort    string `json:"dst_port"`    // "8080" or "8080-8090" or "*"
	Protocol   string `json:"protocol"`

	// Metadata
	FirstSeen       time.Time `json:"first_seen"`
	LastSeen        time.Time `json:"last_seen"`
	HitCount        int64     `json:"hit_count"`
	UniqueSourceIPs []string  `json:"unique_source_ips"`

	// Status
	Status          string     `json:"status"`           // pending, approved, denied, ignored
	SuggestedAction string     `json:"suggested_action"` // Based on heuristics
	ApprovedBy      string     `json:"approved_by"`
	ApprovedAt      *time.Time `json:"approved_at"`

	// Generated rule (when approved)
	GeneratedRule *config.PolicyRule `json:"generated_rule"`
}
