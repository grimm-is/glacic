# Features

Feature maturity based on integration test coverage.

| Level | Meaning |
|:-----:|---------|
| ✅ L5 | Production-ready, comprehensive tests |
| 🟩 L4 | Integration tested in VM |
| 🟨 L3 | Works, limited testing |
| 🟧 L2 | Scaffolded, may not function |
| 🔲 L1 | Config only, no runtime |
| ⬜ L0 | Not started |

---

## Core

| Feature | Level | Notes |
|---------|:-----:|-------|
| Zone-based Firewall | ✅ | Policies, stateful tracking |
| nftables Generation | 🟩 | Atomic apply via script |
| Interface Management | 🟩 | Static IP, DHCP client |
| VLAN / Bonding | 🟩 | Tested |
| Routing (static) | 🟩 | IPv4/IPv6 |
| Policy Routing | 🟩 | fwmark-based |
| NAT (masquerade, DNAT) | 🟩 | Hairpin NAT works |
| HCL Config | ✅ | Validation, migration, hot reload |

## Services

| Feature | Level | Notes |
|---------|:-----:|-------|
| DHCP Server | 🟩 | Leases, persistence, options |
| DNS Forwarder | 🟩 | Caching, blocklists (file/URL) |
| DNS Egress Control | 🟩 | "DNS Wall" - blocks non-resolved IPs |
| Split-Horizon DNS | 🟩 | |
| Wake-on-LAN | 🟩 | |
| mDNS Reflector | 🟩 | Cross-VLAN Bonjour |
| UPnP/NAT-PMP | 🟩 | Port forwarding |
| Router Advertisements | 🟩 | IPv6 SLAAC |
| LLDP Discovery | 🟩 | Switch detection |
| Threat Intel | 🟩 | Blocklist fetching |
| DNSSEC / DoH / DoT | 🔲 | Config only |

## Security

| Feature | Level | Notes |
|---------|:-----:|-------|
| Privilege Separation | ✅ | ctl(root) / api(nobody) |
| Sandbox (netns) | 🟩 | API in isolated namespace |
| Integrity Monitor | 🟩 | Auto-restore on tampering |
| Smart Flush | 🟩 | Dynamic sets persist |
| Fail2Ban-style Blocking | 🟩 | |
| IPSet Blocklists | 🟩 | FireHOL integration |
| SYN Flood Protection | 🟩 | |
| Time-of-Day Rules | 🟩 | Kernel 5.4+ |
| GeoIP Filtering | 🔲 | Config only |

## VPN

| Feature | Level | Notes |
|---------|:-----:|-------|
| WireGuard | 🟩 | Native via netlink/wgctrl |
| Tailscale | 🟩 | Status/control via socket |
| VPN Lockout Protection | 🟩 | |

## API & UI

| Feature | Level | Notes |
|---------|:-----:|-------|
| REST API | 🟩 | Full CRUD |
| WebSocket Events | 🟩 | Real-time updates |
| OpenAPI Docs | 🟩 | |
| Web Dashboard | 🟨 | Most pages functional |
| TLS / Auth | 🟩 | API keys, sessions |

## Operations

| Feature | Level | Notes |
|---------|:-----:|-------|
| Hot Reload | 🟩 | SIGHUP or API |
| Atomic Apply | 🟩 | Rollback on failure |
| Seamless Upgrade | 🟩 | Socket handoff |
| Prometheus Metrics | 🟩 | |
| Syslog Forwarding | 🟩 | |
| HA Replication | 🟧 | DB sync only, no VIP/VRRP |

## Learning Engine

| Feature | Level | Notes |
|---------|:-----:|-------|
| Flow Tracking | 🟩 | nflog-based |
| SNI Snooping | 🟩 | |
| Pending Rule Approval | 🟩 | |
| Device Discovery | 🟩 | DHCP + ARP |

---

## Summary

| Level | Count |
|:-----:|:-----:|
| ✅ L5 | 3 |
| 🟩 L4 | 45 |
| 🟨 L3 | 5 |
| 🟧 L2 | 1 |
| 🔲 L1 | 3 |
