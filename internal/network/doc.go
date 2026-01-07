// Package network implements Linux network management via netlink.
//
// # Overview
//
// This package manages network interfaces, IP addresses, routes, and related
// configuration using the Linux netlink API directly (no shell commands).
//
// # Key Components
//
//   - [Manager]: Main network manager for interface and route operations
//   - [UplinkManager]: Multi-WAN health monitoring and failover
//   - [PolicyRoutingManager]: Policy-based routing with fwmarks
//   - [DHCPClientManager]: DHCP client for WAN interfaces
//
// # Interface Management
//
// The Manager handles:
//   - Creating/deleting interfaces (VLANs, bonds)
//   - IP address assignment (static or DHCP)
//   - MTU configuration
//   - Interface enable/disable
//
// # Multi-WAN
//
// The UplinkManager provides:
//   - Health checks (ping, HTTP, DNS)
//   - Automatic failover between WAN interfaces
//   - Load balancing (weighted, round-robin, latency-based)
//   - Connection marking for policy routing
//
// # Dependencies
//
// Uses github.com/vishvananda/netlink for all netlink operations.
//
// # Example
//
//	mgr, err := network.NewManager()
//	if err != nil {
//	    return err
//	}
//
//	// Apply interface configuration
//	err = mgr.ApplyInterfaces(cfg.Interfaces)
//
//	// Apply static routes
//	err = mgr.ApplyRoutes(cfg.Routes)
package network
