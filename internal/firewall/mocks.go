//go:build linux
// +build linux

package firewall

import (
	"sync"

	"github.com/google/nftables"
	"github.com/stretchr/testify/mock"
)

// MockNFTablesConn is a mock implementation of NFTablesConn for testing.
type MockNFTablesConn struct {
	mock.Mock
	mu sync.Mutex

	// In-memory state for tracking operations
	tables map[string]*nftables.Table
	chains map[string]*nftables.Chain
	rules  map[string][]*nftables.Rule
	sets   map[string]*nftables.Set
}

// NewMockNFTablesConn creates a new mock nftables connection.
func NewMockNFTablesConn() *MockNFTablesConn {
	return &MockNFTablesConn{
		tables: make(map[string]*nftables.Table),
		chains: make(map[string]*nftables.Chain),
		rules:  make(map[string][]*nftables.Rule),
		sets:   make(map[string]*nftables.Set),
	}
}

func (m *MockNFTablesConn) AddTable(t *nftables.Table) *nftables.Table {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Called(t)
	m.tables[t.Name] = t
	return t
}

func (m *MockNFTablesConn) DelTable(t *nftables.Table) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Called(t)
	delete(m.tables, t.Name)
}

func (m *MockNFTablesConn) ListTables() ([]*nftables.Table, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	args := m.Called()
	if args.Get(0) != nil {
		return args.Get(0).([]*nftables.Table), args.Error(1)
	}
	// Return in-memory tables
	tables := make([]*nftables.Table, 0, len(m.tables))
	for _, t := range m.tables {
		tables = append(tables, t)
	}
	return tables, nil
}

func (m *MockNFTablesConn) AddChain(c *nftables.Chain) *nftables.Chain {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Called(c)
	key := c.Table.Name + "/" + c.Name
	m.chains[key] = c
	return c
}

func (m *MockNFTablesConn) DelChain(c *nftables.Chain) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Called(c)
	key := c.Table.Name + "/" + c.Name
	delete(m.chains, key)
}

func (m *MockNFTablesConn) ListChains() ([]*nftables.Chain, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	args := m.Called()
	if args.Get(0) != nil {
		return args.Get(0).([]*nftables.Chain), args.Error(1)
	}
	chains := make([]*nftables.Chain, 0, len(m.chains))
	for _, c := range m.chains {
		chains = append(chains, c)
	}
	return chains, args.Error(1)
}

func (m *MockNFTablesConn) ListChainsOfTableFamily(family nftables.TableFamily) ([]*nftables.Chain, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	args := m.Called(family)
	if args.Get(0) != nil {
		return args.Get(0).([]*nftables.Chain), args.Error(1)
	}
	chains := make([]*nftables.Chain, 0)
	for _, c := range m.chains {
		if c.Table.Family == family {
			chains = append(chains, c)
		}
	}
	return chains, args.Error(1)
}

func (m *MockNFTablesConn) AddRule(r *nftables.Rule) *nftables.Rule {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Called(r)
	key := r.Table.Name + "/" + r.Chain.Name
	m.rules[key] = append(m.rules[key], r)
	return r
}

func (m *MockNFTablesConn) DelRule(r *nftables.Rule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	args := m.Called(r)
	return args.Error(0)
}

func (m *MockNFTablesConn) GetRules(t *nftables.Table, c *nftables.Chain) ([]*nftables.Rule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	args := m.Called(t, c)
	if args.Get(0) != nil {
		return args.Get(0).([]*nftables.Rule), args.Error(1)
	}
	key := t.Name + "/" + c.Name
	return m.rules[key], args.Error(1)
}

func (m *MockNFTablesConn) InsertRule(r *nftables.Rule) *nftables.Rule {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Called(r)
	key := r.Table.Name + "/" + r.Chain.Name
	// Insert at beginning
	m.rules[key] = append([]*nftables.Rule{r}, m.rules[key]...)
	return r
}

func (m *MockNFTablesConn) AddSet(s *nftables.Set, vals []nftables.SetElement) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	args := m.Called(s, vals)
	m.sets[s.Name] = s
	return args.Error(0)
}

func (m *MockNFTablesConn) DelSet(s *nftables.Set) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Called(s)
	delete(m.sets, s.Name)
}

func (m *MockNFTablesConn) GetSets(t *nftables.Table) ([]*nftables.Set, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	args := m.Called(t)
	if args.Get(0) != nil {
		return args.Get(0).([]*nftables.Set), args.Error(1)
	}
	sets := make([]*nftables.Set, 0, len(m.sets))
	for _, s := range m.sets {
		if s.Table.Name == t.Name {
			sets = append(sets, s)
		}
	}
	return sets, args.Error(1)
}

func (m *MockNFTablesConn) GetSetElements(s *nftables.Set) ([]nftables.SetElement, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	args := m.Called(s)
	if args.Get(0) != nil {
		return args.Get(0).([]nftables.SetElement), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockNFTablesConn) SetAddElements(s *nftables.Set, vals []nftables.SetElement) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	args := m.Called(s, vals)
	return args.Error(0)
}

func (m *MockNFTablesConn) SetDeleteElements(s *nftables.Set, vals []nftables.SetElement) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	args := m.Called(s, vals)
	return args.Error(0)
}

func (m *MockNFTablesConn) FlushSet(s *nftables.Set) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Called(s)
}

func (m *MockNFTablesConn) Flush() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	args := m.Called()
	return args.Error(0)
}

// Helper methods for test assertions

// GetTableCount returns the number of tables.
func (m *MockNFTablesConn) GetTableCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tables)
}

// GetChainCount returns the number of chains.
func (m *MockNFTablesConn) GetChainCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.chains)
}

// GetRuleCount returns the total number of rules.
func (m *MockNFTablesConn) GetRuleCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, rules := range m.rules {
		count += len(rules)
	}
	return count
}

// MockQdiscController is a mock implementation of QdiscController for testing.
type MockQdiscController struct {
	mock.Mock
}

func (m *MockQdiscController) QdiscList(link interface{}) ([]interface{}, error) {
	args := m.Called(link)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]interface{}), args.Error(1)
}

func (m *MockQdiscController) QdiscAdd(qdisc interface{}) error {
	args := m.Called(qdisc)
	return args.Error(0)
}

func (m *MockQdiscController) QdiscDel(qdisc interface{}) error {
	args := m.Called(qdisc)
	return args.Error(0)
}

func (m *MockQdiscController) ClassAdd(class interface{}) error {
	args := m.Called(class)
	return args.Error(0)
}

func (m *MockQdiscController) ClassDel(class interface{}) error {
	args := m.Called(class)
	return args.Error(0)
}

func (m *MockQdiscController) ClassList(link interface{}, parent uint32) ([]interface{}, error) {
	args := m.Called(link, parent)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]interface{}), args.Error(1)
}

func (m *MockQdiscController) FilterAdd(filter interface{}) error {
	args := m.Called(filter)
	return args.Error(0)
}

func (m *MockQdiscController) FilterDel(filter interface{}) error {
	args := m.Called(filter)
	return args.Error(0)
}

func (m *MockQdiscController) FilterList(link interface{}, parent uint32) ([]interface{}, error) {
	args := m.Called(link, parent)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]interface{}), args.Error(1)
}

func (m *MockQdiscController) LinkByName(name string) (interface{}, error) {
	args := m.Called(name)
	return args.Get(0), args.Error(1)
}
