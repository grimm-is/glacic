//go:build linux
// +build linux

package firewall

import (
	"fmt"
	"net"
	"regexp"
	"sync"

	"github.com/google/nftables"
)

var validSetNameRegexNative = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func isValidSetNameNative(name string) bool {
	return validSetNameRegexNative.MatchString(name)
}

// NativeIPSetManager handles nftables set operations using the native library.
// This replaces the shell-based IPSetManager for improved testability and robustness.
type NativeIPSetManager struct {
	conn      NFTablesConn
	tableName string
	table     *nftables.Table
	sets      map[string]*nftables.Set // Cache of set references
	mu        sync.RWMutex
}

// NewNativeIPSetManager creates a new native IPSet manager.
func NewNativeIPSetManager(conn NFTablesConn, tableName string) *NativeIPSetManager {
	return &NativeIPSetManager{
		conn:      conn,
		tableName: tableName,
		sets:      make(map[string]*nftables.Set),
	}
}

// SetConn sets the nftables connection (for testing).
func (m *NativeIPSetManager) SetConn(conn NFTablesConn) {
	m.conn = conn
}

// getTable returns the table reference, finding it if needed.
func (m *NativeIPSetManager) getTable() (*nftables.Table, error) {
	if m.table != nil {
		return m.table, nil
	}

	tables, err := m.conn.ListTables()
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}

	for _, t := range tables {
		if t.Name == m.tableName && t.Family == nftables.TableFamilyINet {
			m.table = t
			return t, nil
		}
	}

	return nil, fmt.Errorf("table %s not found", m.tableName)
}

// getSet returns a cached set reference or finds it.
func (m *NativeIPSetManager) getSet(name string) (*nftables.Set, error) {
	m.mu.RLock()
	if s, ok := m.sets[name]; ok {
		m.mu.RUnlock()
		return s, nil
	}
	m.mu.RUnlock()

	table, err := m.getTable()
	if err != nil {
		return nil, err
	}

	sets, err := m.conn.GetSets(table)
	if err != nil {
		return nil, fmt.Errorf("failed to get sets: %w", err)
	}

	for _, s := range sets {
		if s.Name == name {
			m.mu.Lock()
			m.sets[name] = s
			m.mu.Unlock()
			return s, nil
		}
	}

	return nil, fmt.Errorf("set %s not found", name)
}

// setTypeToNft converts our SetType to nftables.SetDatatype.
func setTypeToNft(setType SetType) nftables.SetDatatype {
	switch setType {
	case SetTypeIPv4Addr:
		return nftables.TypeIPAddr
	case SetTypeIPv6Addr:
		return nftables.TypeIP6Addr
	case SetTypeInetService:
		return nftables.TypeInetService
	default:
		return nftables.TypeIPAddr
	}
}

// CreateSet creates a new nftables set.
func (m *NativeIPSetManager) CreateSet(name string, setType SetType, flags ...string) error {
	if !isValidSetNameNative(name) {
		return fmt.Errorf("invalid set name: %s", name)
	}

	table, err := m.getTable()
	if err != nil {
		return err
	}

	set := &nftables.Set{
		Name:    name,
		Table:   table,
		KeyType: setTypeToNft(setType),
	}

	// Handle flags
	for _, f := range flags {
		switch f {
		case "interval":
			set.Interval = true
		case "timeout":
			set.HasTimeout = true
		case "constant":
			set.Constant = true
		}
	}

	if err := m.conn.AddSet(set, nil); err != nil {
		return fmt.Errorf("failed to add set: %w", err)
	}

	if err := m.conn.Flush(); err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}

	// Cache the set
	m.mu.Lock()
	m.sets[name] = set
	m.mu.Unlock()

	return nil
}

// DeleteSet removes an nftables set.
func (m *NativeIPSetManager) DeleteSet(name string) error {
	if !isValidSetNameNative(name) {
		return fmt.Errorf("invalid set name: %s", name)
	}

	set, err := m.getSet(name)
	if err != nil {
		return err
	}

	m.conn.DelSet(set)

	if err := m.conn.Flush(); err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}

	// Remove from cache
	m.mu.Lock()
	delete(m.sets, name)
	m.mu.Unlock()

	return nil
}

// FlushSet removes all elements from a set.
func (m *NativeIPSetManager) FlushSet(name string) error {
	set, err := m.getSet(name)
	if err != nil {
		return err
	}

	m.conn.FlushSet(set)

	return m.conn.Flush()
}

// AddElements adds elements to an existing set.
func (m *NativeIPSetManager) AddElements(setName string, elements []string) error {
	if len(elements) == 0 {
		return nil
	}

	set, err := m.getSet(setName)
	if err != nil {
		return err
	}

	// Convert string elements to SetElement
	setElements := make([]nftables.SetElement, 0, len(elements))
	for _, elem := range elements {
		ip := net.ParseIP(elem)
		if ip == nil {
			// Try parsing as CIDR
			_, ipnet, err := net.ParseCIDR(elem)
			if err != nil {
				continue // Skip invalid entries
			}
			// For interval sets, add start and end
			setElements = append(setElements, nftables.SetElement{
				Key:         ipnet.IP,
				IntervalEnd: false,
			})
			// Calculate end of range
			endIP := make(net.IP, len(ipnet.IP))
			copy(endIP, ipnet.IP)
			for i := len(endIP) - 1; i >= 0; i-- {
				endIP[i] |= ^ipnet.Mask[i]
			}
			// Increment for exclusive end
			for i := len(endIP) - 1; i >= 0; i-- {
				endIP[i]++
				if endIP[i] != 0 {
					break
				}
			}
			setElements = append(setElements, nftables.SetElement{
				Key:         endIP,
				IntervalEnd: true,
			})
		} else {
			if ip.To4() != nil {
				ip = ip.To4()
			}
			setElements = append(setElements, nftables.SetElement{Key: ip})
		}
	}

	if len(setElements) == 0 {
		return nil
	}

	if err := m.conn.SetAddElements(set, setElements); err != nil {
		return fmt.Errorf("failed to add elements: %w", err)
	}

	return m.conn.Flush()
}

// RemoveElements removes elements from a set.
func (m *NativeIPSetManager) RemoveElements(setName string, elements []string) error {
	if len(elements) == 0 {
		return nil
	}

	set, err := m.getSet(setName)
	if err != nil {
		return err
	}

	// Convert string elements to SetElement
	setElements := make([]nftables.SetElement, 0, len(elements))
	for _, elem := range elements {
		ip := net.ParseIP(elem)
		if ip == nil {
			continue // Skip invalid entries
		}
		if ip.To4() != nil {
			ip = ip.To4()
		}
		setElements = append(setElements, nftables.SetElement{Key: ip})
	}

	if len(setElements) == 0 {
		return nil
	}

	if err := m.conn.SetDeleteElements(set, setElements); err != nil {
		return fmt.Errorf("failed to delete elements: %w", err)
	}

	return m.conn.Flush()
}

// ReloadSet atomically replaces a set's contents.
func (m *NativeIPSetManager) ReloadSet(name string, elements []string) error {
	if !isValidSetNameNative(name) {
		return fmt.Errorf("invalid set name: %s", name)
	}

	set, err := m.getSet(name)
	if err != nil {
		return err
	}

	// Flush existing elements
	m.conn.FlushSet(set)

	// Add new elements if any
	if len(elements) > 0 {
		setElements := make([]nftables.SetElement, 0, len(elements))
		for _, elem := range elements {
			ip := net.ParseIP(elem)
			if ip == nil {
				_, ipnet, err := net.ParseCIDR(elem)
				if err != nil {
					continue
				}
				setElements = append(setElements, nftables.SetElement{
					Key:         ipnet.IP,
					IntervalEnd: false,
				})
				endIP := make(net.IP, len(ipnet.IP))
				copy(endIP, ipnet.IP)
				for i := len(endIP) - 1; i >= 0; i-- {
					endIP[i] |= ^ipnet.Mask[i]
				}
				for i := len(endIP) - 1; i >= 0; i-- {
					endIP[i]++
					if endIP[i] != 0 {
						break
					}
				}
				setElements = append(setElements, nftables.SetElement{
					Key:         endIP,
					IntervalEnd: true,
				})
			} else {
				if ip.To4() != nil {
					ip = ip.To4()
				}
				setElements = append(setElements, nftables.SetElement{Key: ip})
			}
		}

		if len(setElements) > 0 {
			if err := m.conn.SetAddElements(set, setElements); err != nil {
				return fmt.Errorf("failed to add elements: %w", err)
			}
		}
	}

	// Atomic commit
	return m.conn.Flush()
}

// GetSetElements returns all elements in a set.
func (m *NativeIPSetManager) GetSetElements(setName string) ([]string, error) {
	set, err := m.getSet(setName)
	if err != nil {
		return nil, err
	}

	elements, err := m.conn.GetSetElements(set)
	if err != nil {
		return nil, fmt.Errorf("failed to get elements: %w", err)
	}

	result := make([]string, 0, len(elements))
	for _, elem := range elements {
		if elem.IntervalEnd {
			continue // Skip interval end markers
		}
		// elem.Key is []byte for IP addresses
		if len(elem.Key) > 0 {
			result = append(result, net.IP(elem.Key).String())
		}
	}

	return result, nil
}

// CheckElement checks if a single element exists in a set.
func (m *NativeIPSetManager) CheckElement(setName, element string) (bool, error) {
	elements, err := m.GetSetElements(setName)
	if err != nil {
		return false, err
	}

	for _, e := range elements {
		if e == element {
			return true, nil
		}
	}
	return false, nil
}

// ListSets returns all sets in the table.
func (m *NativeIPSetManager) ListSets() ([]string, error) {
	table, err := m.getTable()
	if err != nil {
		return nil, err
	}

	sets, err := m.conn.GetSets(table)
	if err != nil {
		return nil, fmt.Errorf("failed to get sets: %w", err)
	}

	names := make([]string, 0, len(sets))
	for _, s := range sets {
		names = append(names, s.Name)
	}

	return names, nil
}
