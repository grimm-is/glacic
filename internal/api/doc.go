// Package api implements the REST API server for Glacic.
//
// # Overview
//
// The API server runs as an unprivileged user (nobody) in a sandboxed network
// namespace. It handles HTTP/HTTPS requests and forwards privileged operations
// to the control plane via RPC.
//
// # Security Model
//
// The API is sandboxed with:
//   - Dropped privileges (runs as nobody)
//   - Network namespace isolation (169.254.255.2/30)
//   - Chroot jail (optional)
//   - CSRF protection for browser clients
//   - Cookie-based sessions and API key authentication
//
// # Key Components
//
//   - [Server]: Main HTTP server with middleware chain
//   - [AuthMiddleware]: Handles authentication (sessions, API keys)
//   - [SecurityManager]: Fail2ban-style blocking (partial implementation)
//
// # Request Flow
//
//	HTTP Request → Middleware → Handler → ctlplane.Client → CTL Server
//
// # Adding New Endpoints
//
//  1. Create handler function: func (s *Server) handleFoo(w, r)
//  2. Register route in initRoutes() in server.go
//  3. Add corresponding RPC method if privileged operation needed
//
// # Endpoints
//
// Key endpoint groups:
//   - /api/status - System status
//   - /api/interfaces - Network interface CRUD
//   - /api/firewall - Firewall rules and policies
//   - /api/dhcp - DHCP leases and scopes
//   - /api/dns - DNS settings and records
//   - /api/system - Reboot, upgrade, safe mode
//
// # TLS
//
// By default:
//   - HTTPS on port 8443 (self-signed cert generated if missing)
//   - HTTP on port 8080 (redirects to HTTPS)
package api
