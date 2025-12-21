# Glacic

Single-binary Linux firewall/router built in Go, managing Linux kernel networking directly via netlink and nftables.

> **⚠️ Project Status: Active Development**
> Most features are at L3 (Functional) to L4 (Integration Tested). Not yet production-ready.
> See [FEATURES.md](FEATURES.md) for detailed maturity levels.

## Working Features (L4-L5)

These features work reliably and have integration test coverage:

- **Zone-Based Firewall** ✅ - Zones, policies, stateful tracking (nftables)
- **Interface Management** 🟩 - Static IPs, DHCP client, enable/disable
- **Routing** 🟩 - Static routes, default gateway
- **NAT** 🟩 - Masquerade for outbound traffic
- **DHCP Server** 🟩 - Lease allocation, persistence, static reservations
- **DNS Server** 🟩 - Forwarding, caching, local records
- **HCL Configuration** ✅ - Parsing, validation, hot reload
- **API** 🟩 - REST API with authentication, CLI over API.
- **Privilege Separation** ✅ - Unprivileged API in chroot/netns
- **State Store** 🟩 - SQLite with service buckets

## Functional Features (L3)

These work for happy-path scenarios but have limited testing:

- **VLAN/Bonding** - Config parsing works, runtime untested
- **DNAT/Port Forwarding** - Works, limited testing
- **DNS Blocklists** - File-based hosts format works
- **DHCP DNS Integration** - Hostname→IP registration
- **Web UI** - Most pages at least partially functional
- **Tailscale Status** - Works if tailscaled running
- **Seamless Upgrades** - Socket handoff works (note: FRR/VPN state not serialized)
- **Multi-WAN** - Health checks, failover, load balancing (unit tested)
- **WireGuard** - Full native implementation via netlink/wgctrl

## Incomplete Features (L1-L2)

Present in config but not fully implemented:

- **GeoIP/App-ID Filtering** - Designed, no runtime
- **TLS for API** - Config structures only
- **DNS-over-HTTPS** - Config only
- **HA Replication** - Database sync works (Hot Standby, requires external VRRP for auto-failover)
- **Rule Learning** - UI works, backend uses stubs
- **Terminal UI** - Scaffolded, not functional

## Documentation

- [**Quick Reference**](AGENTS.md) - Fast-start guide for developers and AI agents
- [Features](FEATURES.md) - Detailed feature list and roadmap
- [Architecture](docs/ARCHITECTURE.md) - System design and config/state separation
- [Developer Guide](DEVELOPMENT.md) - Build instructions and code structure
- [API Reference](API.md) - REST API endpoints
- [Demo Guide](DEMO.md) - How to run the QEMU demo environment

## Quick Start

### Prerequisites

- Go 1.21+
- Node.js 18+ (for UI)
- jq (for brand configuration)
- QEMU (for VM testing)

### Build

```bash
# Build everything (UI + Linux binary)
make build

# Or build components separately
make build-ui      # Build Svelte UI
make build-go      # Build native binary
make build-linux   # Cross-compile for Linux
```

### Development

```bash
# Show all available commands
make help

# Start development VM with UI at http://localhost:8080
make dev

# Run TUI demo (mock data, no VM required)
make demo

# Run Web UI in dev mode with hot reload
make demo-web
```

### Testing

```bash
# Run unit tests
make test

# Run integration tests in VM
make test-int

# Run all tests
make test-all

# Run linters
make lint
```

### VM Management

```bash
# Setup Alpine VM image (first time)
make vm-setup

# Start/stop VM
make vm-start
make vm-stop
```

## CLI Usage

```bash
# Start the firewall daemon
glacic start                          # Background mode
glacic start --foreground             # Foreground (debug)
glacic start --dryrun                 # Dry run (show what would happen)

# Management
glacic stop                           # Stop daemon
glacic reload                         # Hot reload configuration
glacic status                         # Show daemon status
glacic api generate --name "monitor" --preset readonly
glacic upgrade                        # Seamless upgrade with socket handoff
glacic upgrade \
  --remote https://glacic.lan:8443 \
  --api-key <api-key> \
   glacic-binary
```

### Commands

| Command | Description |
|---------|-------------|
| `start` | Start firewall daemon (`--foreground`, `--dryrun`, `--config`) |
| `stop` | Stop running daemon |
| `reload` | Hot reload configuration |
| `status` | Show daemon status |
| `api` | Manage API keys (`generate`, `list`, `revoke`) |
| `config` | Manage configuration (`show`, `edit`, `validate`, `export`) |
| `ipset` | Manage IPSet blocklists (`list`, `update`, `add`, `remove`) |
| `check` | Validate configuration file (`-v` for verbose) |
| `show` | Display firewall rules (`--summary`, `--remote`) |
| `log` | View/stream logs (`-f` follow, `-n` lines) |
| `console` | Interactive TUI dashboard |
| `setup` | First-run setup wizard |
| `upgrade` | Seamless upgrade with socket handoff |

### Examples

```bash
glacic api generate --name "monitor" --preset readonly
glacic check -v /etc/glacic/glacic.hcl
glacic show --remote https://192.168.1.1:8080 --api-key xxx
glacic log -f
```

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│              glacic api (uid=nobody, port 8080)             │
│  ┌──────────┐  ┌─────────┐  ┌─────────────┐                 │
│  │ REST API │  │  Web UI │  │  Prometheus │                 │
│  └────┬─────┘  └─────────┘  └─────────────┘                 │
│       │ Unix Socket RPC                                      │
├───────┴─────────────────────────────────────────────────────┤
│              glacic ctl (uid=root)                          │
│  ┌──────────┐  ┌─────────┐  ┌──────┐  ┌──────┐             │
│  │ Netlink  │  │ NFTables │  │ DHCP │  │ DNS  │             │
│  └──────────┘  └─────────┘  └──────┘  └──────┘             │
└─────────────────────────────────────────────────────────────┘
```

## Configuration

Configuration uses HCL (HashiCorp Configuration Language). See example configs:

- `glacic.hcl` - Basic single-interface config
- `zones.hcl` - Multi-zone firewall config
- `zones-single.hcl` - Zone config for single-VM testing

### Example Configuration

```hcl
schema_version = "1.0"
ip_forwarding = true

# WAN Interface (DHCP)
interface "eth0" {
  description = "WAN Link"
  zone        = "WAN"
  dhcp        = true
}

# LAN Interface (Static)
interface "eth1" {
  description = "LAN (Trusted)"
  zone        = "LAN"
  ipv4        = ["192.168.1.1/24"]
}

# Zone definitions
zone "LAN" {
  description = "Local Network"
  management {
    web_ui = true
    api    = true
    ssh    = true
  }
}

# Allow LAN to WAN
policy "LAN" "WAN" {
  name = "lan_to_wan"

  rule "allow_internet" {
    description = "Allow LAN internet access"
    action      = "accept"
  }
}

# Allow LAN to access firewall services
policy "LAN" "self" {
  name = "lan_to_firewall"

  rule "allow_dns" {
    services = ["dns"]
    action   = "accept"
  }

  rule "allow_dhcp" {
    services = ["dhcp"]
    action   = "accept"
  }
}

# NAT for outbound traffic
nat "outbound" {
  type          = "masquerade"
  out_interface = "eth0"
}

# DNS Server
dns {
  enabled    = true
  listen_on  = ["192.168.1.1"]
  forwarders = ["8.8.8.8", "1.1.1.1"]
}

# DHCP Server
dhcp {
  enabled = true

  scope "lan-scope" {
    interface   = "eth1"
    range_start = "192.168.1.100"
    range_end   = "192.168.1.200"
    router      = "192.168.1.1"
    dns         = ["192.168.1.1"]
  }
}
```

## Project Structure

```
├── main.go                    # CLI entry point & subcommand dispatch
├── cmd/                       # Subcommand implementations
│   ├── ctl.go                 # Privileged control plane
│   ├── api.go                 # Unprivileged API server
│   └── ...                    # Other subcommands (test, apply, etc.)
├── internal/
│   ├── api/                   # REST API handlers, middleware, websockets
│   ├── auth/                  # User authentication & session management
│   ├── client/                # HTTP client for remote management
│   ├── clock/                 # Time abstraction for testing
│   ├── config/                # HCL parsing, validation, migration
│   ├── ctlplane/              # RPC interface between ctl & api
│   ├── firewall/              # nftables rule generation, IPSets
│   ├── health/                # Health check subsystem
│   ├── import/                # Config import from other firewalls
│   ├── learning/              # Network learning & flow database
│   ├── logging/               # Structured logging
│   ├── metrics/               # Prometheus metrics collection
│   ├── monitor/               # Gateway/link monitoring
│   ├── network/               # Interface management (netlink)
│   ├── pki/                   # PKI certificate management
│   ├── qos/                   # Traffic shaping (tc)
│   ├── ratelimit/             # Rate limiting
│   ├── routing/               # Routing table management
│   ├── scheduler/             # Task scheduler (cron, interval)
│   ├── state/                 # SQLite state store, replication
│   ├── tls/                   # TLS certificate generation
│   ├── upgrade/               # Seamless binary upgrade system
│   ├── validation/            # Input validation
│   ├── vpn/                   # VPN integrations (Tailscale, WireGuard)
│   └── services/
│       ├── ddns/              # Dynamic DNS client
│       ├── dhcp/              # DHCP server & client
│       ├── discovery/         # Network device discovery
│       ├── dns/               # DNS server (forwarding, blocklists)
│       ├── lldp/              # LLDP discovery
│       ├── mdns/              # mDNS responder
│       ├── ra/                # IPv6 Router Advertisements
│       ├── scanner/           # Active network scanner
│       ├── threatintel/       # Threat intelligence blocklists
│       ├── upnp/              # UPnP/IGD support
│       └── wol/               # Wake-on-LAN
├── ui/                        # Svelte web UI
├── docs/                      # Architecture and design docs
├── tests/                     # Integration test fixtures
├── scripts/                   # Build, test, and VM scripts
├── configs/                   # Example configurations
└── cmd/glacic-builder/        # Alpine VM & ISO builder
```


## License

This project is licensed under the **GNU Affero General Public License v3.0 (AGPL-3.0)**.

### 🛡️ Permanent Source Openness Promise

**This project will never retroactively close its source code.**

- **✅ AGPL v3.0 Forever**: The source code will remain open source under AGPL v3.0 for all existing and future users
- **✅ No Retroactive Changes**: We will never change the license of existing code to restrict access
- **✅ Community Protection**: All contributions and community improvements will remain permanently open
- **✅ Fork Freedom**: Anyone can fork and continue development under AGPL v3.0

### Why AGPL-3.0?

The AGPL-3.0 license ensures that:

- **Commercial exploitation is prevented** - Companies cannot use this code in proprietary products without sharing their modifications
- **SaaS loophole is closed** - Even when used as a service, modifications must be shared with users
- **Community benefits are preserved** - All improvements and enhancements remain open source
- **Contributor protection** - Your contributions will always remain free and open

### What this means:

✅ **Allowed**:
- Personal and commercial use with source sharing
- Modification and distribution
- Community contributions
- Academic and research use

❌ **Not allowed**:
- Proprietary forks without sharing source
- Commercial SaaS without providing source to users
- Incorporation into closed-source products

### 📋 Contributing

All contributors must sign our [Contributor License Agreement (CLA.md)](CLA.md) to ensure dual licensing compatibility and protect the open source nature of the project.

For commercial licensing options or questions about using this code in proprietary products, please contact the project maintainers.

See [LICENSE](LICENSE) for the full license text.
