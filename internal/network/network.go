package network

import (
	"net"
	"time"

	"github.com/vishvananda/netlink"
	"grimm.is/glacic/internal/config"
)

// NetworkManager defines the interface for managing network configuration.
type NetworkManager interface {
	SetIPForwarding(enable bool) error
	SetupLoopback() error
	InitializeDHCPClientManager(cfg *config.DHCPServer)
	StopDHCPClientManager()
	ApplyInterface(ifaceCfg config.Interface) error
	ApplyStaticRoutes(routes []config.Route) error
	WaitForLinkIP(ifaceName string, timeoutSeconds int) error
	GetDHCPLeases() map[string]LeaseInfo
	ApplyUIDRoutes(routes []config.UIDRouting) error
}

// LeaseInfo represents information about a DHCP lease.
type LeaseInfo struct {
	Interface  string
	IPAddress  net.IP
	SubnetMask net.IP
	Router     net.IP
	DNSServers []net.IP
	LeaseTime  time.Duration
	ObtainedAt time.Time
	ExpiresAt  time.Time
	MAC        string // For static leases
	Hostname   string // From DHCP Option 12
}

// DNSUpdater is an interface for updating the DNS service dynamically.
type DNSUpdater interface {
	UpdateForwarders(forwarders []string)
}

// Netlinker is an interface that abstracts netlink interactions.
type Netlinker interface {
	LinkByName(name string) (netlink.Link, error)
	LinkList() ([]netlink.Link, error)
	LinkSetUp(link netlink.Link) error
	LinkSetDown(link netlink.Link) error
	LinkSetMTU(link netlink.Link, mtu int) error
	LinkSetMaster(slave, master netlink.Link) error
	LinkAdd(link netlink.Link) error
	LinkDel(link netlink.Link) error

	AddrList(link netlink.Link, family int) ([]netlink.Addr, error)
	AddrAdd(link netlink.Link, addr *netlink.Addr) error
	AddrDel(link netlink.Link, addr *netlink.Addr) error

	RouteList(link netlink.Link, family int) ([]netlink.Route, error)
	RouteAdd(route *netlink.Route) error
	RouteDel(route *netlink.Route) error

	RuleList(family int) ([]netlink.Rule, error)
	RuleAdd(rule *netlink.Rule) error
	RuleDel(rule *netlink.Rule) error

	ParseAddr(s string) (*netlink.Addr, error)
	ParseIPNet(s string) (*net.IPNet, error)
}

// SystemController is an interface that abstracts system-level operations like sysctl.
type SystemController interface {
	ReadSysctl(path string) (string, error)
	WriteSysctl(path, value string) error
	IsNotExist(err error) bool
}

// CommandExecutor is an interface that abstracts executing shell commands.
type CommandExecutor interface {
	RunCommand(name string, arg ...string) (string, error)
}
