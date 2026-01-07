# Basic Home Router Configuration
#
# This configuration treats eth0 as the WAN interface (DHCP client)
# and eth1 as the LAN interface (static IP with DHCP server).
# It uses the "masquerade" policy to provide internet access to LAN clients.

schema_version = "1.0"

# Global settings
ip_forwarding = true
mss_clamping = true
enable_flow_offload = true

# -----------------------------------------------------------------------------
# Interfaces
# -----------------------------------------------------------------------------

# WAN Interface (Upstream Internet)
interface "eth0" {
  description = "WAN Uplink"
  zone        = "wan"
  dhcp        = true          # Enable DHCP Client
  dhcp_client = "builtin"     # Use internal DHCP client
}

# LAN Interface (Local Network)
interface "eth1" {
  description = "Local Network"
  zone        = "lan"
  ipv4        = ["192.168.1.1/24"]
}

# -----------------------------------------------------------------------------
# Zones
# -----------------------------------------------------------------------------

zone "wan" {
  description = "Internet / Untrusted"
}

zone "lan" {
  description = "Local Trusted Network"
}

# -----------------------------------------------------------------------------
# Policies (Firewall Rules)
# -----------------------------------------------------------------------------

# Default Policy: Deny all incoming traffic to self (INPUT chain default drop)
# Glacic automatically allows established/related traffic.

# Allow LAN to access WAN (Internet)
policy "lan" "wan" {
  action = "accept"
}

# Allow LAN to access Router (DNS, DHCP, SSH)
policy "lan" "self" {

  rule "allow-ssh" {
    proto = "tcp"
    dest_port = 22
    action = "accept"
  }
  rule "allow-dns" {
    proto = "udp"
    dest_port = 53
    action = "accept"
  }
  rule "allow-dhcp" {
    proto = "udp"
    dest_port = 67
    action = "accept"
  }
  # ICMP Ping
  rule "allow-icmp" {
    proto = "icmp"
    action = "accept"
  }
}

# Allow WAN to ping Router (optional, safe default is drop)
policy "wan" "self" {

  rule "allow-ping" {
    proto = "icmp"
    # icmp_type = "echo-request"
    action = "accept" # Change to drop to hide from pings
  }
}

# -----------------------------------------------------------------------------
# NAT (Network Address Translation)
# -----------------------------------------------------------------------------

# Masquerade (SNAT) all traffic leaving WAN
nat "masquerade-wan" {
  type        = "masquerade"
  out_interface = "eth0"
}

# -----------------------------------------------------------------------------
# Services (DHCP & DNS)
# -----------------------------------------------------------------------------

dhcp {
  enabled = true

  scope "lan-scope" {
    interface   = "eth1"
    range_start = "192.168.1.100"
    range_end   = "192.168.1.200"
    router      = "192.168.1.1"
    dns         = ["192.168.1.1", "1.1.1.1"]

    # Static Lease Example
    reservation "00:11:22:33:44:55" {
      ip          = "192.168.1.10"
      description = "printer"
    }
  }
}

dns_server {
  enabled    = true
  listen_on  = ["192.168.1.1:53"]
  forwarders = ["1.1.1.1", "8.8.8.8"]
  cache_size = 1000

  # Local DNS records
  host "192.168.1.1" {
    hostnames = ["router.lan", "gateway.lan"]
  }
}
