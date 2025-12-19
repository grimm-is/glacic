//go:build linux
// +build linux

package firewall

import (
	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/config"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

// DefaultProtection returns sensible default protection settings.
func DefaultProtection() *config.ProtectionConfig {
	return &config.ProtectionConfig{
		AntiSpoofing:       true,
		BogonFiltering:     true,
		InvalidPackets:     true,
		SynFloodProtection: true,
		SynFloodRate:       25,
		SynFloodBurst:      50,
		ICMPRateLimit:      true,
		ICMPRate:           10,
		NewConnRateLimit:   false, // Off by default, can be aggressive
		NewConnRate:        100,
	}
}

// RFC1918 private address ranges (should not appear on WAN as source)
var privateNetworks = []struct {
	ip   []byte
	mask []byte
}{
	{[]byte{10, 0, 0, 0}, []byte{255, 0, 0, 0}},      // 10.0.0.0/8
	{[]byte{172, 16, 0, 0}, []byte{255, 240, 0, 0}},  // 172.16.0.0/12
	{[]byte{192, 168, 0, 0}, []byte{255, 255, 0, 0}}, // 192.168.0.0/16
}

// Bogon ranges - reserved/invalid addresses
var bogonNetworks = []struct {
	ip   []byte
	mask []byte
}{
	{[]byte{0, 0, 0, 0}, []byte{255, 0, 0, 0}},          // 0.0.0.0/8 (except 0.0.0.0/32)
	{[]byte{127, 0, 0, 0}, []byte{255, 0, 0, 0}},        // 127.0.0.0/8 loopback
	{[]byte{169, 254, 0, 0}, []byte{255, 255, 0, 0}},    // 169.254.0.0/16 link-local
	{[]byte{192, 0, 0, 0}, []byte{255, 255, 255, 0}},    // 192.0.0.0/24 IETF protocol
	{[]byte{192, 0, 2, 0}, []byte{255, 255, 255, 0}},    // 192.0.2.0/24 TEST-NET-1
	{[]byte{198, 51, 100, 0}, []byte{255, 255, 255, 0}}, // 198.51.100.0/24 TEST-NET-2
	{[]byte{203, 0, 113, 0}, []byte{255, 255, 255, 0}},  // 203.0.113.0/24 TEST-NET-3
	{[]byte{224, 0, 0, 0}, []byte{240, 0, 0, 0}},        // 224.0.0.0/4 multicast
	{[]byte{240, 0, 0, 0}, []byte{240, 0, 0, 0}},        // 240.0.0.0/4 reserved
}

// ApplyProtection applies security protection rules to the firewall.
func (m *Manager) ApplyProtection(cfg *config.ProtectionConfig, wanInterfaces []string) error {
	if cfg == nil {
		return nil
	}
	// Get or create the firewall table (use brand name)
	table := &nftables.Table{
		Name:   brand.LowerName,
		Family: nftables.TableFamilyIPv4,
	}

	// Create a protection chain that runs before other input/forward rules
	protChain := m.conn.AddChain(&nftables.Chain{
		Name:     "protection",
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityRaw, // Run very early
	})

	if err := m.conn.Flush(); err != nil {
		return err
	}

	// Invalid packet filtering
	if cfg.InvalidPackets {
		m.addInvalidPacketRule(protChain)
	}

	// Anti-spoofing on WAN interfaces
	if cfg.AntiSpoofing {
		for _, wanIface := range wanInterfaces {
			m.addAntiSpoofingRules(protChain, wanIface)
		}
	}

	// Bogon filtering on WAN interfaces
	if cfg.BogonFiltering {
		for _, wanIface := range wanInterfaces {
			m.addBogonRules(protChain, wanIface)
		}
	}

	// SYN flood protection
	if cfg.SynFloodProtection {
		rate := cfg.SynFloodRate
		if rate == 0 {
			rate = 25
		}
		burst := cfg.SynFloodBurst
		if burst == 0 {
			burst = 50
		}
		m.addSynFloodProtection(protChain, uint64(rate), uint32(burst))
	}

	// ICMP rate limiting
	if cfg.ICMPRateLimit {
		rate := cfg.ICMPRate
		if rate == 0 {
			rate = 10
		}
		m.addICMPRateLimit(protChain, uint64(rate))
	}

	return m.conn.Flush()
}

// addInvalidPacketRule drops packets in INVALID connection tracking state
func (m *Manager) addInvalidPacketRule(chain *nftables.Chain) {
	// ct state invalid drop
	m.conn.AddRule(&nftables.Rule{
		Table: chain.Table,
		Chain: chain,
		Exprs: []expr.Any{
			&expr.Ct{Key: expr.CtKeySTATE, Register: 1},
			&expr.Bitwise{
				SourceRegister: 1,
				DestRegister:   1,
				Len:            4,
				Mask:           hostEndianBytes(1), // CT_STATE_INVALID = 1
				Xor:            []byte{0, 0, 0, 0},
			},
			&expr.Cmp{
				Op:       expr.CmpOpNeq,
				Register: 1,
				Data:     []byte{0, 0, 0, 0},
			},
			&expr.Counter{},
			&expr.Verdict{Kind: expr.VerdictDrop},
		},
	})
}

// addAntiSpoofingRules drops packets with private source IPs on WAN interface
func (m *Manager) addAntiSpoofingRules(chain *nftables.Chain, wanIface string) {
	for _, net := range privateNetworks {
		m.conn.AddRule(&nftables.Rule{
			Table: chain.Table,
			Chain: chain,
			Exprs: []expr.Any{
				// Match input interface
				&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     pad(wanIface),
				},
				// Load source IP
				&expr.Payload{
					DestRegister: 1,
					Base:         expr.PayloadBaseNetworkHeader,
					Offset:       12, // Source IP offset
					Len:          4,
				},
				// Apply network mask
				&expr.Bitwise{
					SourceRegister: 1,
					DestRegister:   1,
					Len:            4,
					Mask:           net.mask,
					Xor:            []byte{0, 0, 0, 0},
				},
				// Compare to network address
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     net.ip,
				},
				&expr.Counter{},
				&expr.Log{
					Key:  1,
					Data: []byte("SPOOFED-SRC: "),
				},
				&expr.Verdict{Kind: expr.VerdictDrop},
			},
		})
	}
}

// addBogonRules drops packets from reserved/invalid IP ranges
func (m *Manager) addBogonRules(chain *nftables.Chain, wanIface string) {
	for _, net := range bogonNetworks {
		m.conn.AddRule(&nftables.Rule{
			Table: chain.Table,
			Chain: chain,
			Exprs: []expr.Any{
				// Match input interface
				&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     pad(wanIface),
				},
				// Load source IP
				&expr.Payload{
					DestRegister: 1,
					Base:         expr.PayloadBaseNetworkHeader,
					Offset:       12,
					Len:          4,
				},
				// Apply network mask
				&expr.Bitwise{
					SourceRegister: 1,
					DestRegister:   1,
					Len:            4,
					Mask:           net.mask,
					Xor:            []byte{0, 0, 0, 0},
				},
				// Compare to network address
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     net.ip,
				},
				&expr.Counter{},
				&expr.Verdict{Kind: expr.VerdictDrop},
			},
		})
	}
}

// addSynFloodProtection limits the rate of TCP SYN packets
func (m *Manager) addSynFloodProtection(chain *nftables.Chain, rate uint64, burst uint32) {
	// Match TCP SYN packets (new connections) and rate limit
	m.conn.AddRule(&nftables.Rule{
		Table: chain.Table,
		Chain: chain,
		Exprs: []expr.Any{
			// TCP protocol
			&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     []byte{byte(unix.IPPROTO_TCP)},
			},
			// TCP flags: SYN set, ACK/RST/FIN not set
			&expr.Payload{
				DestRegister: 1,
				Base:         expr.PayloadBaseTransportHeader,
				Offset:       13, // TCP flags offset
				Len:          1,
			},
			&expr.Bitwise{
				SourceRegister: 1,
				DestRegister:   1,
				Len:            1,
				Mask:           []byte{0x17}, // SYN|ACK|RST|FIN
				Xor:            []byte{0x00},
			},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     []byte{0x02}, // Only SYN
			},
			// Rate limit
			&expr.Limit{
				Type:  expr.LimitTypePkts,
				Rate:  rate,
				Unit:  expr.LimitTimeSecond,
				Burst: burst,
				Over:  true, // Match packets OVER the limit
			},
			&expr.Counter{},
			&expr.Verdict{Kind: expr.VerdictDrop},
		},
	})
}

// addICMPRateLimit limits ICMP packets to prevent ping floods
func (m *Manager) addICMPRateLimit(chain *nftables.Chain, rate uint64) {
	m.conn.AddRule(&nftables.Rule{
		Table: chain.Table,
		Chain: chain,
		Exprs: []expr.Any{
			// ICMP protocol
			&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     []byte{byte(unix.IPPROTO_ICMP)},
			},
			// Rate limit
			&expr.Limit{
				Type:  expr.LimitTypePkts,
				Rate:  rate,
				Unit:  expr.LimitTimeSecond,
				Burst: uint32(rate * 2),
				Over:  true,
			},
			&expr.Counter{},
			&expr.Verdict{Kind: expr.VerdictDrop},
		},
	})
}
