# Glacic Architecture

This document describes the high-level architecture of Glacic for developers working on the codebase.

## Overview

Glacic is a **single-binary Linux firewall/router** that:
- Manages networking directly via **netlink** (no shell commands)
- Applies firewall rules via **nftables** (kernel native)
- Runs as a **privileged control plane** with an **unprivileged API server**

```
┌─────────────────────────────────────────────────────────────────┐
│                         Linux Kernel                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────────┐ │
│  │ netlink  │  │ nftables │  │ routing  │  │ network devices  │ │
│  └────▲─────┘  └────▲─────┘  └────▲─────┘  └────────▲─────────┘ │
└───────┼─────────────┼─────────────┼─────────────────┼───────────┘
        │             │             │                 │
┌───────┴─────────────┴─────────────┴─────────────────┴───────────┐
│                      glacic ctl (root)                           │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────────────────┐  │
│  │ Firewall │ │ Network  │ │   DHCP   │ │   Control Plane    │  │
│  │ Manager  │ │ Manager  │ │  Server  │ │   (Unix Socket)    │  │
│  └──────────┘ └──────────┘ └──────────┘ └──────────▲─────────┘  │
└──────────────────────────────────────────────────────┼──────────┘
                                                       │ RPC
┌──────────────────────────────────────────────────────┼──────────┐
│                    glacic api (nobody)               │          │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────────┴───────┐  │
│  │   Auth   │  │   REST   │  │    Control Plane Client      │  │
│  │  Store   │  │ Handlers │  │    (calls ctl over RPC)      │  │
│  └──────────┘  └──────────┘  └──────────────────────────────┘  │
│                                                                  │
│  Sandboxed in network namespace 169.254.255.2/30                │
└──────────────────────────────────────────────────────────────────┘
        │
        │ Port 8443 (HTTPS) / 8080 (HTTP)
        ▼
    ┌─────────┐
    │  Users  │  Web UI, CLI, API clients
    └─────────┘
```

---

## Process Model

### `glacic ctl` (Privileged)
- **Runs as**: root
- **Responsibilities**:
  - Firewall rules (nftables)
  - Network configuration (netlink)
  - DHCP/DNS servers (ports 67, 53)
  - State persistence (SQLite)
- **Entry point**: `cmd/ctl.go` → `RunCtl()`
- **RPC server**: Unix socket at `/run/glacic/ctl.sock`

### `glacic api` (Unprivileged)
- **Runs as**: nobody (dropped privileges)
- **Sandboxed**: Network namespace with IP 169.254.255.2
- **Responsibilities**:
  - REST API endpoints
  - Authentication/authorization
  - Web UI serving
  - CSRF protection
- **Entry point**: `cmd/api.go` → `RunAPI()`
- **Communicates with ctl via**: Unix socket RPC (forwarded into sandbox)

### Communication Flow
```
User Request → API Server → RPC Client → Unix Socket → CTL Server → Kernel
```

---

## Package Organization

### Core Packages

```
internal/
├── ctlplane/         # RPC server & client for privilege separation
│   ├── server.go     # RPC method implementations (2200+ LOC - needs split)
│   ├── client.go     # RPC client used by API
│   └── types.go      # Request/reply types
│
├── api/              # REST API server
│   ├── server.go     # HTTP handlers (1800+ LOC - needs split)
│   ├── auth.go       # Authentication logic
│   └── *_handlers.go # Domain-specific handlers
│
├── config/           # HCL configuration parsing
│   ├── config.go     # Main Config struct
│   ├── hcl.go        # HCL parser
│   ├── validate.go   # Validation rules
│   └── loader.go     # File loading with migrations
│
├── firewall/         # nftables management
│   ├── manager_linux.go  # Apply rules atomically
│   ├── script_builder.go # Generate nftables scripts
│   └── safemode.go       # Emergency lockdown rules
│
├── network/          # netlink interface management
│   ├── manager_linux.go  # Interface/IP/route management
│   ├── uplink.go         # Multi-WAN health & failover
│   └── dhcp_client.go    # DHCP client via raw sockets
│
└── services/         # Network services
    ├── dhcp/         # DHCP server
    └── dns/          # DNS server (embedded or dnsmasq)
```

### Supporting Packages

```
internal/
├── state/            # SQLite persistence layer
├── learning/         # Traffic learning engine
├── vpn/              # WireGuard management
├── scheduler/        # Cron-like task scheduling
├── logging/          # Structured logging
├── brand/            # White-label branding
└── upgrade/          # Zero-downtime upgrades
```

---

## Data Flow

### Configuration Loading
```
glacic.hcl → config.LoadFile() → Config struct
                                       │
                   ┌───────────────────┴───────────────────┐
                   ▼                                       ▼
           firewall.ApplyConfig()               network.ApplyInterfaces()
                   │                                       │
                   ▼                                       ▼
             nft -f script                          netlink syscalls
```

### API Request Flow
```
HTTP Request
    │
    ▼
┌─────────────────────────────────────┐
│  Middleware Chain (api/server.go)  │
│  1. AccessLogger                   │
│  2. CSRF Protection                │
│  3. Authentication                 │
│  4. Authorization (RBAC)           │
└─────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────┐
│  Handler (api/*_handlers.go)       │
│  - Validates input                 │
│  - Calls ctlplane.Client           │
└─────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────┐
│  CTL Server (ctlplane/server.go)   │
│  - Executes privileged operation   │
│  - Updates state                   │
│  - Returns result                  │
└─────────────────────────────────────┘
```

---

## Key Design Decisions

### 1. Privilege Separation
The API runs as `nobody` in a sandboxed network namespace. All privileged operations go through RPC to the `ctl` process running as root.

**Why**: Defense in depth. A compromised API cannot directly modify firewall rules.

### 2. Atomic Firewall Updates
All firewall changes are written as a complete nftables script and applied atomically with `nft -f`.

**Why**: No window of vulnerability during rule updates. Either all rules apply or none do.

### 3. Boot to Safe Mode
On startup, safe mode rules are applied **before** loading the full configuration.

**Why**: If config parsing fails, the system is still accessible via LAN for recovery.

### 4. No Shell Commands
All network operations use Go libraries directly:
- `github.com/vishvananda/netlink` for interfaces/routes
- `github.com/google/nftables` for firewall (library mode)
- Raw nftables scripts for atomic apply

**Why**: Reliability, testability, no shell injection risk.

---

## Testing Strategy

### Unit Tests
- Located next to source files (`*_test.go`)
- Mock kernel interfaces
- Run with: `make test`

### Integration Tests
- Located in `t/` directory, organized by category:
  - `t/01-sanity/` - Basic connectivity tests
  - `t/10-api/` - API authentication, CRUD
  - `t/20-dhcp/` - DHCP server tests
  - `t/25-dns/` - DNS server tests
  - `t/30-firewall/` - Firewall rule tests
  - `t/40-network/` - Networking (VLAN, bond, NAT)
  - `t/50-security/` - IPSets, learning engine
  - `t/60-vpn/` - WireGuard, Tailscale
  - `t/70-system/` - Config, upgrade, lifecycle
  - `t/80-monitoring/` - Metrics, nflog
- Run in QEMU VM with real kernel
- Run with: `make test-int`

### Test Infrastructure

Integration tests run in QEMU VMs orchestrated by the **Orca** system:

```
┌─────────────────────────────────────────────────────────────┐
│                    Host (macOS/Linux)                       │
│  ┌───────────────────────────────────────────────────────┐  │
│  │              Orchestrator (orca)                      │  │
│  │  - Manages VM pool (parallel execution)               │  │
│  │  - Distributes tests across VMs                       │  │
│  │  - Collects results, formats output                   │  │
│  └───────────────────┬───────────────────────────────────┘  │
│                      │ vserial / vsock                          │
│  ┌───────────────────┴───────────────────────────────────┐  │
│  │              QEMU VM (Alpine Linux)                    │  │
│  │  ┌─────────────────────────────────────────────────┐  │  │
│  │  │            Toolbox Agent                        │  │  │
│  │  │  - Receives test commands                       │  │  │
│  │  │  - Executes shell test scripts (TAP output)     │  │  │
│  │  │  - Reports results back to orchestrator         │  │  │
│  │  └─────────────────────────────────────────────────┘  │  │
│  │                                                        │  │
│  │  /mnt/glacic/  ← Host directory mounted (9p)          │  │
│  │  /mnt/glacic/build/glacic  ← Test binary              │  │
│  │  /mnt/glacic/t/  ← Test scripts                       │  │
│  └────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────┘
```

**Key Components:**

| Component | Location | Purpose |
|-----------|----------|---------|
| **Orca (orchestrator)** | `internal/toolbox/orca/` | Manages VM pool, distributes tests |
| **Toolbox Agent** | `internal/toolbox/agent/` | Runs inside VM, executes tests |
| **VM Scripts** | `scripts/vm/` | VM setup, entrypoint |
| **Test Harness** | `t/common.sh` | Shared test utilities |

**Architecture Principles:**

1. **Parallel Execution**: Orca can run multiple VMs to speed up test suite
2. **Isolation**: Each test runs in a fresh VM state
3. **Real Kernel**: Tests use actual Linux networking (netlink, nftables)
4. **Host Mounting**: Source code mounted read-only, no rebuild needed in VM

See: [docs/dev/toolbox-orca.md](docs/dev/toolbox-orca.md) for detailed documentation.

### Test Coverage Gaps
See [FEATURES.md](FEATURES.md) for per-feature maturity levels.

---

## Safe Mode

Safe Mode provides an emergency lockdown mechanism when configuration is corrupted or the system is compromised.

### Boot Sequence
```
1. Create firewall manager
2. Apply safe mode rules (secure baseline)
3. Load configuration
4. Apply full firewall rules
   └── If fails: system stays in safe mode
```

### Safe Mode Rules
- **INPUT**: Allow management access (SSH, Web UI, API) from LAN interfaces
- **FORWARD**: **DROP all** (including established connections)
- **OUTPUT**: Allow DNS, ICMP, VPN services (Tailscale, WireGuard)

### Forgiving Config Parser
When HCL parsing fails, the system attempts to salvage the configuration:
1. Identify error location
2. Comment out broken block
3. Retry parsing
4. Track skipped blocks for diff display

See: `internal/config/forgiving.go`

### Safe Mode Hints
Cached metadata for recovery scenarios, saved on every successful config load:
- Per-interface DHCP settings
- Zone mappings (WAN/LAN)
- Management flags

See: `internal/config/safemode_hints.go`

---

## Configuration Schema

### File Format
HCL (HashiCorp Configuration Language) with schema versioning.

### Key Blocks
| Block | Purpose | Example |
|-------|---------|---------|
| `interface` | Network interface config | `interface "eth0" { dhcp = true }` |
| `zone` | Firewall zone definition | `zone "LAN" { interfaces = ["eth1"] }` |
| `policy` | Zone-to-zone rules | `policy "LAN→WAN" { action = "accept" }` |
| `dhcp` | DHCP server scope | `dhcp "lan" { range_start = "..." }` |
| `dns` | DNS server config | `dns { forwarders = ["8.8.8.8"] }` |
| `nat` | NAT/port forwarding | `nat { ... }` |
| `api` | API server settings | `api { enabled = true }` |

### Validation
- `internal/config/validate.go` - Validation rules
- Run `glacic check -v <config>` to validate before applying

---

## Common Patterns

### Adding a New Feature
1. **Config**: Add to `internal/config/config.go` with HCL tags
2. **Validation**: Add rules to `internal/config/validate.go`
3. **RPC**: Add types to `internal/ctlplane/types.go`
4. **RPC**: Add method to `internal/ctlplane/server.go`
5. **Client**: Add method to `internal/ctlplane/client.go`
6. **API**: Add route and handler to `internal/api/server.go`
7. **Test**: Add integration test to `t/` directory

### Error Handling
- Return errors up the call stack (don't log and return)
- Use `fmt.Errorf("context: %w", err)` for wrapping
- Log at the top-level handler

### Logging
```go
import "grimm.is/glacic/internal/logging"

logging.Info("message", "key1", value1, "key2", value2)
logging.Warn("message")
logging.Error("message", "error", err)
```

---

## Quick Reference

| Task | Entry Point |
|------|-------------|
| Start daemon | `glacic ctl` → `cmd/ctl.go` |
| API server | `glacic _api-server` → `cmd/api.go` |
| Add firewall rule | `internal/firewall/script_builder.go` |
| Add API endpoint | See [docs/dev/add-api-endpoint.md](docs/dev/add-api-endpoint.md) |
| Add RPC method | `internal/ctlplane/server.go` + `types.go` |
| Add config field | See [docs/dev/add-config-field.md](docs/dev/add-config-field.md) |
| Run tests | See [docs/dev/run-tests.md](docs/dev/run-tests.md) |

---

## Where Things Are

Quick lookup for common functionality:

| Looking For | Location |
|-------------|----------|
| **Authentication** | `internal/api/auth.go` |
| **API routes** | `internal/api/server.go` → `initRoutes()` |
| **Firewall rules** | `internal/firewall/script_builder.go` |
| **Safe mode** | `internal/firewall/safemode.go` |
| **DHCP server** | `internal/services/dhcp/dhcp_server.go` |
| **DNS server** | `internal/services/dns/dns_server.go` |
| **RPC types** | `internal/ctlplane/types.go` |
| **Config struct** | `internal/config/config.go` |
| **Config validation** | `internal/config/validate.go` |
| **Interface management** | `internal/network/manager_linux.go` |
| **Multi-WAN** | `internal/network/uplink.go` |
| **State persistence** | `internal/state/store.go` |
| **Learning engine** | `internal/learning/engine.go` |
| **WireGuard** | `internal/vpn/wireguard.go` |
| **Upgrade logic** | `internal/upgrade/upgrade.go` |
| **Branding** | `internal/brand/brand.go` |
| **Integration tests** | `t/` directory |

---

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `GLACIC_DEBUG=1` | Enable verbose debug logging |
| `GLACIC_CONFIG` | Override default config path |
| `GLACIC_STATE_DIR` | Override state directory |
| `GLACIC_NO_SANDBOX` | Disable API sandbox (development only) |

---

## Developer Guides

Step-by-step guides for common tasks (also used by AI agents):

- [Adding API Endpoints](docs/dev/add-api-endpoint.md) - Full checklist with code examples
- [Adding Config Fields](docs/dev/add-config-field.md) - HCL configuration settings
- [Running Tests](docs/dev/run-tests.md) - Integration test debugging
- [Troubleshooting](docs/dev/troubleshooting.md) - Common errors and fixes
- [Go Code Standards](docs/dev/go-code-standards.md) - Coding conventions

---

## Next Steps for Developers

1. **First time?** Read this doc, then explore `cmd/ctl.go` and `cmd/api.go`
2. **Adding features?** Check the developer guides for step-by-step walkthroughs
3. **Debugging?** Enable verbose logging with `GLACIC_DEBUG=1`
4. **Testing?** `make test` for unit, `make test-int` for integration

