//go:build linux
// +build linux

package firewall

import (
	"fmt"

	"grimm.is/glacic/internal/network"

	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

// MarkRule defines a rule for setting routing marks on packets.
type MarkRule struct {
	Name     string
	Mark     network.RoutingMark
	MarkMask network.RoutingMark // For mark/mask operations

	// Match criteria
	Protocol     string   // tcp, udp, icmp, all
	SrcIP        string   // Source IP/CIDR
	DstIP        string   // Destination IP/CIDR
	SrcPort      uint16   // Source port
	DstPort      uint16   // Destination port
	SrcPorts     []uint16 // Multiple source ports
	DstPorts     []uint16 // Multiple destination ports
	InInterface  string   // Input interface
	OutInterface string   // Output interface
	SrcZone      string   // Source zone
	DstZone      string   // Destination zone
	IPSet        string   // Match against IPSet

	// Additional matching
	ConnState []string // NEW, ESTABLISHED, RELATED, INVALID
	DSCP      uint8    // Match DSCP value

	// Action modifiers
	SaveMark    bool // Save mark to connection (ct mark)
	RestoreMark bool // Restore mark from connection

	Enabled bool
	Comment string
}

// MarkManager manages nftables mark rules.
type MarkManager struct {
	conn  *nftables.Conn
	table *nftables.Table
	chain *nftables.Chain
}

// NewMarkManager creates a new mark manager.
func NewMarkManager(conn *nftables.Conn, table *nftables.Table) *MarkManager {
	return &MarkManager{
		conn:  conn,
		table: table,
	}
}

// SetupMarkChain creates the mangle chain for setting marks.
func (m *MarkManager) SetupMarkChain() error {
	// Create prerouting chain for marking incoming packets
	m.chain = m.conn.AddChain(&nftables.Chain{
		Name:     "mark_prerouting",
		Table:    m.table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityMangle, // -150, before routing decision
	})

	// Create output chain for marking locally-generated packets
	m.conn.AddChain(&nftables.Chain{
		Name:     "mark_output",
		Table:    m.table,
		Type:     nftables.ChainTypeRoute, // Route type for output marking
		Hooknum:  nftables.ChainHookOutput,
		Priority: nftables.ChainPriorityMangle,
	})

	// Create forward chain for marking forwarded packets
	m.conn.AddChain(&nftables.Chain{
		Name:     "mark_forward",
		Table:    m.table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookForward,
		Priority: nftables.ChainPriorityMangle,
	})

	return m.conn.Flush()
}

// AddMarkRule adds a rule to set marks on matching packets.
func (m *MarkManager) AddMarkRule(chain *nftables.Chain, rule MarkRule) error {
	if !rule.Enabled {
		return nil
	}

	var exprs []expr.Any

	// Match input interface
	if rule.InInterface != "" {
		exprs = append(exprs,
			&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     []byte(rule.InInterface + "\x00"),
			},
		)
	}

	// Match output interface
	if rule.OutInterface != "" {
		exprs = append(exprs,
			&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     []byte(rule.OutInterface + "\x00"),
			},
		)
	}

	// Match protocol
	if rule.Protocol != "" && rule.Protocol != "all" {
		proto := protocolToNumber(rule.Protocol)
		if proto > 0 {
			exprs = append(exprs,
				&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     []byte{proto},
				},
			)
		}
	}

	// Match destination port
	if rule.DstPort > 0 {
		exprs = append(exprs,
			&expr.Payload{
				DestRegister: 1,
				Base:         expr.PayloadBaseTransportHeader,
				Offset:       2, // Destination port offset
				Len:          2,
			},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     binaryutil.BigEndian.PutUint16(rule.DstPort),
			},
		)
	}

	// Match source port
	if rule.SrcPort > 0 {
		exprs = append(exprs,
			&expr.Payload{
				DestRegister: 1,
				Base:         expr.PayloadBaseTransportHeader,
				Offset:       0, // Source port offset
				Len:          2,
			},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     binaryutil.BigEndian.PutUint16(rule.SrcPort),
			},
		)
	}

	// Restore mark from conntrack if requested
	if rule.RestoreMark {
		exprs = append(exprs,
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1},
			&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
		)
	} else {
		// Set the mark
		if rule.MarkMask != 0 {
			// Use mark with mask: (existing_mark & ~mask) | (new_mark & mask)
			exprs = append(exprs,
				// Load current mark
				&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
				// AND with inverted mask
				&expr.Bitwise{
					SourceRegister: 1,
					DestRegister:   1,
					Len:            4,
					Mask:           binaryutil.NativeEndian.PutUint32(^uint32(rule.MarkMask)),
					Xor:            binaryutil.NativeEndian.PutUint32(0),
				},
				// OR with new mark (already masked)
				&expr.Bitwise{
					SourceRegister: 1,
					DestRegister:   1,
					Len:            4,
					Mask:           binaryutil.NativeEndian.PutUint32(0xFFFFFFFF),
					Xor:            binaryutil.NativeEndian.PutUint32(uint32(rule.Mark) & uint32(rule.MarkMask)),
				},
				// Set the mark
				&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
			)
		} else {
			// Simple mark set
			exprs = append(exprs,
				&expr.Immediate{
					Register: 1,
					Data:     binaryutil.NativeEndian.PutUint32(uint32(rule.Mark)),
				},
				&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
			)
		}
	}

	// Save mark to conntrack if requested
	if rule.SaveMark {
		exprs = append(exprs,
			&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1, SourceRegister: true},
		)
	}

	m.conn.AddRule(&nftables.Rule{
		Table: m.table,
		Chain: chain,
		Exprs: exprs,
	})

	return nil
}

// AddConnectionMarkRestore adds a rule to restore marks from connection tracking.
// This should be early in the chain to restore marks for existing connections.
func (m *MarkManager) AddConnectionMarkRestore(chain *nftables.Chain) error {
	// ct state established,related meta mark set ct mark
	m.conn.AddRule(&nftables.Rule{
		Table: m.table,
		Chain: chain,
		Exprs: []expr.Any{
			// Match established/related
			&expr.Ct{Key: expr.CtKeySTATE, Register: 1},
			&expr.Bitwise{
				SourceRegister: 1,
				DestRegister:   1,
				Len:            4,
				Mask:           binaryutil.NativeEndian.PutUint32(expr.CtStateBitESTABLISHED | expr.CtStateBitRELATED),
				Xor:            binaryutil.NativeEndian.PutUint32(0),
			},
			&expr.Cmp{
				Op:       expr.CmpOpNeq,
				Register: 1,
				Data:     binaryutil.NativeEndian.PutUint32(0),
			},
			// Restore mark from ct mark
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1},
			&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
		},
	})

	return nil
}

// AddConnectionMarkSave adds a rule to save marks to connection tracking.
// This should be at the end of the marking chain.
func (m *MarkManager) AddConnectionMarkSave(chain *nftables.Chain) error {
	// meta mark != 0 ct mark set meta mark
	m.conn.AddRule(&nftables.Rule{
		Table: m.table,
		Chain: chain,
		Exprs: []expr.Any{
			// Check mark is not zero
			&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
			&expr.Cmp{
				Op:       expr.CmpOpNeq,
				Register: 1,
				Data:     binaryutil.NativeEndian.PutUint32(0),
			},
			// Save to ct mark
			&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1, SourceRegister: true},
		},
	})

	return nil
}

// protocolToNumber converts protocol name to number.
func protocolToNumber(proto string) uint8 {
	switch proto {
	case "tcp":
		return unix.IPPROTO_TCP
	case "udp":
		return unix.IPPROTO_UDP
	case "icmp":
		return unix.IPPROTO_ICMP
	case "icmpv6":
		return unix.IPPROTO_ICMPV6
	case "gre":
		return unix.IPPROTO_GRE
	case "esp":
		return unix.IPPROTO_ESP
	case "ah":
		return unix.IPPROTO_AH
	case "sctp":
		return unix.IPPROTO_SCTP
	default:
		return 0
	}
}

// MarkRuleBuilder provides a fluent interface for building mark rules.
type MarkRuleBuilder struct {
	rule MarkRule
}

// NewMarkRuleBuilder creates a new mark rule builder.
func NewMarkRuleBuilder(name string) *MarkRuleBuilder {
	return &MarkRuleBuilder{
		rule: MarkRule{
			Name:    name,
			Enabled: true,
		},
	}
}

// Mark sets the mark value.
func (b *MarkRuleBuilder) Mark(mark network.RoutingMark) *MarkRuleBuilder {
	b.rule.Mark = mark
	return b
}

// MarkWithMask sets mark with a mask.
func (b *MarkRuleBuilder) MarkWithMask(mark, mask network.RoutingMark) *MarkRuleBuilder {
	b.rule.Mark = mark
	b.rule.MarkMask = mask
	return b
}

// Protocol sets the protocol to match.
func (b *MarkRuleBuilder) Protocol(proto string) *MarkRuleBuilder {
	b.rule.Protocol = proto
	return b
}

// DstPort sets the destination port.
func (b *MarkRuleBuilder) DstPort(port uint16) *MarkRuleBuilder {
	b.rule.DstPort = port
	return b
}

// SrcPort sets the source port.
func (b *MarkRuleBuilder) SrcPort(port uint16) *MarkRuleBuilder {
	b.rule.SrcPort = port
	return b
}

// InInterface sets the input interface.
func (b *MarkRuleBuilder) InInterface(iface string) *MarkRuleBuilder {
	b.rule.InInterface = iface
	return b
}

// OutInterface sets the output interface.
func (b *MarkRuleBuilder) OutInterface(iface string) *MarkRuleBuilder {
	b.rule.OutInterface = iface
	return b
}

// SrcIP sets the source IP/CIDR.
func (b *MarkRuleBuilder) SrcIP(ip string) *MarkRuleBuilder {
	b.rule.SrcIP = ip
	return b
}

// DstIP sets the destination IP/CIDR.
func (b *MarkRuleBuilder) DstIP(ip string) *MarkRuleBuilder {
	b.rule.DstIP = ip
	return b
}

// SaveToConntrack enables saving the mark to connection tracking.
func (b *MarkRuleBuilder) SaveToConntrack() *MarkRuleBuilder {
	b.rule.SaveMark = true
	return b
}

// RestoreFromConntrack enables restoring marks from connection tracking.
func (b *MarkRuleBuilder) RestoreFromConntrack() *MarkRuleBuilder {
	b.rule.RestoreMark = true
	return b
}

// Comment sets the rule comment.
func (b *MarkRuleBuilder) Comment(comment string) *MarkRuleBuilder {
	b.rule.Comment = comment
	return b
}

// Disabled disables the rule.
func (b *MarkRuleBuilder) Disabled() *MarkRuleBuilder {
	b.rule.Enabled = false
	return b
}

// Build returns the constructed rule.
func (b *MarkRuleBuilder) Build() MarkRule {
	return b.rule
}

// Common mark rule presets

// MarkForVPNBypass creates a rule to mark traffic for VPN bypass.
func MarkForVPNBypass(dstIP string) MarkRule {
	return NewMarkRuleBuilder("vpn-bypass").
		Mark(network.MarkBypassVPN).
		DstIP(dstIP).
		SaveToConntrack().
		Comment("Bypass VPN for this destination").
		Build()
}

// MarkForWAN creates a rule to mark traffic for a specific WAN.
func MarkForWAN(wanNum int, srcIP string) MarkRule {
	mark := network.MarkWAN1 + network.RoutingMark(wanNum-1)
	return NewMarkRuleBuilder(fmt.Sprintf("wan%d-route", wanNum)).
		Mark(mark).
		SrcIP(srcIP).
		SaveToConntrack().
		Comment(fmt.Sprintf("Route via WAN%d", wanNum)).
		Build()
}

// MarkByPort creates a rule to mark traffic by destination port.
func MarkByPort(proto string, port uint16, mark network.RoutingMark) MarkRule {
	return NewMarkRuleBuilder(fmt.Sprintf("port-%d", port)).
		Mark(mark).
		Protocol(proto).
		DstPort(port).
		SaveToConntrack().
		Build()
}

// MarkForWireGuard creates a rule to mark traffic for a specific WireGuard tunnel.
func MarkForWireGuard(tunnelIndex int, srcIP string) MarkRule {
	mark := network.MarkForWireGuard(tunnelIndex)
	return NewMarkRuleBuilder(fmt.Sprintf("wg%d-route", tunnelIndex)).
		Mark(mark).
		SrcIP(srcIP).
		SaveToConntrack().
		Comment(fmt.Sprintf("Route via WireGuard tunnel %d", tunnelIndex)).
		Build()
}

// MarkForTailscale creates a rule to mark traffic for a specific Tailscale connection.
func MarkForTailscale(connIndex int, srcIP string) MarkRule {
	mark := network.MarkForTailscale(connIndex)
	return NewMarkRuleBuilder(fmt.Sprintf("ts%d-route", connIndex)).
		Mark(mark).
		SrcIP(srcIP).
		SaveToConntrack().
		Comment(fmt.Sprintf("Route via Tailscale connection %d", connIndex)).
		Build()
}

// MarkForVPNTunnel creates a rule to mark traffic for any VPN tunnel by type and index.
func MarkForVPNTunnel(vpnType string, index int, srcIP string) MarkRule {
	var mark network.RoutingMark
	switch vpnType {
	case "wireguard", "wg":
		mark = network.MarkForWireGuard(index)
	case "tailscale", "ts":
		mark = network.MarkForTailscale(index)
	case "openvpn", "ovpn":
		mark = network.MarkForOpenVPN(index)
	case "ipsec":
		mark = network.MarkForIPsec(index)
	default:
		mark = network.MarkVPNCustomBase + network.RoutingMark(index)
	}
	return NewMarkRuleBuilder(fmt.Sprintf("%s%d-route", vpnType, index)).
		Mark(mark).
		SrcIP(srcIP).
		SaveToConntrack().
		Comment(fmt.Sprintf("Route via %s tunnel %d", vpnType, index)).
		Build()
}

// MarkForQoS creates a rule to mark traffic for QoS classification.
func MarkForQoS(class string, proto string, dstPort uint16) MarkRule {
	var mark network.RoutingMark
	switch class {
	case "realtime", "voice", "video":
		mark = network.MarkQoSRealtime
	case "interactive", "gaming", "ssh":
		mark = network.MarkQoSInteractive
	case "bulk", "download":
		mark = network.MarkQoSBulk
	case "background", "update":
		mark = network.MarkQoSBackground
	default:
		mark = network.MarkQoSBase
	}
	return NewMarkRuleBuilder(fmt.Sprintf("qos-%s", class)).
		Mark(mark).
		Protocol(proto).
		DstPort(dstPort).
		Comment(fmt.Sprintf("QoS class: %s", class)).
		Build()
}
