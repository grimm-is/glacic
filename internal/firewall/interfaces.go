//go:build linux
// +build linux

package firewall

import (
	"github.com/google/nftables"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/services"
)

// FirewallManager defines the interface for managing the firewall.
// It extends the basic Service interface with firewall-specific operations.
type FirewallManager interface {
	services.Service
	ApplyConfig(cfg *Config) error
	AddDynamicNATRule(rule config.NATRule) error
	RemoveDynamicNATRule(match func(config.NATRule) bool) error
}

// NFTablesConn abstracts nftables.Conn operations for testing.
// This interface allows mocking nftables operations on non-Linux systems.
type NFTablesConn interface {
	// Table operations
	AddTable(t *nftables.Table) *nftables.Table
	DelTable(t *nftables.Table)
	ListTables() ([]*nftables.Table, error)

	// Chain operations
	AddChain(c *nftables.Chain) *nftables.Chain
	DelChain(c *nftables.Chain)
	ListChains() ([]*nftables.Chain, error)
	ListChainsOfTableFamily(family nftables.TableFamily) ([]*nftables.Chain, error)

	// Rule operations
	AddRule(r *nftables.Rule) *nftables.Rule
	DelRule(r *nftables.Rule) error
	GetRules(t *nftables.Table, c *nftables.Chain) ([]*nftables.Rule, error)
	InsertRule(r *nftables.Rule) *nftables.Rule

	// Set operations
	AddSet(s *nftables.Set, vals []nftables.SetElement) error
	DelSet(s *nftables.Set)
	GetSets(t *nftables.Table) ([]*nftables.Set, error)
	GetSetElements(s *nftables.Set) ([]nftables.SetElement, error)
	SetAddElements(s *nftables.Set, vals []nftables.SetElement) error
	SetDeleteElements(s *nftables.Set, vals []nftables.SetElement) error
	FlushSet(s *nftables.Set)

	// Commit changes
	Flush() error
}

// RealNFTablesConn wraps the actual nftables.Conn.
// This is used in production on Linux systems.
type RealNFTablesConn struct {
	conn *nftables.Conn
}

// NewRealNFTablesConn creates a new RealNFTablesConn wrapping an nftables.Conn.
func NewRealNFTablesConn(conn *nftables.Conn) *RealNFTablesConn {
	return &RealNFTablesConn{conn: conn}
}

func (r *RealNFTablesConn) AddTable(t *nftables.Table) *nftables.Table {
	return r.conn.AddTable(t)
}

func (r *RealNFTablesConn) DelTable(t *nftables.Table) {
	r.conn.DelTable(t)
}

func (r *RealNFTablesConn) ListTables() ([]*nftables.Table, error) {
	return r.conn.ListTables()
}

func (r *RealNFTablesConn) AddChain(c *nftables.Chain) *nftables.Chain {
	return r.conn.AddChain(c)
}

func (r *RealNFTablesConn) DelChain(c *nftables.Chain) {
	r.conn.DelChain(c)
}

func (r *RealNFTablesConn) ListChains() ([]*nftables.Chain, error) {
	return r.conn.ListChains()
}

func (r *RealNFTablesConn) ListChainsOfTableFamily(family nftables.TableFamily) ([]*nftables.Chain, error) {
	return r.conn.ListChainsOfTableFamily(family)
}

func (r *RealNFTablesConn) AddRule(rule *nftables.Rule) *nftables.Rule {
	return r.conn.AddRule(rule)
}

func (r *RealNFTablesConn) DelRule(rule *nftables.Rule) error {
	return r.conn.DelRule(rule)
}

func (r *RealNFTablesConn) GetRules(t *nftables.Table, c *nftables.Chain) ([]*nftables.Rule, error) {
	return r.conn.GetRules(t, c)
}

func (r *RealNFTablesConn) InsertRule(rule *nftables.Rule) *nftables.Rule {
	return r.conn.InsertRule(rule)
}

func (r *RealNFTablesConn) AddSet(s *nftables.Set, vals []nftables.SetElement) error {
	return r.conn.AddSet(s, vals)
}

func (r *RealNFTablesConn) DelSet(s *nftables.Set) {
	r.conn.DelSet(s)
}

func (r *RealNFTablesConn) GetSets(t *nftables.Table) ([]*nftables.Set, error) {
	return r.conn.GetSets(t)
}

func (r *RealNFTablesConn) GetSetElements(s *nftables.Set) ([]nftables.SetElement, error) {
	return r.conn.GetSetElements(s)
}

func (r *RealNFTablesConn) SetAddElements(s *nftables.Set, vals []nftables.SetElement) error {
	return r.conn.SetAddElements(s, vals)
}

func (r *RealNFTablesConn) SetDeleteElements(s *nftables.Set, vals []nftables.SetElement) error {
	return r.conn.SetDeleteElements(s, vals)
}

func (r *RealNFTablesConn) FlushSet(s *nftables.Set) {
	r.conn.FlushSet(s)
}

func (r *RealNFTablesConn) Flush() error {
	return r.conn.Flush()
}

// QdiscController abstracts netlink qdisc/class/filter operations for QoS.
// This allows mocking traffic control operations on non-Linux systems.
type QdiscController interface {
	// Qdisc operations
	QdiscList(link interface{}) ([]interface{}, error)
	QdiscAdd(qdisc interface{}) error
	QdiscDel(qdisc interface{}) error

	// Class operations
	ClassAdd(class interface{}) error
	ClassDel(class interface{}) error
	ClassList(link interface{}, parent uint32) ([]interface{}, error)

	// Filter operations
	FilterAdd(filter interface{}) error
	FilterDel(filter interface{}) error
	FilterList(link interface{}, parent uint32) ([]interface{}, error)

	// Link operations
	LinkByName(name string) (interface{}, error)
}

// CommandRunner abstracts shell command execution.
// Used by IPSetManager for nft commands.
type CommandRunner interface {
	Run(name string, args ...string) error
	RunInput(input string, name string, args ...string) error
	Output(name string, args ...string) ([]byte, error)
}

// RealCommandRunner executes actual shell commands.
// Methods are implemented in command_linux.go and command_stub.go
type RealCommandRunner struct{}

// DefaultCommandRunner is the default command runner.
var DefaultCommandRunner CommandRunner = &RealCommandRunner{}
