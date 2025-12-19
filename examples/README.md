# Glacic Configuration Examples

This directory contains example configuration files for Glacic Firewall.
You can use these files to learn about HCL syntax, test the configuration validator, or as a base for your own setup.

## Examples

| File | Description | Features |
|------|-------------|----------|
| [basic.hcl](basic.hcl) | Simple Home Router | WAN/LAN, DHCP Client/Server, DNS, Masquerade |
| [vpn-failover.hcl](vpn-failover.hcl) | Multi-WAN & VPN | Uplink Groups, Failover, Policy Routing, WireGuard |
| [port-forward.hcl](port-forward.hcl) | Service Exposure | DNAT (Port Forwarding), Source Restriction, Range Mapping |
| [complex-routing.hcl](complex-routing.hcl) | Enterprise Routing | BGP, OSPF, Routing Tables, Mark Rules, Traffic Isolation |

## Usage

### 1. Validate Configuration
Check the syntax and see a summary of the configuration without applying it:

```bash
# Basic check
glacic check examples/basic.hcl

# Detailed summary (Interfaces, Zones, Policies)
glacic check -v examples/vpn-failover.hcl
```

### 2. Preview Ruleset
See the nftables rules that would be generated from a configuration:

```bash
# Dump raw rules
glacic show examples/port-forward.hcl

# Show summary + rules
glacic show --summary examples/complex-routing.hcl
```

### 3. Run/Apply
To run one of these examples (requires root privileges):

```bash
# Run in foreground (Control Plane)
sudo glacic ctl examples/basic.hcl
```
