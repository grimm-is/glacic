# Architecture

Developer guide for the Glacic codebase.

## Overview

Single-binary Linux firewall with privilege separation:

```
┌─────────────────────────────────────────────────────────────┐
│                         Linux Kernel                         │
│    netlink  │  nftables  │  routing  │  network devices     │
└───────▲─────────────▲────────────────────────────▲──────────┘
        │             │                            │
┌───────┴─────────────┴────────────────────────────┴──────────┐
│                    glacic ctl (root)                         │
│  Firewall │ Network │ DHCP │ DNS │ Learning │ Scheduler     │
│                     Control Plane (Unix Socket)              │
└───────────────────────────────────────▲─────────────────────┘
                                        │ RPC
┌───────────────────────────────────────┴─────────────────────┐
│                 glacic api (nobody, sandboxed)               │
│  REST API │ WebSocket │ Web UI │ Auth │ EventHub            │
│                 Network namespace 169.254.255.2              │
└─────────────────────────────────────────────────────────────┘
        ↑ Via TCP Proxy (in ctl)
    Users (HTTP:8080, HTTPS:8443)
```

## Process Model

| Process | User | Responsibilities |
|---------|------|------------------|
| `glacic ctl` | root | Firewall, networking, services, state |
| `glacic api` | nobody | REST API, auth, Web UI (sandboxed) |

Communication: Unix socket RPC at `/run/glacic/ctl.sock`

## Key Packages

```
internal/
├── ctlplane/       # RPC server/client for privilege separation
├── api/            # REST handlers, middleware, WebSocket
├── config/         # HCL parsing, validation, migration
├── firewall/       # nftables script generation, integrity monitor
├── network/        # netlink interface/route management
├── events/         # Pub/sub EventHub for real-time data
├── state/          # SQLite persistence
├── learning/       # Traffic learning engine (nflog)
└── services/
    ├── dhcp/       # DHCP server
    ├── dns/        # DNS forwarder with blocklists
    ├── upnp/       # UPnP/NAT-PMP
    └── ...         # discovery, lldp, mdns, ra, wol
```

## Design Decisions

### Privilege Separation
API runs as `nobody` in isolated network namespace. All privileged ops go through RPC.

### Atomic Firewall Updates
Rules written as complete nftables script, applied with `nft -f`. No vulnerability window.

### Smart Flush
Dynamic sets (DNS-authorized IPs) persist across reloads. Integrity monitor restores them if kernel state is wiped.

### Boot to Safe Mode
Safe mode rules applied **before** loading config. If parsing fails, system remains accessible.

### No Shell Commands
All network ops use Go libraries:
- `github.com/vishvananda/netlink` - interfaces/routes
- `github.com/google/nftables` - firewall (library mode)

## Request Flow

```
HTTP Request → Middleware (CSRF, Auth) → Handler → RPC Client → CTL → Kernel
```

## Testing

| Type | Location | Command |
|------|----------|---------|
| Unit | `*_test.go` | `make test` |
| Integration | `integration_tests/linux/` | `make test-int` |

Integration tests run in QEMU VMs via the **Orca** orchestrator.

## Quick Reference

| Task | File |
|------|------|
| Add API endpoint | `internal/api/server.go` |
| Add config field | `internal/config/config.go` |
| Add firewall rule | `internal/firewall/script_builder.go` |
| Add RPC method | `internal/ctlplane/server.go` |

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `GLACIC_DEBUG=1` | Verbose logging |
| `GLACIC_CONFIG` | Override config path |
| `GLACIC_NO_SANDBOX` | Disable API sandbox (dev) |
