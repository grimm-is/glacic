# Glacic Features & Roadmap

**Glacic** is a modern, privilege-separated network appliance built for those who demand enterprise reliability with home-lab simplicity. It combines the performance of `nftables`, the safety of Go, and the declarative power of HCL.

---

## Feature Maturity Scale

| Level | Icon | Name | Description |
|:-----:|:----:|------|-------------|
| L0 | ⬜ | Not Started | Feature is on roadmap but no code exists |
| L1 | 🔲 | Designed | Config structures defined, no runtime implementation |
| L2 | 🟧 | Scaffolded | Code exists but uses stubs/placeholders, may not function |
| L3 | 🟨 | Functional | Happy path works, limited or no automated testing |
| L4 | 🟩 | Tested | Integration tests pass, works in VM, edge cases may fail |
| L5 | ✅ | Production | Comprehensive testing, documented, handles edge cases |

Most features are currently at **L3-L4**. Focus is on reaching L4+ across core functionality.

---

## Complete Feature Matrix

| Category | Feature | Level | Notes |
|----------|---------|:-----:|-------|
| **NETWORK** | | | |
| Interfaces | Static IP Configuration | 🟩 L4 | Works, tested in VM |
| Interfaces | DHCP Client | 🟩 L4 | Works, tested |
| Interfaces | Enable/Disable | 🟩 L4 | Via API and config |
| Interfaces | MTU Configuration | 🟨 L3 | Works, limited testing |
| Interfaces | VLAN Support (802.1Q) | 🟧 L2 | Parsing works, runtime untested |
| Interfaces | Bond/LACP | 🟧 L2 | Parsing works, needs hardware |
| Routing | IPv4 Static Routes | 🟩 L4 | Works, tested |
| Routing | Default Gateway | 🟩 L4 | Works, tested |
| Routing | IPv6 Addressing | 🟩 L4 | Confirmed via `t/01-sanity/ipv6_test.sh` |
| Routing | IPv6 Routing | 🟩 L4 | Confirmed via `t/01-sanity/ipv6_test.sh` |
| Routing | Policy Routing (fwmark) | 🟩 L4 | Confirmed via `t/30-firewall/policy_routing_test.sh` |
| Routing | Multi-WAN Failover | 🟨 L3 | Health checks, tier-based failover, connmark restore |
| Routing | Multi-WAN Load Balance | 🟨 L3 | Weighted, latency, adaptive modes via numgen |
| Routing | User/UID-Based Routing | 🔲 L1 | Config structures only |
| **FIREWALL** | | | |
| Zones | Zone Definition | ✅ L5 | Complete, well tested |
| Zones | Zone-Based Policies | 🟩 L4 | Works, tested in VM |
| Zones | Management Zone Access | 🟨 L3 | Works, needs more testing |
| Rules | nftables Rule Generation | 🟩 L4 | inet family, IPv4+IPv6 |
| Rules | Stateful Connection Tracking | 🟩 L4 | Enabled by default |
| Rules | Rule Logging (nflog) | 🟨 L3 | Rules log, viewer works |
| Rules | Rate Limiting | 🟩 L4 | Confirmed via `t/65-qos/qos_test.sh` |
| Rules | GeoIP Matching | 🔲 L1 | Config only, no implementation |
| Rules | Time-of-Day Rules | 🔲 L1 | Parsing exists, scheduler incomplete |
| Rules | Layer 7 / Application ID | 🔲 L1 | SNI hooks exist, no L7 filtering |
| NAT | Masquerade | 🟩 L4 | Works, tested |
| NAT | SNAT (Static NAT) | 🟨 L3 | Works, limited testing |
| NAT | DNAT / Port Forwarding | 🟨 L3 | Works, limited testing |
| NAT | Hairpin NAT | 🟧 L2 | Config exists, untested |
| Protection | SYN Flood Protection | 🟨 L3 | Rules generated, not stress tested |
| Protection | Anti-Spoofing / Bogon | 🟨 L3 | Rules generated |
| Protection | ICMP Handling | 🟩 L4 | Tested, proper allow rules |
| Protection | Panic Mode (Lockdown) | 🟩 L4 | Works via API |
| IPSets | Manual Entries | 🟨 L3 | Works for basic sets |
| IPSets | FireHOL Integration | 🟧 L2 | Download works, auto-update incomplete |
| IPSets | IPSet in Policy Rules | 🟨 L3 | Works, needs more testing |
| Accounting | Traffic Accounting | 🟨 L3 | Per-IP/Subnet bytes/packets (internal/firewall/accounting.go) |
| **DHCP** | | | |
| Server | DHCP Server Core | 🟩 L4 | DISCOVER/OFFER/REQUEST/ACK works |
| Server | Lease Allocation | 🟩 L4 | Pool allocation works |
| Server | Lease Persistence | 🟩 L4 | SQLite, survives restarts |
| Server | Static Reservations | 🟩 L4 | MAC→IP mapping works |
| Server | Lease Expiration | 🟩 L4  | Logic works, unit tests pass |
| Server | DHCP Options (basic) | 🟩 L4 | Confirmed via `t/20-dhcp/dhcp_options_test.sh` |
| Server | DHCP Options (custom) | 🟩 L4 | Confirmed via `t/20-dhcp/dhcp_options_test.sh` |
| Server | DHCPv6 | ⬜ L0 | Not started |
| Server | DHCP Relay | ⬜ L0 | Not started |
| Server | DNS Dynamic Updates | 🟨 L3 | Hostname→IP added to DNS |
| **DNS** | | | |
| Forwarder | DNS Forwarder | 🟩 L4 | Upstream queries work |
| Forwarder | DNS Caching | 🟩 L4 | TTL-based caching works |
| Forwarder | Conditional Forwarding | 🟨 L3 | Works, limited testing |
| Records | Local Records (static) | 🟩 L4 | Zone records work |
| Records | /etc/hosts Integration | 🟩 L4 | Auto-loaded |
| Records | Split-Horizon DNS | 🟧 L2 | Basic support |
| Blocklists | DNS Blocklists (file) | 🟨 L3 | Hosts-format files work |
| Blocklists | DNS Blocklists (URL) | 🟩 L4 | Downloads and caches blocklists |
| Security | DNSSEC Validation | 🔲 L1 | Config only |
| Security | DNS-over-HTTPS (DoH) | 🔲 L1 | Config only |
| Security | DNS-over-TLS (DoT) | 🔲 L1 | Config only |
| Security | DNSCrypt | 🔲 L1 | Config only |
| Advanced | Recursive Resolver | 🔲 L1 | Config only |
| **SERVICES** | | | |
| WoL | Wake-on-LAN | 🟩 L4 | Magic packet works |
| IPv6 | Router Advertisements | 🟧 L2 | Basic RA, limited testing |
| Discovery | LLDP Listener | 🟧 L2 | Stub implementation |
| Threat Intel | IP/Domain Blocklists | 🟩 L4 | Fetches URLs, updates ipsets (`t/50-security/threat_intel_test.sh`) |
| Services | mDNS Reflector | 🟩 L4 | Tested via `t/40-network/mdns_test.sh` |
| Future | UPnP/NAT-PMP | ⬜ L0 | Not started |
| Future | NTP Server | ⬜ L0 | Not started |
| Future | Syslog Server | ⬜ L0 | Not started |
| **INTELLIGENCE** | | | |
| Discovery | Device Discovery (DHCP) | 🟨 L3 | MAC/IP/hostname from leases |
| Discovery | Device Discovery (ARP) | 🔲 L1 | Designed, not implemented |
| Discovery | MAC Vendor Lookup | 🟨 L3 | Works for known vendors |
| Discovery | Device Identity | 🟨 L3 | Persists User/Owner metadata |
| Flows | Flow Tracking | 🟧 L2 | Basic structures exist |
| Flows | SNI Snooping | 🟨 L3 | TLS SNI extracted, storage limited |
| Flows | Application Identification | 🔲 L1 | SNI→app mapping designed |
| Learning | Rule Learning (NFLog) | 🟩 L4 | UI works, backend integrated |
| Learning | Real nflog Integration | 🟩 L4 | Bridges nflog to engine |
| Learning | Pending Rule Approval | 🟨 L3 | UI/API works, no auto-apply |
| Scanning | Active Port Scanning | 🟨 L3 | Basic scan works |
| Topology | Network Topology View | 🔲 L1 | Placeholder UI only |
| **SECURITY** | | | |
| Arch | Privilege Separation | ✅ L5 | ctl/api split, complete |
| Arch | Chroot Sandboxing | 🟩 L4 | Works, tested |
| Arch | Network Namespace | 🟨 L3 | Works, some edge cases |
| Auth | API Key Authentication | 🟩 L4 | Works, persisted |
| Auth | Password Authentication | 🟨 L3 | Basic bcrypt, no account mgmt |
| Auth | Session Management | 🟨 L3 | Tokens work, no refresh |
| Auth | Fail2Ban-style Blocking | 🔲 L1 | Config only |
| Audit | Audit Logging | 🔲 L1 | Basic logs, no audit trail |
| TLS | TLS for API | 🟩 L4 | `api_tls.sh` verifies cert/HTTPS |
| TLS | Certificate Management | 🟩 L4 | Generates self-signed if missing |
| **VPN** | | | |
| Tailscale | Status Monitoring | 🟨 L3 | Works if tailscaled running |
| Tailscale | Up/Down Control | 🟨 L3 | CLI wrapper works |
| Tailscale | Firewall Bypass | 🟧 L2 | Rules generated, untested |
| Tailscale | MagicDNS Integration | 🔲 L1 | Designed |
| Headscale | Headscale Support | 🔲 L1 | Same as Tailscale |
| WireGuard | WireGuard Native | 🟩 L4 | Tested via `vpn_test.sh`, status reporting works |
| WireGuard | Key Management | 🟨 L3 | Keys generated, masked in API (Security L4) |
| General | VPN Lockout Protection | 🟧 L2 | Flag exists, incomplete |
| **OPERATIONS** | | | |
| Config | HCL Config Parsing | ✅ L5 | Complete, extensive tests |
| Config | Config Validation | 🟩 L4 | Works, good error messages |
| Config | Schema Versioning | 🟨 L3 | Framework works |
| Config | Schema Migration | 🟧 L2 | Framework exists, few migrations |
| Config | Config Hot Reload | 🟨 L3 | Works for most services |
| Config | Atomic Apply + Rollback | 🟩 L4 | Apply works, rollback timer, connectivity check |
| Upgrade | Seamless Upgrade | 🟨 L3 | Socket handoff works; FRR/BGP/VPN state NOT serialized |
| State | State Store (SQLite) | 🟩 L4 | Works, buckets for all services |
| State | State Replication | 🟧 L2 | Database sync works (Hot Standby, no VIP/VRRP) |
| State | VIP Failover (VRRP) | ⬜ L0 | Requires external keepalived/VRRP for auto-failover |
| Backup | Config Backup/Restore | 🟩 L4 | Works, versioned |
| Monitoring | Prometheus Metrics | 🟧 L2 | Some metrics, incomplete |
| Logging | Structured Logging | 🟩 L4 | slog-based, works |
| Logging | Log Forwarding (syslog) | 🔲 L1 | Config only |
| Supervisor | Watchdog/Monitor | 🟨 L3 | Works, restarts services |
| Stability | Crash / Boot Loop Protection | 🟩 L4 | Panic counting, safe mode persistence |
| **API** | | | |
| Endpoints | Status Endpoints | 🟩 L4 | Works |
| Endpoints | Interface CRUD | 🟩 L4 | Works (Tested via `api_crud_test.sh`) |
| Endpoints | Firewall/Policy CRUD | 🟩 L4 | Basic works (Tested via `api_crud_test.sh`) |
| Endpoints | DHCP Endpoints | 🟩 L4 | Leases work, scopes partial (Tested via `api_crud_test.sh`) |
| Endpoints | DNS Endpoints | 🟩 L4 | CRUD for DNS settings/records (Tested) |
| Endpoints | IPSet Endpoints | 🟩 L4 | CRUD for IPSets (Tested) |
| Endpoints | VPN Endpoints | 🟨 L3 | `vpn_test.sh` confirms status via `/api/interfaces` |
| Endpoints | Learning Endpoints | 🟨 L3 | CRUD works |
| Endpoints | System (reboot, upgrade) | 🟨 L3 | Works |
| Realtime | WebSocket Notifications | 🟨 L3 | Works |
| Docs | OpenAPI Documentation | ⬜ L0 | Not started |
| **WEB UI** | | | |
| Pages | Dashboard | 🟩 L4 | Status, uptime, services |
| Pages | Interfaces | 🟨 L3 | View works, edit basic |
| Pages | Zones | 🟨 L3 | View works, mgmt zones incomplete |
| Pages | Firewall | 🟨 L3 | Policy list, rule editor basic |
| Pages | NAT | 🟨 L3 | List/add works |
| Pages | DHCP | 🟨 L3 | Leases show, scope edit partial |
| Pages | DNS | 🟨 L3 | Records work, encrypted DNS UI-only |
| Pages | Routing | 🟨 L3 | Route table display |
| Pages | IPSets | 🟧 L2 | List works |
| Pages | VPN | 🟧 L2 | Status if running |
| Pages | Clients | 🟨 L3 | DHCP clients shown |
| Pages | Discovery | 🟧 L2 | Device list, topology placeholder |
| Pages | Learning | 🟧 L2 | Pending rules, backend stub |
| Pages | Scanner | 🟨 L3 | Port scan works |
| Pages | Logs | 🟨 L3 | Viewing works |
| Pages | Topology | 🔲 L1 | Placeholder |
| Pages | Settings | 🟨 L3 | Backup works |
| **CLI** | | | |
| Commands | `glacic ctl` | 🟩 L4 | Control plane start |
| Commands | `glacic api` | 🟩 L4 | API server start |
| Commands | `glacic test` | 🟩 L4 | Config test mode |
| Commands | `glacic upgrade` | 🟨 L3 | Binary upgrade |
| Commands | `glacic status` | 🟨 L3 | Basic status |
| Commands | `glacic config validate` | 🟨 L3 | Works |
| Commands | `glacic ipset` | 🟧 L2 | Basic commands |
| **TUI** | | | |
| Console | Navigation | 🟧 L2 | Basic works |
| Console | Status View | 🟧 L2 | Shows status |
| Console | Configuration | 🔲 L1 | Placeholder |
| Console | Logs | 🔲 L1 | Placeholder |

---

## Summary by Level

| Level | Count | Percentage |
|:-----:|:-----:|:----------:|
| ✅ L5 | 4 | 2% |
| 🟩 L4 | 37 | 23% |
| 🟨 L3 | 55 | 34% |
| 🟧 L2 | 35 | 22% |
| 🔲 L1 | 24 | 15% |
| ⬜ L0 | 5 | 3% |

**Total Features: 160**

---

## Test Coverage

| Area | Unit | Integration | Overall |
|------|:----:|:-----------:|:-------:|
| Config Parsing | ✅ | ✅ | 🟩 L4 |
| Firewall Rules | 🟨 | 🟨 | 🟨 L3 |
| DHCP Server | 🟨 | 🟨 | 🟨 L3 |
| DNS Server | 🟨 | 🟨 | 🟨 L3 |
| API Auth | 🟨 | 🟨 | 🟨 L3 |
| State Store | ✅ | 🟨 | 🟩 L4 |
| Replication | 🟧 | 🟧 | 🟧 L2 |
| Upgrade | 🟧 | 🟧 | 🟧 L2 |
| VPN | ⬜ | 🟩 | 🟩 L4 |
| Learning | ⬜ | 🟩 | 🟩 L4 |

---

## Current Focus

**Target: Get core features to L4 (Tested)**

### Immediate (This Week)
- Fix remaining integration test failures
- DHCP lease expiration testing
- DNS blocklist URL download

### Short Term (This Month)
- [x] **Endpoints**: L3 (Functional) - Routes, Peers, Status checks implemented.
- [x] **Status**: L4 (Tested) - Verified via `vpn_test.sh`. API reports interfaces correctly.
- [x] Real nflog implementation
- API documentation (OpenAPI)
- [x] TLS support

### Medium Term (Next Quarter)
- HA failover testing
- Multi-WAN implementation
- Complete Web UI features
- Performance benchmarks

---

## Quick Start

```bash
# Build
make build

# Start (requires root for ctl)
sudo glacic ctl /etc/glacic/firewall.hcl &
glacic api

# Development
make dev          # VM with Web UI
make test-int     # Integration tests
```

See [DEVELOPMENT.md](DEVELOPMENT.md) for detailed instructions.
