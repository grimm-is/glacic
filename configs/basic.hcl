# Demo Firewall Configuration
# Basic setup for demonstration purposes

ip_forwarding = true

# Interface configuration
interface "eth0" {
  description = "WAN Interface"
  dhcp        = true
}

interface "eth1" {
  description = "Green Zone (Trusted LAN)"
  ipv4        = ["10.1.0.1/24"]
}

interface "eth2" {
  description = "Orange Zone (DMZ)"
  ipv4        = ["10.2.0.1/24"]
}

interface "eth3" {
  description = "Red Zone (Restricted)"
  ipv4        = ["10.3.0.1/24"]
}

# Zone Definitions
zone "WAN" {
  interface = "eth0"
}
zone "Green" {
  interface = "eth1"
}
zone "Orange" {
  interface = "eth2"
}
zone "Red" {
  interface = "eth3"
}

# Basic firewall policies
policy "Green" "WAN" {
  name = "green_to_wan"

  rule "allow_internet" {
    description = "Allow Green zone internet access"
    action = "accept"
  }
}

policy "Orange" "WAN" {
  name = "orange_to_wan"

  rule "allow_internet" {
    description = "Allow Orange zone internet access"
    action = "accept"
  }
}

policy "Red" "WAN" {
  name = "red_blocked"

  rule "block_internet" {
    description = "Block Red zone from internet"
    action = "drop"
  }
}

# NAT for outbound traffic
nat "outbound" {
  type          = "masquerade"
  out_interface = "eth0"
}

# DHCP Server
dhcp {
  enabled = true
  scope "Green" {
    interface   = "eth1"
    range_start = "10.1.0.100"
    range_end   = "10.1.0.200"
    router      = "10.1.0.1"
    lease_time  = "24h"
  }
}

# DNS Server
dns_server {
  enabled   = true
  listen_on = ["10.1.0.1"]
  forwarders = ["8.8.8.8"]
}
