//go:build linux
// +build linux

package network

import (
	"net"
	// Added for RealCommandExecutor
	"github.com/vishvananda/netlink"
)

// NetworkManager defines the interface for managing network configuration.

// Netlinker is an interface that abstracts netlink interactions.
// This allows for mocking netlink calls during unit testing.

// RealNetlinker is a concrete implementation of Netlinker that uses the actual netlink package.
type RealNetlinker struct{}

// LinkByName retrieves a link by name.
func (r *RealNetlinker) LinkByName(name string) (netlink.Link, error) {
	return netlink.LinkByName(name)
}

// LinkList retrieves all links.
func (r *RealNetlinker) LinkList() ([]netlink.Link, error) {
	return netlink.LinkList()
}

// LinkSetUp sets the link up.
func (r *RealNetlinker) LinkSetUp(link netlink.Link) error {
	return netlink.LinkSetUp(link)
}

// LinkSetDown sets the link down.
func (r *RealNetlinker) LinkSetDown(link netlink.Link) error {
	return netlink.LinkSetDown(link)
}

// LinkSetMTU sets the MTU of the link.
func (r *RealNetlinker) LinkSetMTU(link netlink.Link, mtu int) error {
	return netlink.LinkSetMTU(link, mtu)
}

// LinkSetMaster sets the master of a slave link.
func (r *RealNetlinker) LinkSetMaster(slave, master netlink.Link) error {
	return netlink.LinkSetMaster(slave, master)
}

// LinkAdd adds a link.
func (r *RealNetlinker) LinkAdd(link netlink.Link) error {
	return netlink.LinkAdd(link)
}

// LinkDel deletes a link.
func (r *RealNetlinker) LinkDel(link netlink.Link) error {
	return netlink.LinkDel(link)
}

// AddrList retrieves a list of addresses for a link.
func (r *RealNetlinker) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return netlink.AddrList(link, family)
}

// AddrAdd adds an address to a link.
func (r *RealNetlinker) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrAdd(link, addr)
}

// AddrDel deletes an address from a link.
func (r *RealNetlinker) AddrDel(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrDel(link, addr)
}

// RouteList retrieves a list of routes.
func (r *RealNetlinker) RouteList(link netlink.Link, family int) ([]netlink.Route, error) {
	return netlink.RouteList(link, family)
}

// RouteAdd adds a route.
func (r *RealNetlinker) RouteAdd(route *netlink.Route) error {
	return netlink.RouteAdd(route)
}

// RouteDel deletes a route.
func (r *RealNetlinker) RouteDel(route *netlink.Route) error {
	return netlink.RouteDel(route)
}

// RuleAdd adds a rule.
func (r *RealNetlinker) RuleAdd(rule *netlink.Rule) error {
	return netlink.RuleAdd(rule)
}

// RuleDel deletes a rule.
func (r *RealNetlinker) RuleDel(rule *netlink.Rule) error {
	return netlink.RuleDel(rule)
}

// RuleList lists rules.
func (r *RealNetlinker) RuleList(family int) ([]netlink.Rule, error) {
	return netlink.RuleList(family)
}

// ParseAddr parses an IP address string.
func (r *RealNetlinker) ParseAddr(s string) (*netlink.Addr, error) {
	return netlink.ParseAddr(s)
}

// ParseIPNet parses an IP network string.
func (r *RealNetlinker) ParseIPNet(s string) (*net.IPNet, error) {
	_, ipNet, err := net.ParseCIDR(s) // netlink.ParseIPNet is not public
	if err != nil {
		return nil, err
	}
	return ipNet, nil
}
