//go:build linux
// +build linux

package firewall

import (
	"fmt"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// QoSConfig defines Quality of Service configuration.
type QoSConfig struct {
	Enabled    bool       `hcl:"enabled,optional" json:"enabled"`
	Interfaces []QoSIface `hcl:"interface,block" json:"interfaces"`
	Classes    []QoSClass `hcl:"class,block" json:"classes"`
	Rules      []QoSRule  `hcl:"rule,block" json:"rules"`
}

// QoSIface defines QoS settings for an interface.
type QoSIface struct {
	Name         string `hcl:"name,label" json:"name"`
	DownloadRate string `hcl:"download,optional" json:"download"` // e.g., "100mbit"
	UploadRate   string `hcl:"upload,optional" json:"upload"`     // e.g., "20mbit"
}

// QoSClass defines a traffic class with guaranteed/max bandwidth.
type QoSClass struct {
	Name     string `hcl:"name,label" json:"name"`
	Priority int    `hcl:"priority,optional" json:"priority"` // 1-7, lower is higher priority
	Rate     string `hcl:"rate,optional" json:"rate"`         // Guaranteed rate
	Ceil     string `hcl:"ceil,optional" json:"ceil"`         // Max rate (ceiling)
	Burst    string `hcl:"burst,optional" json:"burst"`       // Burst size
}

// QoSRule maps traffic to a class.
type QoSRule struct {
	Name     string   `hcl:"name,label" json:"name"`
	Class    string   `hcl:"class" json:"class"`                // Target class name
	Services []string `hcl:"services,optional" json:"services"` // Service names
	Ports    []int    `hcl:"ports,optional" json:"ports"`       // Explicit ports
	Protocol string   `hcl:"proto,optional" json:"proto"`       // tcp, udp
	SrcIP    string   `hcl:"src_ip,optional" json:"src_ip"`
	DstIP    string   `hcl:"dst_ip,optional" json:"dst_ip"`
	DSCP     string   `hcl:"dscp,optional" json:"dscp"` // DSCP marking
}

// QoSManager manages traffic shaping using netlink (kernel APIs).
type QoSManager struct {
	config *QoSConfig
}

// NewQoSManager creates a new QoS manager.
func NewQoSManager() *QoSManager {
	return &QoSManager{}
}

// Apply applies QoS configuration to interfaces using netlink.
func (m *QoSManager) Apply(cfg *QoSConfig) error {
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	m.config = cfg

	for _, iface := range cfg.Interfaces {
		link, err := netlink.LinkByName(iface.Name)
		if err != nil {
			return fmt.Errorf("interface %s not found: %w", iface.Name, err)
		}

		// Clear existing qdisc
		m.clearQdisc(link)

		// Set up HTB (Hierarchical Token Bucket) qdisc
		if err := m.setupHTB(link, iface); err != nil {
			return fmt.Errorf("failed to setup HTB on %s: %w", iface.Name, err)
		}

		// Create classes
		for i, class := range cfg.Classes {
			if err := m.createClass(link, uint32(i+10), class, iface); err != nil {
				return fmt.Errorf("failed to create class %s: %w", class.Name, err)
			}
		}

		// Create filters for rules
		for _, rule := range cfg.Rules {
			if err := m.createFilter(link, rule, cfg.Classes); err != nil {
				return fmt.Errorf("failed to create filter %s: %w", rule.Name, err)
			}
		}
	}

	return nil
}

// clearQdisc removes existing qdisc from interface using netlink.
func (m *QoSManager) clearQdisc(link netlink.Link) {
	// Delete root qdisc (this removes all child classes and filters)
	qdiscs, _ := netlink.QdiscList(link)
	for _, qdisc := range qdiscs {
		netlink.QdiscDel(qdisc)
	}
}

// setupHTB sets up HTB qdisc on interface using netlink.
func (m *QoSManager) setupHTB(link netlink.Link, iface QoSIface) error {
	// Parse rate
	rate := parseRate(iface.UploadRate)
	if rate == 0 {
		rate = 1000 * 1000 * 1000 / 8 // 1Gbps in bytes/sec
	}

	// Create root HTB qdisc
	htbQdisc := &netlink.Htb{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: link.Attrs().Index,
			Handle:    netlink.MakeHandle(1, 0),
			Parent:    netlink.HANDLE_ROOT,
		},
		Defcls: 99, // Default class
	}

	if err := netlink.QdiscAdd(htbQdisc); err != nil {
		return fmt.Errorf("failed to add HTB qdisc: %w", err)
	}

	// Create root class (1:1) with full bandwidth
	rootClass := &netlink.HtbClass{
		ClassAttrs: netlink.ClassAttrs{
			LinkIndex: link.Attrs().Index,
			Handle:    netlink.MakeHandle(1, 1),
			Parent:    netlink.MakeHandle(1, 0),
		},
		Rate:    uint64(rate),
		Ceil:    uint64(rate),
		Buffer:  uint32(rate/250 + 1600), // ~4ms buffer
		Cbuffer: uint32(rate/250 + 1600),
	}

	if err := netlink.ClassAdd(rootClass); err != nil {
		return fmt.Errorf("failed to add root class: %w", err)
	}

	// Create default class (1:99) - lowest priority catch-all
	defaultClass := &netlink.HtbClass{
		ClassAttrs: netlink.ClassAttrs{
			LinkIndex: link.Attrs().Index,
			Handle:    netlink.MakeHandle(1, 99),
			Parent:    netlink.MakeHandle(1, 1),
		},
		Rate:    uint64(rate / 100), // 1% guaranteed
		Ceil:    uint64(rate),       // Can burst to full
		Prio:    7,                  // Lowest priority
		Buffer:  uint32(rate/250 + 1600),
		Cbuffer: uint32(rate/250 + 1600),
	}

	if err := netlink.ClassAdd(defaultClass); err != nil {
		return fmt.Errorf("failed to add default class: %w", err)
	}

	return nil
}

// createClass creates a traffic class using netlink.
func (m *QoSManager) createClass(link netlink.Link, classID uint32, class QoSClass, iface QoSIface) error {
	parentRate := parseRate(iface.UploadRate)
	if parentRate == 0 {
		parentRate = 1000 * 1000 * 1000 / 8
	}

	rate := parseRate(class.Rate)
	if rate == 0 {
		rate = parentRate / 10 // Default 10% guaranteed
	}

	ceil := parseRate(class.Ceil)
	if ceil == 0 {
		ceil = parentRate // Default: can use full bandwidth
	}

	prio := uint32(class.Priority)
	if prio == 0 {
		prio = 4 // Default middle priority
	}

	htbClass := &netlink.HtbClass{
		ClassAttrs: netlink.ClassAttrs{
			LinkIndex: link.Attrs().Index,
			Handle:    netlink.MakeHandle(1, uint16(classID)),
			Parent:    netlink.MakeHandle(1, 1),
		},
		Rate:    uint64(rate),
		Ceil:    uint64(ceil),
		Prio:    prio,
		Buffer:  uint32(rate/250 + 1600),
		Cbuffer: uint32(ceil/250 + 1600),
	}

	if err := netlink.ClassAdd(htbClass); err != nil {
		return fmt.Errorf("failed to add class: %w", err)
	}

	// Add SFQ qdisc to class for fair queuing within the class
	sfqQdisc := &netlink.Sfq{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: link.Attrs().Index,
			Handle:    netlink.MakeHandle(uint16(classID), 0),
			Parent:    netlink.MakeHandle(1, uint16(classID)),
		},
		Perturb: 10, // Rehash every 10 seconds
	}

	// SFQ is optional, don't fail if it doesn't work
	netlink.QdiscAdd(sfqQdisc)

	return nil
}

// createFilter creates a tc filter to classify traffic using netlink.
func (m *QoSManager) createFilter(link netlink.Link, rule QoSRule, classes []QoSClass) error {
	// Find class ID
	classID := uint32(99) // Default
	for i, c := range classes {
		if c.Name == rule.Class {
			classID = uint32(i + 10)
			break
		}
	}

	// Handle services - look up in our service catalog
	for _, svc := range rule.Services {
		if svcDef, ok := BuiltinServices[svc]; ok {
			// BuiltinServices has single Port, not Ports list for now?
			// Checking services.go to confirm structure.
			// services.go: type Service struct { Port int; Code []string; Module string }
			port := svcDef.Port
			if port > 0 {
				// Use "tcp" as default if Protocol is not available or assume it matches service definition logic
				// Actually Service struct has Protocol field.
				protoStr := "tcp"
				if svcDef.Protocol == ProtoUDP {
					protoStr = "udp"
				}
				if err := m.addU32Filter(link, uint16(port), protoStr, classID); err != nil {
					return err
				}
			}
		}
	}

	// Handle explicit ports
	for _, port := range rule.Ports {
		proto := rule.Protocol
		if proto == "" {
			proto = "tcp"
		}
		if err := m.addU32Filter(link, uint16(port), proto, classID); err != nil {
			return err
		}
	}

	// Handle DSCP marking
	if rule.DSCP != "" {
		if err := m.addDSCPFilter(link, rule.DSCP, classID); err != nil {
			return err
		}
	}

	return nil
}

// addU32Filter adds a u32 filter for port matching.
func (m *QoSManager) addU32Filter(link netlink.Link, port uint16, proto string, classID uint32) error {
	protoNum := uint8(unix.IPPROTO_TCP)
	if proto == "udp" {
		protoNum = unix.IPPROTO_UDP
	}

	// Create u32 filter matching destination port
	filter := &netlink.U32{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: link.Attrs().Index,
			Parent:    netlink.MakeHandle(1, 0),
			Priority:  1,
			Protocol:  unix.ETH_P_IP,
		},
		ClassId: netlink.MakeHandle(1, uint16(classID)),
		Sel: &netlink.TcU32Sel{
			Flags: netlink.TC_U32_TERMINAL,
			Nkeys: 2,
			Keys: []netlink.TcU32Key{
				{
					// Match IP protocol (offset 9 in IP header)
					Mask: 0x00ff0000,
					Val:  uint32(protoNum) << 16,
					Off:  8,
				},
				{
					// Match destination port (offset 2 in TCP/UDP header = 22 from IP)
					Mask: 0x0000ffff,
					Val:  uint32(port),
					Off:  20,
				},
			},
		},
	}

	if err := netlink.FilterAdd(filter); err != nil {
		// Filter might already exist
		if err.Error() != "file exists" {
			return fmt.Errorf("failed to add port filter: %w", err)
		}
	}

	return nil
}

// addDSCPFilter adds a filter matching DSCP values.
func (m *QoSManager) addDSCPFilter(link netlink.Link, dscp string, classID uint32) error {
	dscpVal, ok := dscpValues[dscp]
	if !ok {
		return fmt.Errorf("unknown DSCP value: %s", dscp)
	}

	// DSCP is in bits 2-7 of TOS byte (byte 1 of IP header)
	tosValue := uint32(dscpVal << 2)

	filter := &netlink.U32{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: link.Attrs().Index,
			Parent:    netlink.MakeHandle(1, 0),
			Priority:  1,
			Protocol:  unix.ETH_P_IP,
		},
		ClassId: netlink.MakeHandle(1, uint16(classID)),
		Sel: &netlink.TcU32Sel{
			Flags: netlink.TC_U32_TERMINAL,
			Nkeys: 1,
			Keys: []netlink.TcU32Key{
				{
					// Match TOS byte (offset 1 in IP header, in first 4-byte word)
					Mask: 0x00fc0000, // DSCP bits in TOS
					Val:  tosValue << 16,
					Off:  0,
				},
			},
		},
	}

	if err := netlink.FilterAdd(filter); err != nil {
		if err.Error() != "file exists" {
			return fmt.Errorf("failed to add DSCP filter: %w", err)
		}
	}

	return nil
}

// Clear removes all QoS configuration using netlink.
func (m *QoSManager) Clear() error {
	if m.config == nil {
		return nil
	}

	for _, iface := range m.config.Interfaces {
		link, err := netlink.LinkByName(iface.Name)
		if err != nil {
			continue
		}
		m.clearQdisc(link)
	}

	return nil
}

// QoSStats holds statistics for a QoS class.
type QoSStats struct {
	ClassID uint32 `json:"class_id"`
	Bytes   uint64 `json:"bytes"`
	Packets uint32 `json:"packets"`
	Drops   uint32 `json:"drops"`
	Rate    uint64 `json:"rate"`
}

// GetStats returns QoS statistics for an interface using netlink.
func (m *QoSManager) GetStats(ifaceName string) ([]QoSStats, error) {
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return nil, err
	}

	classes, err := netlink.ClassList(link, netlink.MakeHandle(1, 0))
	if err != nil {
		return nil, err
	}

	stats := make([]QoSStats, 0, len(classes))
	for _, class := range classes {
		if htb, ok := class.(*netlink.HtbClass); ok {
			stats = append(stats, QoSStats{
				ClassID: htb.Handle & 0xFFFF,
				Bytes:   htb.Statistics.Basic.Bytes,
				Packets: htb.Statistics.Basic.Packets,
				Drops:   htb.Statistics.Queue.Drops,
				Rate:    htb.Rate,
			})
		}
	}

	return stats, nil
}

// parseRate parses rate strings like "10mbit", "1gbit", "500kbit" to bytes/sec.
func parseRate(s string) uint64 {
	if s == "" {
		return 0
	}

	var value float64
	var unit string
	fmt.Sscanf(s, "%f%s", &value, &unit)

	multiplier := uint64(1)
	switch unit {
	case "kbit", "kbps":
		multiplier = 1000 / 8
	case "mbit", "mbps":
		multiplier = 1000 * 1000 / 8
	case "gbit", "gbps":
		multiplier = 1000 * 1000 * 1000 / 8
	case "kbyte", "kBps":
		multiplier = 1000
	case "mbyte", "mBps":
		multiplier = 1000 * 1000
	case "%":
		// Percentage - caller needs to handle
		return uint64(value)
	}

	return uint64(value) * multiplier
}

// DSCP value mappings
var dscpValues = map[string]int{
	"ef":   46, // Expedited Forwarding (VoIP)
	"af11": 10, // Assured Forwarding
	"af12": 12,
	"af13": 14,
	"af21": 18,
	"af22": 20,
	"af23": 22,
	"af31": 26,
	"af32": 28,
	"af33": 30,
	"af41": 34,
	"af42": 36,
	"af43": 38,
	"cs1":  8, // Class Selector
	"cs2":  16,
	"cs3":  24,
	"cs4":  32,
	"cs5":  40,
	"cs6":  48,
	"cs7":  56,
}

// GetInterfaceStats returns interface-level traffic statistics.
func GetInterfaceStats(ifaceName string) (*netlink.LinkStatistics, error) {
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return nil, err
	}
	return link.Attrs().Statistics, nil
}

// PredefinedQoSProfiles contains common QoS configurations.
var PredefinedQoSProfiles = map[string]QoSConfig{
	"gaming": {
		Enabled: true,
		Classes: []QoSClass{
			{Name: "realtime", Priority: 1, Rate: "30%", Ceil: "100%"},
			{Name: "interactive", Priority: 2, Rate: "30%", Ceil: "100%"},
			{Name: "bulk", Priority: 5, Rate: "20%", Ceil: "90%"},
		},
		Rules: []QoSRule{
			{Name: "voip", Class: "realtime", Services: []string{"sip"}, DSCP: "ef"},
			{Name: "gaming", Class: "realtime", Ports: []int{3074, 3478, 3479, 3480}}, // Common game ports
			{Name: "ssh", Class: "interactive", Services: []string{"ssh"}},
			{Name: "web", Class: "interactive", Services: []string{"http", "https"}},
			{Name: "downloads", Class: "bulk", Ports: []int{6881, 6889}}, // BitTorrent
		},
	},
	"voip": {
		Enabled: true,
		Classes: []QoSClass{
			{Name: "voice", Priority: 1, Rate: "20%", Ceil: "50%"},
			{Name: "signaling", Priority: 2, Rate: "10%", Ceil: "30%"},
			{Name: "default", Priority: 4, Rate: "70%", Ceil: "100%"},
		},
		Rules: []QoSRule{
			{Name: "rtp", Class: "voice", DSCP: "ef"},
			{Name: "sip", Class: "signaling", Services: []string{"sip"}},
		},
	},
	"balanced": {
		Enabled: true,
		Classes: []QoSClass{
			{Name: "priority", Priority: 1, Rate: "20%", Ceil: "100%"},
			{Name: "normal", Priority: 3, Rate: "60%", Ceil: "100%"},
			{Name: "bulk", Priority: 5, Rate: "20%", Ceil: "80%"},
		},
		Rules: []QoSRule{
			{Name: "interactive", Class: "priority", Services: []string{"ssh", "dns"}},
			{Name: "web", Class: "normal", Services: []string{"http", "https"}},
		},
	},
}
