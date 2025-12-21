package network

import (
	"net"

	"github.com/stretchr/testify/mock"
	"github.com/vishvananda/netlink"
)

// MockNetlinker is a mock implementation of the Netlinker interface.
type MockNetlinker struct {
	mock.Mock
}

func (m *MockNetlinker) LinkByName(name string) (netlink.Link, error) {
	// Debugging: Log every call
	// t.Logf("MockNetlinker: LinkByName called with name: %s", name)
	args := m.Called(name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(netlink.Link), args.Error(1)
}
func (m *MockNetlinker) LinkList() ([]netlink.Link, error) {
	args := m.Called()
	return args.Get(0).([]netlink.Link), args.Error(1)
}
func (m *MockNetlinker) LinkSetUp(link netlink.Link) error {
	args := m.Called(link)
	return args.Error(0)
}
func (m *MockNetlinker) LinkSetDown(link netlink.Link) error {
	args := m.Called(link)
	return args.Error(0)
}
func (m *MockNetlinker) LinkSetMTU(link netlink.Link, mtu int) error {
	args := m.Called(link, mtu)
	return args.Error(0)
}
func (m *MockNetlinker) LinkSetMaster(slave, master netlink.Link) error {
	args := m.Called(slave, master)
	return args.Error(0)
}
func (m *MockNetlinker) LinkAdd(link netlink.Link) error {
	args := m.Called(link)
	return args.Error(0)
}
func (m *MockNetlinker) LinkDel(link netlink.Link) error {
	args := m.Called(link)
	return args.Error(0)
}
func (m *MockNetlinker) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	args := m.Called(link, family)
	return args.Get(0).([]netlink.Addr), args.Error(1)
}
func (m *MockNetlinker) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	args := m.Called(link, addr)
	return args.Error(0)
}
func (m *MockNetlinker) AddrDel(link netlink.Link, addr *netlink.Addr) error {
	args := m.Called(link, addr)
	return args.Error(0)
}
func (m *MockNetlinker) RouteList(link netlink.Link, family int) ([]netlink.Route, error) {
	args := m.Called(link, family)
	return args.Get(0).([]netlink.Route), args.Error(1)
}
func (m *MockNetlinker) RouteAdd(route *netlink.Route) error {
	args := m.Called(route)
	return args.Error(0)
}
func (m *MockNetlinker) RouteDel(route *netlink.Route) error {
	args := m.Called(route)
	return args.Error(0)
}
func (m *MockNetlinker) RuleList(family int) ([]netlink.Rule, error) {
	args := m.Called(family)
	return args.Get(0).([]netlink.Rule), args.Error(1)
}
func (m *MockNetlinker) RuleAdd(rule *netlink.Rule) error {
	args := m.Called(rule)
	return args.Error(0)
}
func (m *MockNetlinker) RuleDel(rule *netlink.Rule) error {
	args := m.Called(rule)
	return args.Error(0)
}
func (m *MockNetlinker) ParseAddr(s string) (*netlink.Addr, error) {
	args := m.Called(s)
	return args.Get(0).(*netlink.Addr), args.Error(1)
}
func (m *MockNetlinker) ParseIPNet(s string) (*net.IPNet, error) {
	args := m.Called(s)
	return args.Get(0).(*net.IPNet), args.Error(1)
}

// MockSystemController is a mock implementation of the SystemController interface.
type MockSystemController struct {
	mock.Mock
}

func (m *MockSystemController) ReadSysctl(path string) (string, error) {
	args := m.Called(path)
	return args.String(0), args.Error(1)
}
func (m *MockSystemController) WriteSysctl(path, value string) error {
	args := m.Called(path, value)
	return args.Error(0)
}
func (m *MockSystemController) IsNotExist(err error) bool {
	args := m.Called(err)
	return args.Bool(0)
}

// MockCommandExecutor is a mock implementation of the CommandExecutor interface.
type MockCommandExecutor struct {
	mock.Mock
}

func (m *MockCommandExecutor) RunCommand(name string, arg ...string) (string, error) {
	// Need to handle variadic args for `arg...`
	// Convert arg to an interface{} slice
	var argsSlice []interface{}
	argsSlice = append(argsSlice, name)
	for _, a := range arg {
		argsSlice = append(argsSlice, a)
	}

	args := m.Called(argsSlice...)
	return args.String(0), args.Error(1)
}

// MockNFTManager is a mock implementation of the NFTManager interface.
type MockNFTManager struct {
	mock.Mock
}

func (m *MockNFTManager) AddMarkRule(chain, srcNet string, ctState string, mark uint32, comment string) error {
	args := m.Called(chain, srcNet, ctState, mark, comment)
	return args.Error(0)
}

func (m *MockNFTManager) AddNumgenMarkRule(chain, srcNet string, weights []NumgenWeight, comment string) error {
	args := m.Called(chain, srcNet, weights, comment)
	return args.Error(0)
}

func (m *MockNFTManager) AddConnmarkRestore(chain, iface string) error {
	args := m.Called(chain, iface)
	return args.Error(0)
}

func (m *MockNFTManager) AddSNAT(chain string, mark uint32, oif, snatIP string) error {
	args := m.Called(chain, mark, oif, snatIP)
	return args.Error(0)
}

func (m *MockNFTManager) DeleteRulesByComment(chain, commentPrefix string) error {
	args := m.Called(chain, commentPrefix)
	return args.Error(0)
}

func (m *MockNFTManager) Flush() error {
	args := m.Called()
	return args.Error(0)
}
