//go:build linux

package ctlplane

import (
	"fmt"
	"syscall"

	"github.com/vishvananda/netlink"
)

// GetRoutes returns the current kernel routing table
func (s *Server) GetRoutes(_ *Empty, reply *GetRoutesReply) error {
	// 1. Fetch Links for Name Resolution
	links, err := netlink.LinkList()
	if err != nil {
		return fmt.Errorf("failed to list links: %w", err)
	}
	linkMap := make(map[int]string)
	for _, l := range links {
		linkMap[l.Attrs().Index] = l.Attrs().Name
	}

	// 2. Fetch Routes (IPv4 and IPv6)
	// We deliberately ignore errors on individual families if one works
	routes4, err4 := netlink.RouteList(nil, netlink.FAMILY_V4)
	routes6, err6 := netlink.RouteList(nil, netlink.FAMILY_V6)

	if err4 != nil && err6 != nil {
		return fmt.Errorf("failed to list routes: %v / %v", err4, err6)
	}

	allRoutes := append(routes4, routes6...)
	reply.Routes = make([]Route, 0, len(allRoutes))

	for _, r := range allRoutes {
		// Resolve Interface Name
		ifaceName := "*"
		if name, ok := linkMap[r.LinkIndex]; ok {
			ifaceName = name
		}

		// Destination
		dst := "default"
		if r.Dst != nil {
			dst = r.Dst.String()
		}

		// Gateway
		gw := ""
		if r.Gw != nil {
			gw = r.Gw.String()
		}

		// Source preference
		src := ""
		if r.Src != nil {
			src = r.Src.String()
		}

		// Protocol Decoder
		proto := fmt.Sprintf("%d", r.Protocol)
		switch r.Protocol {
		case syscall.RTPROT_UNSPEC:
			proto = "unspec"
		case syscall.RTPROT_REDIRECT:
			proto = "redirect"
		case syscall.RTPROT_KERNEL:
			proto = "kernel"
		case syscall.RTPROT_BOOT:
			proto = "boot"
		case syscall.RTPROT_STATIC:
			proto = "static"
		case 11: // RTPROT_DHCP (commonly 11 or 16 depending on client, usually implicit)
			// proto = "dhcp" (Not standard constant)
		}

		// Scope Decoder
		scope := fmt.Sprintf("%d", r.Scope)
		switch netlink.Scope(r.Scope) {
		case netlink.SCOPE_UNIVERSE:
			scope = "global"
		case netlink.SCOPE_SITE:
			scope = "site"
		case netlink.SCOPE_LINK:
			scope = "link"
		case netlink.SCOPE_HOST:
			scope = "host"
		case netlink.SCOPE_NOWHERE:
			scope = "nowhere"
		}

		reply.Routes = append(reply.Routes, Route{
			Destination: dst,
			Gateway:     gw,
			Interface:   ifaceName,
			Protocol:    proto,
			Metric:      r.Priority,
			Scope:       scope,
			Src:         src,
			Table:       r.Table,
		})
	}

	return nil
}
