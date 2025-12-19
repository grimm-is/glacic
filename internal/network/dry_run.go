package network

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/vishvananda/netlink"
)

// DryRunExecutor implements CommandExecutor but only logs commands.
type DryRunExecutor struct {
	mu       sync.Mutex
	Commands []string
}

// NewDryRunExecutor creates a new dry run executor.
func NewDryRunExecutor() *DryRunExecutor {
	return &DryRunExecutor{
		Commands: make([]string, 0),
	}
}

// RunCommand logs the command instead of executing it.
func (e *DryRunExecutor) RunCommand(name string, arg ...string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	cmd := fmt.Sprintf("%s %s", name, strings.Join(arg, " "))
	e.Commands = append(e.Commands, cmd)
	return "", nil
}

// DryRunSystemController logs sysctl writes.
type DryRunSystemController struct {
	mu     sync.Mutex
	Writes []string
}

func (s *DryRunSystemController) ReadSysctl(path string) (string, error) {
	return "0", nil
}

func (s *DryRunSystemController) WriteSysctl(path, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Writes = append(s.Writes, fmt.Sprintf("sysctl -w %s=%s", path, value))
	return nil
}

func (s *DryRunSystemController) IsNotExist(err error) bool {
	return false
}

// DryRunNetlinker logs netlink operations.
type DryRunNetlinker struct {
	mu  sync.Mutex
	Ops []string
}

func (n *DryRunNetlinker) log(op string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Ops = append(n.Ops, fmt.Sprintf("ip %s", op))
}

func (n *DryRunNetlinker) LinkByName(name string) (netlink.Link, error) {
	return &netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: name}}, nil
}
func (n *DryRunNetlinker) LinkList() ([]netlink.Link, error) { return nil, nil }
func (n *DryRunNetlinker) LinkSetUp(link netlink.Link) error {
	n.log(fmt.Sprintf("link set %s up", link.Attrs().Name))
	return nil
}
func (n *DryRunNetlinker) LinkSetDown(link netlink.Link) error {
	n.log(fmt.Sprintf("link set %s down", link.Attrs().Name))
	return nil
}
func (n *DryRunNetlinker) LinkSetMTU(link netlink.Link, mtu int) error {
	n.log(fmt.Sprintf("link set %s mtu %d", link.Attrs().Name, mtu))
	return nil
}
func (n *DryRunNetlinker) LinkSetMaster(slave, master netlink.Link) error {
	n.log(fmt.Sprintf("link set %s master %s", slave.Attrs().Name, master.Attrs().Name))
	return nil
}
func (n *DryRunNetlinker) LinkAdd(link netlink.Link) error {
	n.log(fmt.Sprintf("link add %s type %s", link.Attrs().Name, link.Type()))
	return nil
}
func (n *DryRunNetlinker) LinkDel(link netlink.Link) error {
	n.log(fmt.Sprintf("link del %s", link.Attrs().Name))
	return nil
}
func (n *DryRunNetlinker) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return nil, nil
}
func (n *DryRunNetlinker) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	n.log(fmt.Sprintf("addr add %s dev %s", addr.String(), link.Attrs().Name))
	return nil
}
func (n *DryRunNetlinker) AddrDel(link netlink.Link, addr *netlink.Addr) error {
	n.log(fmt.Sprintf("addr del %s dev %s", addr.String(), link.Attrs().Name))
	return nil
}
func (n *DryRunNetlinker) RouteList(link netlink.Link, family int) ([]netlink.Route, error) {
	return nil, nil
}
func (n *DryRunNetlinker) RouteAdd(route *netlink.Route) error {
	n.log(fmt.Sprintf("route add %s", route.String()))
	return nil
}
func (n *DryRunNetlinker) RouteDel(route *netlink.Route) error {
	n.log(fmt.Sprintf("route del %s", route.String()))
	return nil
}
func (n *DryRunNetlinker) RuleList(family int) ([]netlink.Rule, error) { return nil, nil }
func (n *DryRunNetlinker) RuleAdd(rule *netlink.Rule) error {
	n.log(fmt.Sprintf("rule add %s", rule.String()))
	return nil
}
func (n *DryRunNetlinker) RuleDel(rule *netlink.Rule) error {
	n.log(fmt.Sprintf("rule del %s", rule.String()))
	return nil
}
func (n *DryRunNetlinker) ParseAddr(s string) (*netlink.Addr, error) {
	return netlink.ParseAddr(s)
}
func (n *DryRunNetlinker) ParseIPNet(s string) (*net.IPNet, error) {
	return netlink.ParseIPNet(s)
}
