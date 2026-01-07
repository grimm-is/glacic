// Package ctlplane implements the privileged control plane for Glacic.
//
// # Overview
//
// The control plane runs as root and handles all privileged operations:
//   - Firewall rules via nftables
//   - Network configuration via netlink
//   - DHCP and DNS server management
//   - State persistence (SQLite)
//
// # Architecture
//
// The control plane exposes an RPC server over a Unix socket at /run/glacic/ctl.sock.
// The unprivileged API server connects as a client to execute privileged operations.
//
//	API Server (nobody) → RPC Client → Unix Socket → RPC Server (root) → Kernel
//
// # Key Types
//
//   - [Server]: RPC server with all privileged operations
//   - [Client]: RPC client used by the API server
//   - [ControlPlaneClient]: Interface for mocking in tests
//
// # Adding New RPC Methods
//
//  1. Define request/reply types in types.go
//  2. Add method to Server in server.go
//  3. Add client method in client.go
//  4. Add interface method in client_interface.go
//  5. Add mock implementation in client_mock.go
//
// # Example
//
// Starting the server:
//
//	server := ctlplane.NewServer(cfg, configPath)
//	server.SetFirewallManager(fwMgr)
//	server.Start()
//
// Using the client:
//
//	client, err := ctlplane.NewClient()
//	status, err := client.GetStatus()
package ctlplane
