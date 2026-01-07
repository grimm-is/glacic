# Glacic

Single-binary Linux firewall/router built in Go. Manages networking via netlink and nftables.

## v0.1 Release

**Status**: Functional for home lab / enthusiast use. Not production-ready.

### Working Features
- **Zone-based Firewall** - nftables with stateful tracking
- **DHCP/DNS Services** - Embedded servers with lease management
- **Web UI** - Svelte 5 dashboard at port 8080
- **REST API** - Full CRUD with WebSocket events
- **Multi-WAN** - Health checks and failover
- **WireGuard VPN** - Native implementation via netlink
- **Integrity Monitor** - Auto-restores tampered rulesets
- **Smart Flush** - Dynamic sets persist across reloads

### In Progress
- GeoIP filtering (config only)
- DNS-over-HTTPS/TLS (config only)
- HA/VRRP failover (requires external keepalived)

## Quick Start

```bash
# Build
make build

# Development (VM with Web UI)
make dev

# Run tests
make test-int
```

## Architecture

```
┌────────────────────────────────────────────────────────────┐
│  glacic api (nobody, sandboxed)   HTTP:8080 / HTTPS:8443  │
│  └─ REST + WebSocket + Web UI                              │
├────────────────────────────────────────────────────────────┤
│  glacic ctl (root)                Unix Socket RPC          │
│  └─ Firewall │ Network │ DHCP │ DNS │ Learning            │
└────────────────────────────────────────────────────────────┘
```

## Configuration

HCL configuration. Example:

```hcl
interface "eth0" { zone = "WAN"; dhcp = true }
interface "eth1" { zone = "LAN"; ipv4 = ["192.168.1.1/24"] }

zone "LAN" { management { web_ui = true } }

policy "LAN" "WAN" {
  rule "allow" { action = "accept" }
}

nat "outbound" { type = "masquerade"; out_interface = "eth0" }

dhcp {
  scope "lan" {
    interface = "eth1"
    range_start = "192.168.1.100"
    range_end = "192.168.1.200"
  }
}

dns { forwarders = ["8.8.8.8"] }
```

## Documentation

- [Architecture](ARCHITECTURE.md) - System design
- [Features](FEATURES.md) - Feature matrix with maturity levels

## License

AGPL-3.0. See [LICENSE](LICENSE).
