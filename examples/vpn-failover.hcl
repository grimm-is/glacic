# VPN Failover & Multi-WAN Configuration
#
# This example demonstrates a complex scenario with:
# 1. Primary WAN (eth0)
# 2. Backup LTE (eth2)
# 3. WireGuard VPN for privacy
# 4. Policy routing to direct specific traffic
# 5. Automatic failover

schema_version = "1.0"
ip_forwarding = true

# -----------------------------------------------------------------------------
# Interfaces
# -----------------------------------------------------------------------------

interface "eth0" {
  description = "Primary WAN (Fiber)"
  zone        = "wan"
  dhcp        = true
}

interface "eth2" {
  description = "Backup LTE"
  zone        = "wan_lte"
  dhcp        = true
  # Higher metric to ensure it's not default route by default OS logic
  # Glacic uplink groups handle the routing tables, but this helps.
}

interface "wg0" {
  description = "Privacy VPN"
  zone        = "vpn"
  # WireGuard interface configuration would happen externally or via
  # a dedicated wireguard block if implemented, but here we treat it
  # as an interface that is ALREADY UP or managed by OS.
  # For this example, we assume wg0 is a standard interface.
  ipv4 = ["10.100.0.2/24"]
}

interface "eth1" {
  description = "LAN"
  zone        = "lan"
  ipv4        = ["192.168.10.1/24"]
}

# -----------------------------------------------------------------------------
# Uplink Groups (Failover & Load Balancing)
# -----------------------------------------------------------------------------

# Main Internet Access Group
# Tries Fiber first, fails over to LTE
uplink_group "internet" {
  source_networks = ["192.168.10.0/24"]
  failover_mode   = "graceful"

  uplink "fiber" {
    interface = "eth0"
    tier      = 0  # Primary
    type      = "wan"
    enabled   = true

    health_check_cmd = "ping -c 1 -W 1 8.8.8.8"
  }

  uplink "lte" {
    interface = "eth2"
    tier      = 1  # Secondary
    type      = "wan"
    enabled   = true

    # LTE might be metered, so we put it in tier 1
  }
}

# VPN Group
# Traffic routed here goes through WireGuard
uplink_group "secure-vpn" {
  source_networks = [] # We use policy routing rules instead of broad networks
  failover_mode   = "immediate"

  uplink "wg-primary" {
    interface = "wg0"
    gateway   = "10.100.0.1"
    type      = "wireguard"
    enabled   = true
  }
}

# -----------------------------------------------------------------------------
# Policy Routing (Directing Traffic)
# -----------------------------------------------------------------------------

# Default: Everything not matched goes via "internet" uplink group (implicitly
# handled if source_networks matches, but we can be explicit if needed).

# Route specific sensitive device via VPN always
policy_route "tv-via-vpn" {
  from    = "192.168.10.50/32"
  comment = "Smart TV uses VPN for geo-unblocking"
  # We can target a specific table ID or (future) target an uplink group directly.
  # For now, we assume user configured a custom table for VPN or maps manually.
  # In valid Glacic config, UplinkGroup creates required rules.
  # But we can override/add specific policy routes.

  # Example: Force to VPN table (assuming ID 100 for VPN)
  table = 100
}

# -----------------------------------------------------------------------------
# NAT
# -----------------------------------------------------------------------------

nat "masquerade-fiber" {
  type          = "masquerade"
  out_interface = "eth0"
}

nat "masquerade-lte" {
  type          = "masquerade"
  out_interface = "eth2"
}

nat "masquerade-vpn" {
  type          = "masquerade"
  out_interface = "wg0"
}

# -----------------------------------------------------------------------------
# Firewall Policies
# -----------------------------------------------------------------------------

# Allow LAN to ALL WANs
policy "lan" "wan" { action = "accept" }
policy "lan" "wan_lte" { action = "accept" }
policy "lan" "vpn" { action = "accept" }

# -----------------------------------------------------------------------------
# UID Routing (Process Based)
# -----------------------------------------------------------------------------

# Force transmission client (running as user 'debian-transmission') to use VPN
uid_routing "torrent-vpn" {
  username = "debian-transmission"
  uplink   = "wg-primary" # Matches uplink name
  vpn_link = "wg-primary" # For backwards compatibility/required field
  enabled  = true
}
