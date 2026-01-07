# Zone Configuration Design

## Problem

Zone membership is configured in two places:
- `interface "eth0" { zone = "WAN" }` 
- `zone "WAN" { interfaces = ["eth0"] }`

This creates redundancy and potential for conflicts.

## Solution: Zone with Match Rules

Zone-centric config with flexible match criteria:

```hcl
# Simple: single interface
zone "wan" {
  interface = "eth0"
  dhcp = true
}

zone "lan" {
  interface = "eth1"
  ipv4 = ["192.168.1.1/24"]
  management { web = true }
  services { dhcp = true; dns = true }
}

# Advanced: multiple match rules (OR logic)
zone "dmz" {
  match = [
    { interface = "eth2" },
    { interface = "eth3" }
  ]
}

# Compound matches (AND logic within each match)
zone "guest" {
  match = [
    { interface = "eth1", vlan = 100, src = "192.168.10.0/24" }
  ]
  services { dns = true }
}

# Prefix matching
zone "vpn" {
  match = [
    { interface_prefix = "wg" }
  ]
}
```

## Match Criteria

Available both at zone top level (simple) or within `match` blocks (advanced):

| Field | Description |
|-------|-------------|
| `interface` | Exact interface name |
| `interface_prefix` | Interface prefix (like FireHOL's `wg+`) |
| `src` | Source IP/network |
| `dst` | Destination IP/network |
| `vlan` | VLAN tag |

**Usage:**

```hcl
# Simple: criteria at zone level
zone "lan" {
  interface = "eth1"
  src = "192.168.1.0/24"
}

# Advanced: match blocks (mutually exclusive with top-level criteria)
zone "guest" {
  match = [
    { interface = "eth1", vlan = 100 },
    { interface = "wlan0", src = "192.168.10.0/24" }
  ]
}
```

**OR additive approach** (global + overrides):

```hcl
zone "guest" {
  src = "192.168.10.0/24"  # Applies to ALL matches below
  
  match = [
    { interface = "eth1" },   # Inherits src
    { interface = "wlan0" }   # Inherits src
  ]
}
```

## Migration Path

1. Add `match` field to Zone (optional)
2. If `interfaces` set, convert internally
3. Deprecate `zone` on Interface
4. Deprecate `interfaces` on Zone
5. Remove deprecated fields in v2.0
