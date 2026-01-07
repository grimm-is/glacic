# Example: Using IPSets with FireHOL blocklists
#
# This configuration demonstrates:
# 1. Importing FireHOL threat intelligence lists
# 2. Creating custom IP sets
# 3. Using sets in firewall rules

# Enable IP forwarding for routing
ip_forwarding = true

# Interface configuration
interface "eth0" {
  zone = "wan"
  dhcp = true
}

interface "eth1" {
  zone = "lan"
  ipv4 = ["192.168.1.1/24"]
}

# ===========================================
# IPSet Definitions
# ===========================================

# FireHOL Level 1 - Safe for production
# Blocks known attackers, malware, and bad actors from the last 48 hours
ipset "firehol_blocklist" {
  description   = "FireHOL Level 1 - Known attackers and malware"
  firehol_list  = "firehol_level1"
  auto_update   = true
  refresh_hours = 24

  # Auto-generate blocking rules
  action          = "drop"
  apply_to        = "input"
  match_on_source = true
}

# Spamhaus DROP list - Hijacked IP space
ipset "spamhaus_drop" {
  description   = "Spamhaus DROP - Hijacked/leased IP space"
  firehol_list  = "spamhaus_drop"
  auto_update   = true
  refresh_hours = 12

  action          = "drop"
  apply_to        = "both"   # Block on input and forward
  match_on_source = true
}

# Spamhaus Extended DROP
ipset "spamhaus_edrop" {
  description   = "Spamhaus EDROP - Extended hijacked space"
  firehol_list  = "spamhaus_edrop"
  auto_update   = true
  refresh_hours = 12

  action          = "drop"
  apply_to        = "both"
  match_on_source = true
}

# DShield - Top attacking IPs
ipset "dshield_attackers" {
  description   = "DShield - Top attacking IPs in the last 24 hours"
  firehol_list  = "dshield"
  auto_update   = true
  refresh_hours = 6

  action          = "drop"
  apply_to        = "input"
  match_on_source = true
}

# Custom URL-based blocklist
ipset "custom_blocklist" {
  description = "Custom blocklist from internal threat feed"
  url         = "https://internal.example.com/threat-ips.txt"
  auto_update = true
  refresh_hours = 4

  action          = "drop"
  apply_to        = "both"
  match_on_source = true
}

# Static IP set for known bad actors
ipset "manual_blocklist" {
  description = "Manually maintained blocklist"
  type        = "ipv4_addr"

  entries = [
    "203.0.113.0/24",    # Example bad network
    "198.51.100.50",     # Known attacker
    "192.0.2.100",       # Another bad actor
  ]

  action          = "drop"
  apply_to        = "both"
  match_on_source = true
}

# TOR exit nodes (optional - block anonymous access)
# Uncomment if you want to block TOR traffic
# ipset "tor_exits" {
#   description   = "TOR Exit Nodes"
#   firehol_list  = "tor_exits"
#   auto_update   = true
#   refresh_hours = 6
#
#   action          = "drop"
#   apply_to        = "input"
#   match_on_source = true
# }

# Trusted IP set for bypass
ipset "trusted_ips" {
  description = "Trusted IPs - bypass blocklists"
  type        = "ipv4_addr"

  entries = [
    "10.0.0.0/8",        # Internal networks
    "172.16.0.0/12",
    "192.168.0.0/16",
  ]
}

# ===========================================
# Firewall Policies
# ===========================================

# WAN to Firewall (input) policy
policy "wan" "self" {

  # Allow trusted IPs to bypass other rules
  rule "allow-trusted" {
    src_ipset = "trusted_ips"
    action    = "accept"
  }

  # Note: FireHOL blocklists with action="drop" are applied automatically
  # before these rules when apply_to includes "input"

  # Allow established connections
  rule "allow-established" {
    description = "Allow established/related connections"
    action      = "accept"
  }

  # Allow SSH from specific IPs only
  rule "allow-ssh" {
    proto     = "tcp"
    dest_port = 22
    src_ip    = "203.0.113.10/32"  # Admin IP
    action    = "accept"
    log       = true
    log_prefix = "SSH-ACCESS: "
  }

  # Drop everything else
  rule "default-drop" {
    action     = "drop"
    log        = true
    log_prefix = "WAN-DROP: "
  }
}

# LAN to WAN (forward) policy
policy "lan" "wan" {

  # Allow all outbound from LAN
  rule "allow-outbound" {
    action = "accept"
  }
}

# WAN to LAN (forward) policy
policy "wan" "lan" {

  # Note: FireHOL blocklists with apply_to="both" or "forward"
  # are applied automatically before these rules

  # Allow established connections
  rule "allow-established" {
    action = "accept"
  }

  # Drop by default
  rule "default-drop" {
    action = "drop"
  }
}

# ===========================================
# NAT Configuration
# ===========================================

nat "masquerade-wan" {
  type          = "masquerade"
  out_interface = "eth0"
}

# ===========================================
# Protection Settings
# ===========================================

protection {
  anti_spoofing       = true
  bogon_filtering     = true
  invalid_packets     = true
  syn_flood_protection = true
  syn_flood_rate      = 25
  syn_flood_burst     = 50
  icmp_rate_limit     = true
  icmp_rate           = 10
}
