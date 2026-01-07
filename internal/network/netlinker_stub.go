//go:build !linux
// +build !linux

package network

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

// DefaultNetlinker is the default RealNetlinker instance (stub).
var DefaultNetlinker Netlinker = &RealNetlinker{}

// RealNetlinker is a stub implementation of Netlinker.
type RealNetlinker struct{}

func (r *RealNetlinker) LinkByName(name string) (netlink.Link, error) {
	return nil, fmt.Errorf("LinkByName not supported on this platform")
}

func (r *RealNetlinker) LinkList() ([]netlink.Link, error) {
	return nil, nil
}

func (r *RealNetlinker) LinkSetUp(link netlink.Link) error {
	return nil
}

func (r *RealNetlinker) LinkSetDown(link netlink.Link) error {
	return nil
}

func (r *RealNetlinker) LinkSetMTU(link netlink.Link, mtu int) error {
	return nil
}

func (r *RealNetlinker) LinkSetMaster(slave, master netlink.Link) error {
	return nil
}

func (r *RealNetlinker) LinkAdd(link netlink.Link) error {
	return nil
}

func (r *RealNetlinker) LinkDel(link netlink.Link) error {
	return nil
}

func (r *RealNetlinker) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return nil, nil
}

func (r *RealNetlinker) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	return nil
}

func (r *RealNetlinker) AddrDel(link netlink.Link, addr *netlink.Addr) error {
	return nil
}

func (r *RealNetlinker) RouteList(link netlink.Link, family int) ([]netlink.Route, error) {
	return nil, nil
}

func (r *RealNetlinker) RouteAdd(route *netlink.Route) error {
	return nil
}

func (r *RealNetlinker) RouteDel(route *netlink.Route) error {
	return nil
}

func (r *RealNetlinker) RuleList(family int) ([]netlink.Rule, error) {
	return nil, nil
}

func (r *RealNetlinker) RuleAdd(rule *netlink.Rule) error {
	return nil
}

func (r *RealNetlinker) RuleDel(rule *netlink.Rule) error {
	return nil
}

func (r *RealNetlinker) ParseAddr(s string) (*netlink.Addr, error) {
	return netlink.ParseAddr(s)
}

func (r *RealNetlinker) ParseIPNet(s string) (*net.IPNet, error) {
	_, ipNet, err := net.ParseCIDR(s)
	if err != nil {
		return nil, err
	}
	return ipNet, nil
}
