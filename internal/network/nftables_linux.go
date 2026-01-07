//go:build linux
// +build linux

package network

import (
	"fmt"
	"net"
	"strings"

	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

// NFTManager abstracts nftables operations for uplink management.
type NFTManager interface {
	// AddMarkRule adds packet marking rule (for failover)
	// ctState should be "new" for new connections
	AddMarkRule(chain, srcNet string, ctState string, mark uint32, comment string) error

	// AddNumgenMarkRule adds weighted load-balanced marking using numgen
	AddNumgenMarkRule(chain, srcNet string, weights []NumgenWeight, comment string) error

	// AddConnmarkRestore adds connmark restore rule for sticky connections
	AddConnmarkRestore(chain, iface string) error

	// AddSNAT adds SNAT rule for uplink
	AddSNAT(chain string, mark uint32, oif, snatIP string) error

	// DeleteRulesByComment removes rules with matching comment prefix
	DeleteRulesByComment(chain, commentPrefix string) error

	// Flush commits pending changes
	Flush() error
}

// NumgenWeight represents a weight entry for numgen load balancing
type NumgenWeight struct {
	Mark   uint32
	Weight int
}

// RealNFTManager implements NFTManager using google/nftables library
type RealNFTManager struct {
	conn      *nftables.Conn
	table     *nftables.Table
	tableName string
	family    nftables.TableFamily
}

// NewRealNFTManager creates a new NFT manager for the specified table
func NewRealNFTManager(tableName string) (*RealNFTManager, error) {
	conn, err := nftables.New()
	if err != nil {
		return nil, fmt.Errorf("failed to open nftables connection: %w", err)
	}

	// Find or create the table
	tables, err := conn.ListTables()
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}

	var table *nftables.Table
	for _, t := range tables {
		if t.Name == tableName && t.Family == nftables.TableFamilyINet {
			table = t
			break
		}
	}

	if table == nil {
		// Table should already exist (created by firewall manager)
		return nil, fmt.Errorf("table %s not found", tableName)
	}

	return &RealNFTManager{
		conn:      conn,
		table:     table,
		tableName: tableName,
		family:    nftables.TableFamilyINet,
	}, nil
}

// getChain finds a chain by name
func (m *RealNFTManager) getChain(chainName string) (*nftables.Chain, error) {
	chains, err := m.conn.ListChainsOfTableFamily(m.family)
	if err != nil {
		return nil, fmt.Errorf("failed to list chains: %w", err)
	}

	for _, c := range chains {
		if c.Table.Name == m.tableName && c.Name == chainName {
			return c, nil
		}
	}
	return nil, fmt.Errorf("chain %s not found in table %s", chainName, m.tableName)
}

// AddMarkRule adds a rule to mark new connections from srcNet
func (m *RealNFTManager) AddMarkRule(chainName, srcNet string, ctState string, mark uint32, comment string) error {
	chain, err := m.getChain(chainName)
	if err != nil {
		return err
	}

	// Parse source network
	_, ipNet, err := net.ParseCIDR(srcNet)
	if err != nil {
		return fmt.Errorf("invalid source network %s: %w", srcNet, err)
	}

	var exprs []expr.Any

	// Match source IP
	exprs = append(exprs,
		// Load saddr
		&expr.Payload{
			DestRegister: 1,
			Base:         expr.PayloadBaseNetworkHeader,
			Offset:       12, // IPv4 src offset
			Len:          4,
		},
		// Bitwise AND with mask
		&expr.Bitwise{
			SourceRegister: 1,
			DestRegister:   1,
			Len:            4,
			Mask:           ipNet.Mask,
			Xor:            []byte{0, 0, 0, 0},
		},
		// Compare with network address
		&expr.Cmp{
			Op:       expr.CmpOpEq,
			Register: 1,
			Data:     ipNet.IP.To4(),
		},
	)

	// Match connection state
	if ctState != "" {
		var stateBits uint32
		for _, s := range strings.Split(ctState, ",") {
			switch strings.TrimSpace(s) {
			case "new":
				stateBits |= expr.CtStateBitNEW
			case "established":
				stateBits |= expr.CtStateBitESTABLISHED
			case "related":
				stateBits |= expr.CtStateBitRELATED
			}
		}
		exprs = append(exprs,
			&expr.Ct{Key: expr.CtKeySTATE, Register: 1},
			&expr.Bitwise{
				SourceRegister: 1,
				DestRegister:   1,
				Len:            4,
				Mask:           binaryutil.NativeEndian.PutUint32(stateBits),
				Xor:            []byte{0, 0, 0, 0},
			},
			&expr.Cmp{
				Op:       expr.CmpOpNeq,
				Register: 1,
				Data:     []byte{0, 0, 0, 0},
			},
		)
	}

	// Set meta mark
	exprs = append(exprs,
		&expr.Immediate{
			Register: 1,
			Data:     binaryutil.NativeEndian.PutUint32(mark),
		},
		&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
	)

	// Save to conntrack mark
	exprs = append(exprs,
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		&expr.Ct{Key: expr.CtKeyMARK, Register: 1, SourceRegister: true},
	)

	m.conn.AddRule(&nftables.Rule{
		Table:    m.table,
		Chain:    chain,
		Exprs:    exprs,
		UserData: []byte(comment),
	})

	return nil
}

// AddNumgenMarkRule adds weighted load balancing using numgen random
func (m *RealNFTManager) AddNumgenMarkRule(chainName, srcNet string, weights []NumgenWeight, comment string) error {
	chain, err := m.getChain(chainName)
	if err != nil {
		return err
	}

	// Calculate total weight
	totalWeight := 0
	for _, w := range weights {
		totalWeight += w.Weight
	}
	if totalWeight == 0 {
		return fmt.Errorf("total weight is 0")
	}

	// Parse source network
	_, ipNet, err := net.ParseCIDR(srcNet)
	if err != nil {
		return fmt.Errorf("invalid source network %s: %w", srcNet, err)
	}

	// Create anonymous set for numgen mapping
	// Set elements: range -> mark
	var setElements []nftables.SetElement
	pos := uint32(0)
	for _, w := range weights {
		if w.Weight <= 0 {
			continue
		}
		// Range: [pos, pos+weight-1] -> mark
		setElements = append(setElements, nftables.SetElement{
			Key:         binaryutil.NativeEndian.PutUint32(pos),
			KeyEnd:      binaryutil.NativeEndian.PutUint32(pos + uint32(w.Weight) - 1),
			Val:         binaryutil.NativeEndian.PutUint32(w.Mark),
			IntervalEnd: false,
		})
		pos += uint32(w.Weight)
	}

	// Create the anonymous set
	set := &nftables.Set{
		Table:     m.table,
		Anonymous: true,
		Constant:  true,
		KeyType:   nftables.TypeInteger,
		DataType:  nftables.TypeInteger,
		Interval:  true,
	}

	if err := m.conn.AddSet(set, setElements); err != nil {
		return fmt.Errorf("failed to add numgen set: %w", err)
	}

	var exprs []expr.Any

	// Match source IP
	exprs = append(exprs,
		&expr.Payload{
			DestRegister: 1,
			Base:         expr.PayloadBaseNetworkHeader,
			Offset:       12,
			Len:          4,
		},
		&expr.Bitwise{
			SourceRegister: 1,
			DestRegister:   1,
			Len:            4,
			Mask:           ipNet.Mask,
			Xor:            []byte{0, 0, 0, 0},
		},
		&expr.Cmp{
			Op:       expr.CmpOpEq,
			Register: 1,
			Data:     ipNet.IP.To4(),
		},
	)

	// Match ct state new
	exprs = append(exprs,
		&expr.Ct{Key: expr.CtKeySTATE, Register: 1},
		&expr.Bitwise{
			SourceRegister: 1,
			DestRegister:   1,
			Len:            4,
			Mask:           binaryutil.NativeEndian.PutUint32(expr.CtStateBitNEW),
			Xor:            []byte{0, 0, 0, 0},
		},
		&expr.Cmp{
			Op:       expr.CmpOpNeq,
			Register: 1,
			Data:     []byte{0, 0, 0, 0},
		},
	)

	// Generate random number mod totalWeight
	exprs = append(exprs,
		&expr.Numgen{
			Register: 1,
			Modulus:  uint32(totalWeight),
			Type:     unix.NFT_NG_RANDOM,
		},
	)

	// Lookup in set to get mark
	exprs = append(exprs,
		&expr.Lookup{
			SourceRegister: 1,
			DestRegister:   1,
			SetName:        set.Name,
			SetID:          set.ID,
		},
	)

	// Set meta mark from lookup result
	exprs = append(exprs,
		&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
	)

	// Save to conntrack mark
	exprs = append(exprs,
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		&expr.Ct{Key: expr.CtKeyMARK, Register: 1, SourceRegister: true},
	)

	m.conn.AddRule(&nftables.Rule{
		Table:    m.table,
		Chain:    chain,
		Exprs:    exprs,
		UserData: []byte(comment),
	})

	return nil
}

// AddConnmarkRestore adds a rule to restore mark from conntrack for established connections
func (m *RealNFTManager) AddConnmarkRestore(chainName, iface string) error {
	chain, err := m.getChain(chainName)
	if err != nil {
		return err
	}

	ifaceBytes := make([]byte, 16)
	copy(ifaceBytes, iface)

	var exprs []expr.Any

	// Match input interface
	exprs = append(exprs,
		&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
		&expr.Cmp{
			Op:       expr.CmpOpEq,
			Register: 1,
			Data:     ifaceBytes,
		},
	)

	// Match ct state established,related
	exprs = append(exprs,
		&expr.Ct{Key: expr.CtKeySTATE, Register: 1},
		&expr.Bitwise{
			SourceRegister: 1,
			DestRegister:   1,
			Len:            4,
			Mask:           binaryutil.NativeEndian.PutUint32(expr.CtStateBitESTABLISHED | expr.CtStateBitRELATED),
			Xor:            []byte{0, 0, 0, 0},
		},
		&expr.Cmp{
			Op:       expr.CmpOpNeq,
			Register: 1,
			Data:     []byte{0, 0, 0, 0},
		},
	)

	// Restore mark from conntrack: meta mark set ct mark
	exprs = append(exprs,
		&expr.Ct{Key: expr.CtKeyMARK, Register: 1},
		&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
	)

	m.conn.InsertRule(&nftables.Rule{
		Table:    m.table,
		Chain:    chain,
		Exprs:    exprs,
		UserData: []byte("connmark_restore_" + iface),
	})

	return nil
}

// AddSNAT adds a SNAT rule for traffic with specific mark going out an interface
func (m *RealNFTManager) AddSNAT(chainName string, mark uint32, oif, snatIP string) error {
	chain, err := m.getChain(chainName)
	if err != nil {
		return err
	}

	ip := net.ParseIP(snatIP)
	if ip == nil {
		return fmt.Errorf("invalid SNAT IP: %s", snatIP)
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return fmt.Errorf("only IPv4 SNAT supported: %s", snatIP)
	}

	oifBytes := make([]byte, 16)
	copy(oifBytes, oif)

	var exprs []expr.Any

	// Match mark
	exprs = append(exprs,
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		&expr.Cmp{
			Op:       expr.CmpOpEq,
			Register: 1,
			Data:     binaryutil.NativeEndian.PutUint32(mark),
		},
	)

	// Match output interface
	exprs = append(exprs,
		&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1},
		&expr.Cmp{
			Op:       expr.CmpOpEq,
			Register: 1,
			Data:     oifBytes,
		},
	)

	// SNAT to specified IP
	exprs = append(exprs,
		&expr.Immediate{
			Register: 1,
			Data:     ip4,
		},
		&expr.NAT{
			Type:       expr.NATTypeSourceNAT,
			Family:     unix.NFPROTO_IPV4,
			RegAddrMin: 1,
		},
	)

	m.conn.AddRule(&nftables.Rule{
		Table:    m.table,
		Chain:    chain,
		Exprs:    exprs,
		UserData: []byte(fmt.Sprintf("snat_%s_%x", oif, mark)),
	})

	return nil
}

// DeleteRulesByComment removes rules with UserData matching commentPrefix
func (m *RealNFTManager) DeleteRulesByComment(chainName, commentPrefix string) error {
	chain, err := m.getChain(chainName)
	if err != nil {
		return err
	}

	rules, err := m.conn.GetRules(m.table, chain)
	if err != nil {
		return fmt.Errorf("failed to get rules: %w", err)
	}

	for _, rule := range rules {
		if strings.HasPrefix(string(rule.UserData), commentPrefix) {
			if err := m.conn.DelRule(rule); err != nil {
				return fmt.Errorf("failed to delete rule: %w", err)
			}
		}
	}

	return nil
}

// Flush commits all pending changes
func (m *RealNFTManager) Flush() error {
	return m.conn.Flush()
}
